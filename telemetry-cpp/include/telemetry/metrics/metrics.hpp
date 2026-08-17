#pragma once

#include "telemetry/platform.hpp"
#include "telemetry/config.hpp"

#include <string>
#include <memory>
#include <map>
#include <vector>
#include <cstdint>

namespace telemetry {
namespace mcap {
class McapWriter;
}
namespace otel {
class OtelExporter;
}
}

namespace telemetry::metrics {

enum class MetricType {
    Counter,
    Histogram,
    Gauge
};

inline const char* metric_type_to_string(MetricType type) {
    switch (type) {
        case MetricType::Counter: return "counter";
        case MetricType::Histogram: return "histogram";
        case MetricType::Gauge: return "gauge";
        default: return "unknown";
    }
}

struct MetricField {
    std::string name;
    MetricType type;
    std::string description;
    double value;
};

class RecordMetrics {
public:
    virtual ~RecordMetrics() = default;
    virtual std::vector<MetricField> metric_fields() const = 0;
};

class Counter {
public:
    Counter(const std::string& name, const std::string& description = "");

    void add(double value = 1.0);
    double value() const { return value_; }
    const std::string& name() const { return name_; }

private:
    std::string name_;
    std::string description_;
    double value_ = 0.0;
    platform::Mutex mutex_;
};

class Histogram {
public:
    Histogram(const std::string& name, const std::string& description = "",
              const std::vector<double>& buckets = {});

    void record(double value);
    uint64_t count() const { return count_; }
    double sum() const { return sum_; }
    const std::string& name() const { return name_; }

private:
    std::string name_;
    std::string description_;
    std::vector<double> buckets_;
    std::vector<uint64_t> bucket_counts_;
    uint64_t count_ = 0;
    double sum_ = 0.0;
    platform::Mutex mutex_;
};

class Gauge {
public:
    Gauge(const std::string& name, const std::string& description = "");

    void set(double value);
    void increment(double value = 1.0);
    void decrement(double value = 1.0);
    double value() const { return value_; }
    const std::string& name() const { return name_; }

private:
    std::string name_;
    std::string description_;
    double value_ = 0.0;
    platform::Mutex mutex_;
};

class Metrics {
public:
    Metrics(const ServiceOptions& service_opts);
    Metrics(const ServiceOptions& service_opts,
            std::shared_ptr<mcap::McapWriter> mcap_writer
#if TELEMETRY_USE_OTEL
            , otel::OtelExporter* otel_exporter = nullptr
#endif
    );
    ~Metrics();

    Metrics(const Metrics&) = delete;
    Metrics& operator=(const Metrics&) = delete;
    Metrics(Metrics&&) noexcept;
    Metrics& operator=(Metrics&&) noexcept;

    void counter(const std::string& name, double value = 1.0);
    void histogram(const std::string& name, double value);
    void gauge(const std::string& name, double value);

    template<typename T>
    void record(const T& model) {
        static_assert(std::is_base_of<RecordMetrics, T>::value,
                      "T must derive from RecordMetrics");
        for (const auto& field : model.metric_fields()) {
            record_dynamic(field.name, field.type, field.value);
        }
    }

    Counter& get_counter(const std::string& name, const std::string& description = "");
    Histogram& get_histogram(const std::string& name, const std::string& description = "",
                             const std::vector<double>& buckets = {});
    Gauge& get_gauge(const std::string& name, const std::string& description = "");

private:
    void record_dynamic(const std::string& name, MetricType type, double value);
    void write_to_mcap(const std::string& name, MetricType type, double value);
#if TELEMETRY_USE_OTEL
    void write_to_otel(const std::string& name, MetricType type, double value);
#endif

    std::string service_name_;
    std::shared_ptr<mcap::McapWriter> mcap_writer_;
#if TELEMETRY_USE_OTEL
    otel::OtelExporter* otel_exporter_ = nullptr;
#endif

    std::map<std::string, std::unique_ptr<Counter>> counters_;
    std::map<std::string, std::unique_ptr<Histogram>> histograms_;
    std::map<std::string, std::unique_ptr<Gauge>> gauges_;

    platform::Mutex mutex_;
};

}  // namespace telemetry::metrics
