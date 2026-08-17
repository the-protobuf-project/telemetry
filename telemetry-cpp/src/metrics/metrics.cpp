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

/**
 * @brief Creates a metrics registry for a service.
 *
 * @param service_opts Service configuration containing the service name.
 */
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

/**
 * @brief Releases resources held by the metrics registry.
 */
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
#if TELEMETRY_USE_OTEL
    write_to_otel(name, MetricType::Counter, value);
#endif
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
#if TELEMETRY_USE_OTEL
    write_to_otel(name, MetricType::Histogram, value);
#endif
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

/**
 * @brief Records a value in the metric identified by its name and type.
 *
 * @param name Name of the metric to update.
 * @param type Metric type that determines how the value is recorded.
 * @param value Value to record.
 */
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

#if TELEMETRY_USE_OTEL
/**
 * @brief Exports a metric value to OpenTelemetry.
 *
 * @param name Metric name.
 * @param type Metric type determining the OpenTelemetry instrument.
 * @param value Metric value to export.
 */
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
