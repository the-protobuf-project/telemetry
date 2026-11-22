#include "telemetry/otel/exporter.hpp"

#if TELEMETRY_USE_OTEL

#include <opentelemetry/sdk/logs/batch_log_record_processor.h>
#include <opentelemetry/sdk/metrics/view/view_registry.h>
#include <opentelemetry/sdk/trace/batch_span_processor.h>

#include <algorithm>
#include <chrono>
#include <thread>
#include <vector>

namespace telemetry::otel {

void Counters::emit() noexcept {
    emitted.fetch_add(1, std::memory_order_acq_rel);
    if (total) total->emit();
}

void Counters::record(uint64_t n, bool ok) noexcept {
    (ok ? exported : failed).fetch_add(n, std::memory_order_acq_rel);
    if (total) total->record(n, ok);
}

uint64_t Counters::pending() const noexcept {
    return stats().pending();
}

SignalStats Counters::stats() const noexcept {
    SignalStats s;
    s.exported = exported.load(std::memory_order_acquire);
    s.failed = failed.load(std::memory_order_acquire);
    s.emitted = emitted.load(std::memory_order_acquire);
    return s;
}

namespace {

namespace common = opentelemetry::sdk::common;
using std::chrono::microseconds;

// The SDK's batch processors drop what does not fit in their queue and say
// so only through internal logging, so a burst of a million spans could lose
// most of them without a word. These wrappers count what was emitted,
// exported and failed; what was emitted and never left the process by
// shutdown was dropped, and Telemetry::close says so.

// Holds a writer back while the queue behind it is full (lossless mode),
// rather than letting the batch processor drop the record.
class Gate {
public:
    explicit Gate(const ExportOptions& opts)
        : lossless_(opts.lossless)
        ,
        // kept below the queue size by enough headroom for writers racing
        // past the check at once
        capacity_(std::max<uint64_t>(opts.queue_size - std::min<uint64_t>(opts.queue_size / 4, 1024), 1)) {}

    void wait(const Counters& counters) const noexcept {
        if (!lossless_) return;
        auto backoff = microseconds(1);
        while (counters.pending() >= capacity_ && !closed_.load(std::memory_order_acquire)) {
            std::this_thread::sleep_for(backoff);
            backoff = std::min(backoff * 2, microseconds(1000));
        }
    }

    void close() noexcept { closed_.store(true, std::memory_order_release); }

private:
    bool lossless_;
    uint64_t capacity_;
    std::atomic<bool> closed_{false};
};

// A span exporter that counts what it exports and what fails.
class CountingSpanExporter final : public trace_sdk::SpanExporter {
public:
    CountingSpanExporter(std::unique_ptr<trace_sdk::SpanExporter> inner, std::shared_ptr<Counters> counters)
        : inner_(std::move(inner)), counters_(std::move(counters)) {}

    std::unique_ptr<trace_sdk::Recordable> MakeRecordable() noexcept override { return inner_->MakeRecordable(); }

    common::ExportResult Export(
        const opentelemetry::nostd::span<std::unique_ptr<trace_sdk::Recordable>>& spans) noexcept override {
        const auto result = inner_->Export(spans);
        counters_->record(spans.size(), result == common::ExportResult::kSuccess);
        return result;
    }

    bool ForceFlush(microseconds timeout) noexcept override { return inner_->ForceFlush(timeout); }
    bool Shutdown(microseconds timeout) noexcept override { return inner_->Shutdown(timeout); }

private:
    std::unique_ptr<trace_sdk::SpanExporter> inner_;
    std::shared_ptr<Counters> counters_;
};

// A log exporter that counts what it exports and what fails.
class CountingLogExporter final : public logs_sdk::LogRecordExporter {
public:
    CountingLogExporter(std::unique_ptr<logs_sdk::LogRecordExporter> inner, std::shared_ptr<Counters> counters)
        : inner_(std::move(inner)), counters_(std::move(counters)) {}

    std::unique_ptr<logs_sdk::Recordable> MakeRecordable() noexcept override { return inner_->MakeRecordable(); }

