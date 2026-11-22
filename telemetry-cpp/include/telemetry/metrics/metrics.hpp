#pragma once

#include "telemetry/platform.hpp"
#include "telemetry/config.hpp"

#include <atomic>
#include <cstdint>
#include <functional>
#include <map>
#include <memory>
#include <string>
#include <string_view>
#include <vector>
#if !TELEMETRY_PLATFORM_FREERTOS
#include <shared_mutex>
#endif

namespace telemetry {
namespace mcap {
class McapWriter;
}
namespace otel {
class OtelExporter;
}
}  // namespace telemetry

namespace telemetry::metrics {

enum class MetricType { Counter, Histogram, Gauge };

inline const char* metric_type_to_string(MetricType type) {
    switch (type) {
        case MetricType::Counter:   return "counter";
        case MetricType::Histogram: return "histogram";
        case MetricType::Gauge:     return "gauge";
        default:                    return "unknown";
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

/// Where an instrument also sends what it records, as it records it: a
/// histogram's OpenTelemetry instrument, or nothing. Metrics has OpenTelemetry
/// read counters and gauges when it collects instead.
using Sink = std::function<void(double)>;

class Counter {
public:
    Counter(const std::string& name, const std::string& description = "", Sink sink = {});

    void add(double value = 1.0);
    /// The sum of every increment so far, from every thread.
    double value() const;
    const std::string& name() const { return name_; }

private:
    friend class Metrics;

    // Each thread adds to one of several cells a cache line apart, so threads
    // seldom write the same line. Whole increments go to an integer, which
    // adds faster than a double and exactly.
#if TELEMETRY_PLATFORM_FREERTOS
    struct Cell {
#else
    struct alignas(128) Cell {
#endif
        std::atomic<int64_t> whole{0};
        std::atomic<double> rest{0.0};
    };

    std::string name_;
    std::string description_;
    std::unique_ptr<Cell[]> cells_;
    size_t mask_ = 0;
    Sink sink_;
    // The OpenTelemetry instrument that reads value() when it collects;
    // declared last so it is let go first, before the cells it reads.
    std::shared_ptr<void> observer_;
};

class Histogram {
public:
    Histogram(const std::string& name, const std::string& description = "", const std::vector<double>& buckets = {},
              Sink sink = {});

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
    Sink sink_;
};

class Gauge {
public:
    /// sink is given each change, so a sum behind it holds the gauge's value.
    Gauge(const std::string& name, const std::string& description = "", Sink sink = {});

    void set(double value);
    void increment(double value = 1.0);
    void decrement(double value = 1.0);
    double value() const { return value_.load(std::memory_order_relaxed); }
    const std::string& name() const { return name_; }

private:
    friend class Metrics;

    std::string name_;
    std::string description_;
    std::atomic<double> value_{0.0};
    Sink sink_;
    // The OpenTelemetry instrument that reads value() when it collects.
    std::shared_ptr<void> observer_;
};

class Metrics {
public:
    Metrics(const ServiceOptions& service_opts);
    Metrics(const ServiceOptions& service_opts, std::shared_ptr<mcap::McapWriter> mcap_writer
#if TELEMETRY_USE_OTEL
            ,
            otel::OtelExporter* otel_exporter = nullptr
#endif
    );
    ~Metrics();

    Metrics(const Metrics&) = delete;
    Metrics& operator=(const Metrics&) = delete;
    Metrics(Metrics&&) noexcept;
    Metrics& operator=(Metrics&&) noexcept;

    // Each thread remembers the instruments it last used by name, so these
    // find them without a lock or an allocation.
    void counter(std::string_view name, double value = 1.0);
    void histogram(std::string_view name, double value);
    void gauge(std::string_view name, double value);

    template <typename T>
    void record(const T& model) {
        static_assert(std::is_base_of<RecordMetrics, T>::value, "T must derive from RecordMetrics");
        for (const auto& field : model.metric_fields()) {
            record_dynamic(field.name, field.type, field.value);
        }
    }

    /// The instruments by name, made on first use. Each lives as long as this
    /// registry, so a reference kept to one is the cheapest way to record.
    Counter& get_counter(std::string_view name, const std::string& description = "");
    Histogram& get_histogram(std::string_view name, const std::string& description = "",
                             const std::vector<double>& buckets = {});
    Gauge& get_gauge(std::string_view name, const std::string& description = "");

private:
    void record_dynamic(const std::string& name, MetricType type, double value);
    void write_to_mcap(std::string_view name, MetricType type, double value);
    Sink otel_sink(const std::string& name, MetricType type);
    std::shared_ptr<void> otel_observer(const std::string& name, MetricType type, void* instrument);

    // Tells this registry's entries in the per-thread caches apart from those
    // of registries gone or moved from; never reused.
    uint64_t id_;
    std::string service_name_;
    std::shared_ptr<mcap::McapWriter> mcap_writer_;
#if TELEMETRY_USE_OTEL
    otel::OtelExporter* otel_exporter_ = nullptr;
#endif

    // std::less<> finds a name given as a string_view without copying it.
    std::map<std::string, std::unique_ptr<Counter>, std::less<>> counters_;
    std::map<std::string, std::unique_ptr<Histogram>, std::less<>> histograms_;
    std::map<std::string, std::unique_ptr<Gauge>, std::less<>> gauges_;

    // Guards the maps above. Instruments are found far more often than made,
    // so where the platform has one, a reader-writer lock lets threads find
    // them at once.
#if !TELEMETRY_PLATFORM_FREERTOS
    mutable std::shared_mutex instruments_;
#endif
    platform::Mutex mutex_;
};

}  // namespace telemetry::metrics
