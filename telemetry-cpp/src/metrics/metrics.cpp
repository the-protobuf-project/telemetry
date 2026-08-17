#include "telemetry/metrics/metrics.hpp"
#include "telemetry/mcap/writer.hpp"

#if TELEMETRY_USE_OTEL
#include "telemetry/otel/exporter.hpp"
#include <opentelemetry/metrics/meter.h>
#endif

namespace telemetry::metrics {

Counter::Counter(const std::string& name, const std::string& description)
    : name_(name)
    , description_(description)
    , mutex_(platform::create_mutex()) {
}

void Counter::add(double value) {
    platform::ScopedLock lock(mutex_);
    value_ += value;
}

Histogram::Histogram(const std::string& name, const std::string& description,
                     const std::vector<double>& buckets)
    : name_(name)
    , description_(description)
    , buckets_(buckets.empty() ? std::vector<double>{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0} : buckets)
    , bucket_counts_(buckets_.size() + 1, 0)
    , mutex_(platform::create_mutex()) {
}

void Histogram::record(double value) {
    platform::ScopedLock lock(mutex_);
    count_++;
    sum_ += value;

    for (size_t i = 0; i < buckets_.size(); ++i) {
        if (value <= buckets_[i]) {
            bucket_counts_[i]++;
            return;
        }
    }
    bucket_counts_.back()++;
}

Gauge::Gauge(const std::string& name, const std::string& description)
    : name_(name)
    , description_(description)
    , mutex_(platform::create_mutex()) {
}

void Gauge::set(double value) {
    platform::ScopedLock lock(mutex_);
    value_ = value;
}

void Gauge::increment(double value) {
    platform::ScopedLock lock(mutex_);
    value_ += value;
}

void Gauge::decrement(double value) {
    platform::ScopedLock lock(mutex_);
    value_ -= value;
}

Metrics::Metrics(const ServiceOptions& service_opts)
    : service_name_(service_opts.name)
    , mutex_(platform::create_mutex()) {
}

Metrics::Metrics(const ServiceOptions& service_opts,
                 std::shared_ptr<mcap::McapWriter> mcap_writer
#if TELEMETRY_USE_OTEL
                 , otel::OtelExporter* otel_exporter
#endif
)
    : service_name_(service_opts.name)
    , mcap_writer_(std::move(mcap_writer))
#if TELEMETRY_USE_OTEL
    , otel_exporter_(otel_exporter)
#endif
    , mutex_(platform::create_mutex()) {
}

Metrics::~Metrics() {
    platform::destroy_mutex(mutex_);
}

Metrics::Metrics(Metrics&& other) noexcept
    : service_name_(std::move(other.service_name_))
    , mcap_writer_(std::move(other.mcap_writer_))
#if TELEMETRY_USE_OTEL
    , otel_exporter_(other.otel_exporter_)
#endif
    , counters_(std::move(other.counters_))
    , histograms_(std::move(other.histograms_))
    , gauges_(std::move(other.gauges_))
    , mutex_(platform::create_mutex()) {
}

Metrics& Metrics::operator=(Metrics&& other) noexcept {
    if (this != &other) {
        platform::destroy_mutex(mutex_);
        service_name_ = std::move(other.service_name_);
        mcap_writer_ = std::move(other.mcap_writer_);
#if TELEMETRY_USE_OTEL
        otel_exporter_ = other.otel_exporter_;
#endif
        counters_ = std::move(other.counters_);
        histograms_ = std::move(other.histograms_);
        gauges_ = std::move(other.gauges_);
        mutex_ = platform::create_mutex();
    }
    return *this;
}

void Metrics::counter(const std::string& name, double value) {
    get_counter(name).add(value);
    write_to_mcap(name, MetricType::Counter, value);
#if TELEMETRY_USE_OTEL
    write_to_otel(name, MetricType::Counter, value);
#endif
}

void Metrics::histogram(const std::string& name, double value) {
    get_histogram(name).record(value);
    write_to_mcap(name, MetricType::Histogram, value);
#if TELEMETRY_USE_OTEL
    write_to_otel(name, MetricType::Histogram, value);
#endif
}

void Metrics::gauge(const std::string& name, double value) {
    get_gauge(name).set(value);
    write_to_mcap(name, MetricType::Gauge, value);
#if TELEMETRY_USE_OTEL
    write_to_otel(name, MetricType::Gauge, value);
#endif
}

Counter& Metrics::get_counter(const std::string& name, const std::string& description) {
    platform::ScopedLock lock(mutex_);
    auto it = counters_.find(name);
    if (it == counters_.end()) {
        auto [inserted, _] = counters_.emplace(name, std::make_unique<Counter>(name, description));
        return *inserted->second;
    }
    return *it->second;
}

Histogram& Metrics::get_histogram(const std::string& name, const std::string& description,
                                   const std::vector<double>& buckets) {
    platform::ScopedLock lock(mutex_);
    auto it = histograms_.find(name);
    if (it == histograms_.end()) {
        auto [inserted, _] = histograms_.emplace(name, std::make_unique<Histogram>(name, description, buckets));
        return *inserted->second;
    }
    return *it->second;
}

Gauge& Metrics::get_gauge(const std::string& name, const std::string& description) {
    platform::ScopedLock lock(mutex_);
    auto it = gauges_.find(name);
    if (it == gauges_.end()) {
        auto [inserted, _] = gauges_.emplace(name, std::make_unique<Gauge>(name, description));
        return *inserted->second;
    }
    return *it->second;
}

void Metrics::record_dynamic(const std::string& name, MetricType type, double value) {
    switch (type) {
        case MetricType::Counter:
            counter(name, value);
            break;
        case MetricType::Histogram:
            histogram(name, value);
            break;
        case MetricType::Gauge:
            gauge(name, value);
            break;
    }
}

void Metrics::write_to_mcap(const std::string& name, MetricType type, double value) {
    if (!mcap_writer_) return;
    mcap_writer_->write_metric(name, metric_type_to_string(type), value,
                                platform::get_timestamp_ns());
}

#if TELEMETRY_USE_OTEL
void Metrics::write_to_otel(const std::string& name, MetricType type, double value) {
    if (!otel_exporter_) return;

    auto meter = otel_exporter_->get_meter();
    if (!meter) return;

    switch (type) {
        case MetricType::Counter: {
            auto counter = meter->CreateDoubleCounter(name);
            counter->Add(value);
            break;
        }
        case MetricType::Histogram: {
            auto histogram = meter->CreateDoubleHistogram(name);
            histogram->Record(value, {});
            break;
        }
        case MetricType::Gauge: {
            // Use UpDownCounter as gauge alternative (CreateDoubleGauge not available in this OTEL version)
            auto gauge = meter->CreateDoubleUpDownCounter(name);
            gauge->Add(value, {});
            break;
        }
    }
}
#endif

}  // namespace telemetry::metrics