    common::ExportResult Export(
        const opentelemetry::nostd::span<std::unique_ptr<logs_sdk::Recordable>>& records) noexcept override {
        const auto result = inner_->Export(records);
        counters_->record(records.size(), result == common::ExportResult::kSuccess);
        return result;
    }

    bool ForceFlush(microseconds timeout) noexcept override { return inner_->ForceFlush(timeout); }
    bool Shutdown(microseconds timeout) noexcept override { return inner_->Shutdown(timeout); }
    bool RecordableEnforcesLogRecordLimits() const noexcept override {
        return inner_->RecordableEnforcesLogRecordLimits();
    }

private:
    std::unique_ptr<logs_sdk::LogRecordExporter> inner_;
    std::shared_ptr<Counters> counters_;
};

// The span processor in front of one batch processor: counts what is
// emitted and, when lossless, waits for room.
class AccountedSpanProcessor final : public trace_sdk::SpanProcessor {
public:
    AccountedSpanProcessor(std::unique_ptr<trace_sdk::SpanProcessor> inner, std::shared_ptr<Counters> counters,
                           const ExportOptions& opts)
        : inner_(std::move(inner)), counters_(std::move(counters)), gate_(opts) {}

    std::unique_ptr<trace_sdk::Recordable> MakeRecordable() noexcept override { return inner_->MakeRecordable(); }

    void OnStart(trace_sdk::Recordable& span, const trace_api::SpanContext& parent) noexcept override {
        inner_->OnStart(span, parent);
    }

    void OnEnd(std::unique_ptr<trace_sdk::Recordable>&& span) noexcept override {
        gate_.wait(*counters_);
        counters_->emit();
        inner_->OnEnd(std::move(span));
    }

    bool ForceFlush(microseconds timeout) noexcept override { return inner_->ForceFlush(timeout); }

    bool Shutdown(microseconds timeout) noexcept override {
        gate_.close();
        return inner_->Shutdown(timeout);
    }

private:
    std::unique_ptr<trace_sdk::SpanProcessor> inner_;
    std::shared_ptr<Counters> counters_;
    Gate gate_;
};

// The log processor in front of one batch processor: counts what is emitted
// and, when lossless, waits for room.
class AccountedLogProcessor final : public logs_sdk::LogRecordProcessor {
public:
    AccountedLogProcessor(std::unique_ptr<logs_sdk::LogRecordProcessor> inner, std::shared_ptr<Counters> counters,
                          const ExportOptions& opts)
        : inner_(std::move(inner)), counters_(std::move(counters)), gate_(opts) {}

    std::unique_ptr<logs_sdk::Recordable> MakeRecordable() noexcept override { return inner_->MakeRecordable(); }

    void OnEmit(std::unique_ptr<logs_sdk::Recordable>&& record) noexcept override {
        gate_.wait(*counters_);
        counters_->emit();
        inner_->OnEmit(std::move(record));
    }

    bool ForceFlush(microseconds timeout) noexcept override { return inner_->ForceFlush(timeout); }

    bool Shutdown(microseconds timeout) noexcept override {
        gate_.close();
        return inner_->Shutdown(timeout);
    }

    bool RecordableEnforcesLogRecordLimits() const noexcept override {
        return inner_->RecordableEnforcesLogRecordLimits();
    }

private:
    std::unique_ptr<logs_sdk::LogRecordProcessor> inner_;
    std::shared_ptr<Counters> counters_;
    Gate gate_;
};

// Spreads records over several processors, each with its own exporter,
// round robin: one exporter sends one batch at a time, so n of them send n
// batches at once. Each shard is accounted and gated on its own, so one
// exporter draining slower than the rest cannot overflow its queue.
template <class P>
class Shards {
public:
    explicit Shards(std::vector<std::unique_ptr<P>> shards) : shards_(std::move(shards)) {}

protected:
    P& pick() noexcept { return *shards_[next_.fetch_add(1, std::memory_order_relaxed) % shards_.size()]; }

    template <class F>
    bool each(F f) noexcept {
        bool ok = true;
        for (auto& p : shards_) ok = f(*p) && ok;
        return ok;
    }

