#include "telemetry/metrics/metrics.hpp"
#include "telemetry/mcap/writer.hpp"

#if TELEMETRY_USE_OTEL
#include "telemetry/otel/exporter.hpp"
#include <opentelemetry/metrics/meter.h>
#endif

namespace telemetry::metrics {

/**
 * @brief Creates a counter metric with the specified name and description.
 *
 * @param name Name of the counter.
 * @param description Description of the counter.
 */
Counter::Counter(const std::string& name, const std::string& description, Sink sink)
    : name_(name)
    , description_(description)
    , sink_(std::move(sink)) {
}

namespace {
// atomic<double>::fetch_add is C++20; this is it for C++17.
double add_to(std::atomic<double>& v, double delta) {
    double old = v.load(std::memory_order_relaxed);
    while (!v.compare_exchange_weak(old, old + delta, std::memory_order_relaxed)) {
    }
    return old + delta;
}
}  // namespace

void Counter::add(double value) {
    add_to(value_, value);
    if (sink_) sink_(value);
}

Histogram::Histogram(const std::string& name, const std::string& description,
                     const std::vector<double>& buckets, Sink sink)
    : name_(name)
    , description_(description)
    , buckets_(buckets.empty() ? std::vector<double>{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0} : buckets)
    , bucket_counts_(buckets_.size() + 1, 0)
    , mutex_(platform::create_mutex())
    , sink_(std::move(sink)) {
}

void Histogram::record(double value) {
    if (sink_) sink_(value);
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

Gauge::Gauge(const std::string& name, const std::string& description, Sink sink)
    : name_(name)
    , description_(description)
    , sink_(std::move(sink)) {
}

void Gauge::set(double value) {
    const double old = value_.exchange(value, std::memory_order_relaxed);
    if (sink_) sink_(value - old);
}

void Gauge::increment(double value) {
    add_to(value_, value);
    if (sink_) sink_(value);
}

void Gauge::decrement(double value) {
    add_to(value_, -value);
    if (sink_) sink_(-value);
}

Metrics::Metrics(const ServiceOptions& service_opts)
    : service_name_(service_opts.name)
    , mutex_(platform::create_mutex()) {
}

/**
 * @brief Initializes metrics for a service.
 *
 * @param service_opts Service configuration containing the service name.
 * @param mcap_writer Optional MCAP writer for metric export.
 * @param otel_exporter Optional OpenTelemetry exporter.
 */
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

/**
 * @brief Constructs a metrics manager by transferring state from another instance.
 *
 * @param other Metrics manager whose service configuration, exporters, and metric collections are transferred.
 */
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

/**
 * @brief Replaces this metrics registry with the state of another registry.
 *
 * @param other Registry whose service configuration, exporters, and metrics are transferred.
 * @return Metrics& Reference to this registry.
 */
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

/**
 * @brief Adds a value to the named counter metric.
 *
 * @param name Name of the counter metric.
 * @param value Amount to add to the counter.
 */
void Metrics::counter(const std::string& name, double value) {
    get_counter(name).add(value);
    write_to_mcap(name, MetricType::Counter, value);
}

/**
 * @brief Records a value for a named histogram metric.
 *
 * @param name Name of the histogram metric.
 * @param value Value to record.
 */
void Metrics::histogram(const std::string& name, double value) {
    get_histogram(name).record(value);
    write_to_mcap(name, MetricType::Histogram, value);
}

/**
 * @brief Sets the named gauge to the specified value.
 *
 * @param name Name of the gauge.
 * @param value New gauge value.
 */
void Metrics::gauge(const std::string& name, double value) {
    get_gauge(name).set(value);
    write_to_mcap(name, MetricType::Gauge, value);
}

namespace {
// Finds name in instruments, or makes it with make; found under a shared
// lock, made under an exclusive one.
template <class T, class Lock, class Make>
T& find_or_make(std::map<std::string, std::unique_ptr<T>>& instruments, const std::string& name,
                Lock& lock, Make make) {
#if !TELEMETRY_PLATFORM_FREERTOS
    {
        std::shared_lock<std::shared_mutex> read(lock);
        if (auto it = instruments.find(name); it != instruments.end()) return *it->second;
    }
    std::unique_lock<std::shared_mutex> write(lock);
#else
    platform::ScopedLock write(lock);
#endif
    auto it = instruments.find(name);
    if (it == instruments.end()) it = instruments.emplace(name, make()).first;
    return *it->second;
}
}  // namespace

#if !TELEMETRY_PLATFORM_FREERTOS
#define TELEMETRY_INSTRUMENTS_LOCK instruments_
#else
#define TELEMETRY_INSTRUMENTS_LOCK mutex_
#endif

Counter& Metrics::get_counter(const std::string& name, const std::string& description) {
    return find_or_make(counters_, name, TELEMETRY_INSTRUMENTS_LOCK, [&] {
        return std::make_unique<Counter>(name, description, otel_sink(name, MetricType::Counter));
    });
}

Histogram& Metrics::get_histogram(const std::string& name, const std::string& description,
                                   const std::vector<double>& buckets) {
    return find_or_make(histograms_, name, TELEMETRY_INSTRUMENTS_LOCK, [&] {
        return std::make_unique<Histogram>(name, description, buckets, otel_sink(name, MetricType::Histogram));
    });
}

Gauge& Metrics::get_gauge(const std::string& name, const std::string& description) {
    return find_or_make(gauges_, name, TELEMETRY_INSTRUMENTS_LOCK, [&] {
        return std::make_unique<Gauge>(name, description, otel_sink(name, MetricType::Gauge));
    });
}

#undef TELEMETRY_INSTRUMENTS_LOCK

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

/**
 * @brief Writes a metric value to the configured MCAP writer.
 *
 * @param name Metric name.
 * @param type Metric type.
 * @param value Metric value.
 */
void Metrics::write_to_mcap(const std::string& name, MetricType type, double value) {
    if (!mcap_writer_) return;
    mcap_writer_->write_metric(name, metric_type_to_string(type), value,
                                platform::get_timestamp_ns());
}

/**
 * @brief The OpenTelemetry instrument a local one also records to, made once
 * with it; empty without an exporter.
 *
 * @param name Metric name.
 * @param type Metric type determining the OpenTelemetry instrument.
 */
Sink Metrics::otel_sink(const std::string& name, MetricType type) {
#if TELEMETRY_USE_OTEL
    if (!otel_exporter_) return {};
    auto meter = otel_exporter_->get_meter();
    if (!meter) return {};
    switch (type) {
        case MetricType::Counter: {
            std::shared_ptr<opentelemetry::metrics::Counter<double>> c(meter->CreateDoubleCounter(name).release());
            return [c](double v) { c->Add(v); };
        }
        case MetricType::Histogram: {
            std::shared_ptr<opentelemetry::metrics::Histogram<double>> h(
                meter->CreateDoubleHistogram(name).release());
            return [h](double v) { h->Record(v, opentelemetry::context::Context{}); };
        }
        case MetricType::Gauge: {
            // An up-down counter given each change holds the gauge's value
            // (the synchronous gauge needs the newer OpenTelemetry ABI).
            std::shared_ptr<opentelemetry::metrics::UpDownCounter<double>> g(
                meter->CreateDoubleUpDownCounter(name).release());
            return [g](double v) { g->Add(v); };
        }
    }
#else
    (void)name;
    (void)type;
#endif
    return {};
}

}  // namespace telemetry::metrics