    std::vector<std::unique_ptr<P>> shards_;
    std::atomic<size_t> next_{0};
};

class SpanShards final : public trace_sdk::SpanProcessor, Shards<trace_sdk::SpanProcessor> {
public:
    using Shards::Shards;

    std::unique_ptr<trace_sdk::Recordable> MakeRecordable() noexcept override { return shards_[0]->MakeRecordable(); }
    void OnStart(trace_sdk::Recordable&, const trace_api::SpanContext&) noexcept override {}
    void OnEnd(std::unique_ptr<trace_sdk::Recordable>&& span) noexcept override { pick().OnEnd(std::move(span)); }
    bool ForceFlush(microseconds t) noexcept override {
        return each([t](auto& p) { return p.ForceFlush(t); });
    }
    bool Shutdown(microseconds t) noexcept override {
        return each([t](auto& p) { return p.Shutdown(t); });
    }
};

class LogShards final : public logs_sdk::LogRecordProcessor, Shards<logs_sdk::LogRecordProcessor> {
public:
    using Shards::Shards;

    std::unique_ptr<logs_sdk::Recordable> MakeRecordable() noexcept override { return shards_[0]->MakeRecordable(); }
    void OnEmit(std::unique_ptr<logs_sdk::Recordable>&& record) noexcept override { pick().OnEmit(std::move(record)); }
    bool ForceFlush(microseconds t) noexcept override {
        return each([t](auto& p) { return p.ForceFlush(t); });
    }
    bool Shutdown(microseconds t) noexcept override {
        return each([t](auto& p) { return p.Shutdown(t); });
    }
    bool RecordableEnforcesLogRecordLimits() const noexcept override {
        return shards_[0]->RecordableEnforcesLogRecordLimits();
    }
};

}  // namespace

/**
 * @brief Initializes the OpenTelemetry exporter for a service.
 *
 * @param service_opts Service name and version used for telemetry resource attributes.
 * @param endpoint Base OTLP endpoint used to configure telemetry exporters.
 */
OtelExporter::OtelExporter(const ServiceOptions& service_opts, const std::string& endpoint,
                           const ExportOptions& export_options)
    : service_name_(service_opts.name)
    , service_version_(service_opts.version)
    , endpoint_(endpoint)
    , export_options_(export_options) {
    auto resource_attributes = resource::ResourceAttributes{
        {"service.name", service_name_},
        {"service.version", service_version_},
    };
    auto resource = resource::Resource::Create(resource_attributes);

    init_tracer(resource);
    init_logger(resource);
    init_metrics(resource);

    enabled_ = true;
}

OtelExporter::~OtelExporter() {
    shutdown();
}

void OtelExporter::init_tracer(const resource::Resource& resource) {
    const auto& ex = export_options_;
    std::vector<std::unique_ptr<trace_sdk::SpanProcessor>> shards;
    for (size_t i = 0; i < std::max<size_t>(ex.exporters, 1); ++i) {
        otlp::OtlpHttpExporterOptions opts;
        opts.url = endpoint_ + "/v1/traces";
        auto counters = std::make_shared<Counters>();
        counters->total = spans_;

        trace_sdk::BatchSpanProcessorOptions processor_opts;
        processor_opts.max_queue_size = ex.queue_size;
        processor_opts.max_export_batch_size = std::min(ex.batch_size, ex.queue_size);
        auto batch = std::make_unique<trace_sdk::BatchSpanProcessor>(
            std::make_unique<CountingSpanExporter>(std::make_unique<otlp::OtlpHttpExporter>(opts), counters),
            processor_opts);
        shards.push_back(std::make_unique<AccountedSpanProcessor>(std::move(batch), counters, ex));
    }
    std::unique_ptr<trace_sdk::SpanProcessor> processor =
        shards.size() == 1 ? std::move(shards[0]) : std::make_unique<SpanShards>(std::move(shards));

    tracer_provider_ = std::make_shared<trace_sdk::TracerProvider>(std::move(processor), resource);
    tracer_ = tracer_provider_->GetTracer(service_name_, service_version_);

    trace_api::Provider::SetTracerProvider(
        opentelemetry::nostd::shared_ptr<trace_api::TracerProvider>(tracer_provider_));
}

void OtelExporter::init_logger(const resource::Resource& resource) {
    const auto& ex = export_options_;
    std::vector<std::unique_ptr<logs_sdk::LogRecordProcessor>> shards;
    for (size_t i = 0; i < std::max<size_t>(ex.exporters, 1); ++i) {
        otlp::OtlpHttpLogRecordExporterOptions opts;
        opts.url = endpoint_ + "/v1/logs";
        auto counters = std::make_shared<Counters>();
        counters->total = logs_;

        logs_sdk::BatchLogRecordProcessorOptions processor_opts;
        processor_opts.max_queue_size = ex.queue_size;
        processor_opts.max_export_batch_size = std::min(ex.batch_size, ex.queue_size);
        auto batch = std::make_unique<logs_sdk::BatchLogRecordProcessor>(
            std::make_unique<CountingLogExporter>(std::make_unique<otlp::OtlpHttpLogRecordExporter>(opts), counters),
            processor_opts);
        shards.push_back(std::make_unique<AccountedLogProcessor>(std::move(batch), counters, ex));
    }
    std::unique_ptr<logs_sdk::LogRecordProcessor> processor =
        shards.size() == 1 ? std::move(shards[0]) : std::make_unique<LogShards>(std::move(shards));

    logger_provider_ = std::make_shared<logs_sdk::LoggerProvider>(std::move(processor), resource);
    logger_ = logger_provider_->GetLogger(service_name_, service_version_);

    logs_api::Provider::SetLoggerProvider(opentelemetry::nostd::shared_ptr<logs_api::LoggerProvider>(logger_provider_));
}

void OtelExporter::init_metrics(const resource::Resource& resource) {
    otlp::OtlpHttpMetricExporterOptions opts;
    opts.url = endpoint_ + "/v1/metrics";

    auto exporter = std::make_unique<otlp::OtlpHttpMetricExporter>(opts);

    metrics_sdk::PeriodicExportingMetricReaderOptions reader_opts;
    reader_opts.export_interval_millis = std::chrono::milliseconds(1000);
    reader_opts.export_timeout_millis = std::chrono::milliseconds(500);

    auto reader = std::make_unique<metrics_sdk::PeriodicExportingMetricReader>(std::move(exporter), reader_opts);

    // With the service's resource, as the tracer and logger providers have;
    // without it every metric reached the backend as unknown_service.
    meter_provider_ =
        std::make_shared<metrics_sdk::MeterProvider>(std::make_unique<metrics_sdk::ViewRegistry>(), resource);
    meter_provider_->AddMetricReader(std::move(reader));

    metrics_api::Provider::SetMeterProvider(
        opentelemetry::nostd::shared_ptr<metrics_api::MeterProvider>(meter_provider_));
}

opentelemetry::nostd::shared_ptr<trace_api::Tracer> OtelExporter::get_tracer() {
    return tracer_;
}

opentelemetry::nostd::shared_ptr<logs_api::Logger> OtelExporter::get_logger() {
    return logger_;
}

opentelemetry::nostd::shared_ptr<metrics_api::Meter> OtelExporter::get_meter() {
    if (!meter_provider_) return opentelemetry::nostd::shared_ptr<metrics_api::Meter>();
    return meter_provider_->GetMeter(service_name_, service_version_);
}

Stats OtelExporter::stats() const {
    return Stats{logs_->stats(), spans_->stats()};
}

/**
 * @brief Shuts down all initialized OpenTelemetry providers and disables the exporter.
 */
void OtelExporter::shutdown() {
    if (!enabled_) return;

    if (tracer_provider_) {
        tracer_provider_->Shutdown();
    }
    if (logger_provider_) {
        logger_provider_->Shutdown();
    }
    if (meter_provider_) {
        meter_provider_->Shutdown();
    }

    enabled_ = false;
}

}  // namespace telemetry::otel

#endif  // TELEMETRY_USE_OTEL
