#include "telemetry/metrics/metrics.hpp"
#include "telemetry/mcap/writer.hpp"

#if TELEMETRY_USE_OTEL
#include "telemetry/otel/exporter.hpp"
#include <opentelemetry/metrics/meter.h>
#endif

#include <cstdint>
#include <cstring>
#include <iterator>
#if !TELEMETRY_PLATFORM_FREERTOS
#include <thread>
#endif

namespace telemetry::metrics {

namespace {

// How many cells a counter spreads its threads over: one per core, rounded
// up to a power of two so a thread's slot picks one with a mask.
size_t cell_count() {
#if TELEMETRY_PLATFORM_FREERTOS
    return 1;
#else
    const size_t cores = std::thread::hardware_concurrency();
    size_t n = 1;
    while (n < cores && n < 64) n <<= 1;
    return n;
#endif
}

#if !TELEMETRY_PLATFORM_FREERTOS
// An instrument a thread found by name, with the registry it came from.
// name points into the instrument, which lives while that registry does.
struct CachedInstrument {
    uint64_t owner = 0;
    MetricType type = MetricType::Counter;
    const char* name = nullptr;
    size_t size = 0;
    void* instrument = nullptr;
};

// Trivially destructible, so it is set up without a guard on each access.
struct ThreadState {
    size_t slot = SIZE_MAX;
    size_t next = 0;
    CachedInstrument cache[8];
};

thread_local ThreadState thread_state;
std::atomic<size_t> next_slot{0};
#endif

// Which of a counter's cells this thread adds to; threads take slots in turn,
// so up to a counter's cell count of them each get their own.
size_t this_thread_slot() {
#if TELEMETRY_PLATFORM_FREERTOS
    return 0;
#else
    size_t& slot = thread_state.slot;
    if (slot == SIZE_MAX) slot = next_slot.fetch_add(1, std::memory_order_relaxed);
    return slot;
#endif
}

std::atomic<uint64_t> next_registry_id{1};

uint64_t new_registry_id() {
    return next_registry_id.fetch_add(1, std::memory_order_relaxed);
}

#if !TELEMETRY_PLATFORM_FREERTOS
// Equal bytes, compared eight at a time inline: names are short, and a call
// to memcmp costs about as much as the rest of a cache hit.
bool same_bytes(const char* a, const char* b, size_t n) {
    uint64_t x = 0;
    uint64_t y = 0;
    while (n >= 8) {
        std::memcpy(&x, a, 8);
        std::memcpy(&y, b, 8);
        if (x != y) return false;
        a += 8;
        b += 8;
        n -= 8;
    }
    for (size_t i = 0; i < n; ++i) {
        if (a[i] != b[i]) return false;
    }
    return true;
}

#if defined(__GNUC__)
#define TELEMETRY_NOINLINE __attribute__((noinline))
#else
#define TELEMETRY_NOINLINE
#endif

// Kept out of line so the cache hit stays small.
template <class T, class Find>
TELEMETRY_NOINLINE T& find_and_cache(ThreadState& state, uint64_t owner, MetricType type, Find& find) {
    T& found = find();
    state.cache[state.next++ % std::size(state.cache)] = {owner, type, found.name().data(), found.name().size(),
                                                          &found};
    return found;
}

#undef TELEMETRY_NOINLINE
#endif

// The instrument of this type named name in registry owner, from this
// thread's cache, else from find, which is then cached.
template <class T, class Find>
T& cached(uint64_t owner, MetricType type, std::string_view name, Find find) {
#if !TELEMETRY_PLATFORM_FREERTOS
    ThreadState& state = thread_state;
    for (const auto& entry : state.cache) {
        if (entry.owner == owner && entry.type == type && entry.size == name.size() &&
            same_bytes(entry.name, name.data(), name.size())) {
            return *static_cast<T*>(entry.instrument);
        }
    }
    return find_and_cache<T>(state, owner, type, find);
#else
    (void)owner;
    (void)type;
    (void)name;
    return find();
#endif
}

}  // namespace

/**
 * @brief Creates a counter metric with the specified name and description.
 *
 * @param name Name of the counter.
 * @param description Description of the counter.
 */
Counter::Counter(const std::string& name, const std::string& description, Sink sink)
    : name_(name)
    , description_(description)
    , cells_(new Cell[cell_count()])
    , mask_(cell_count() - 1)
    , sink_(std::move(sink)) {}

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
    Cell& cell = cells_[this_thread_slot() & mask_];
    // A double holds every whole number up to 2^53 exactly; the range check
    // comes first so the cast is defined.
    constexpr double exact = 9007199254740992.0;
    if (value >= -exact && value <= exact && value == static_cast<double>(static_cast<int64_t>(value))) {
        cell.whole.fetch_add(static_cast<int64_t>(value), std::memory_order_relaxed);
    } else {
        add_to(cell.rest, value);
    }
    if (sink_) sink_(value);
}

double Counter::value() const {
    int64_t whole = 0;
    double rest = 0.0;
    for (size_t i = 0; i <= mask_; ++i) {
        whole += cells_[i].whole.load(std::memory_order_relaxed);
        rest += cells_[i].rest.load(std::memory_order_relaxed);
    }
    return static_cast<double>(whole) + rest;
}

Histogram::Histogram(const std::string& name, const std::string& description, const std::vector<double>& buckets,
                     Sink sink)
    : name_(name)
    , description_(description)
    , buckets_(buckets.empty() ? std::vector<double>{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0}
                               : buckets)
    , bucket_counts_(buckets_.size() + 1, 0)
    , mutex_(platform::create_mutex())
    , sink_(std::move(sink)) {}

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
    : name_(name), description_(description), sink_(std::move(sink)) {}

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
    : id_(new_registry_id()), service_name_(service_opts.name), mutex_(platform::create_mutex()) {}

/**
 * @brief Initializes metrics for a service.
 *
 * @param service_opts Service configuration containing the service name.
 * @param mcap_writer Optional MCAP writer for metric export.
 * @param otel_exporter Optional OpenTelemetry exporter.
 */
Metrics::Metrics(const ServiceOptions& service_opts, std::shared_ptr<mcap::McapWriter> mcap_writer
#if TELEMETRY_USE_OTEL
                 ,
                 otel::OtelExporter* otel_exporter
#endif
                 )
    : id_(new_registry_id())
    , service_name_(service_opts.name)
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
    : id_(new_registry_id())
    , service_name_(std::move(other.service_name_))
    , mcap_writer_(std::move(other.mcap_writer_))
#if TELEMETRY_USE_OTEL
    , otel_exporter_(other.otel_exporter_)
#endif
    , counters_(std::move(other.counters_))
    , histograms_(std::move(other.histograms_))
    , gauges_(std::move(other.gauges_))
    , mutex_(platform::create_mutex()) {
    // Threads cached other's instruments, which are this registry's now.
    other.id_ = new_registry_id();
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
        id_ = new_registry_id();
        other.id_ = new_registry_id();
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
void Metrics::counter(std::string_view name, double value) {
    cached<Counter>(id_, MetricType::Counter, name, [&]() -> Counter& { return get_counter(name); }).add(value);
    if (mcap_writer_) write_to_mcap(name, MetricType::Counter, value);
}

/**
 * @brief Records a value for a named histogram metric.
 *
 * @param name Name of the histogram metric.
 * @param value Value to record.
 */
void Metrics::histogram(std::string_view name, double value) {
    cached<Histogram>(id_, MetricType::Histogram, name, [&]() -> Histogram& {
        return get_histogram(name);
    }).record(value);
    if (mcap_writer_) write_to_mcap(name, MetricType::Histogram, value);
}

/**
 * @brief Sets the named gauge to the specified value.
 *
 * @param name Name of the gauge.
 * @param value New gauge value.
 */
void Metrics::gauge(std::string_view name, double value) {
    cached<Gauge>(id_, MetricType::Gauge, name, [&]() -> Gauge& { return get_gauge(name); }).set(value);
    if (mcap_writer_) write_to_mcap(name, MetricType::Gauge, value);
}

namespace {
// Finds name in instruments, or makes it with make; found under a shared
// lock, made under an exclusive one.
template <class T, class Lock, class Make>
T& find_or_make(std::map<std::string, std::unique_ptr<T>, std::less<>>& instruments, std::string_view name, Lock& lock,
                Make make) {
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
    if (it == instruments.end()) {
        std::string key(name);
        it = instruments.emplace(key, make(key)).first;
    }
    return *it->second;
}
}  // namespace

#if !TELEMETRY_PLATFORM_FREERTOS
#define TELEMETRY_INSTRUMENTS_LOCK instruments_
#else
#define TELEMETRY_INSTRUMENTS_LOCK mutex_
#endif

// Counters and gauges are read by OpenTelemetry when it collects rather than
// sent each change, so recording one costs no call into it.
Counter& Metrics::get_counter(std::string_view name, const std::string& description) {
    return find_or_make(counters_, name, TELEMETRY_INSTRUMENTS_LOCK, [&](const std::string& key) {
        auto counter = std::make_unique<Counter>(key, description);
        counter->observer_ = otel_observer(key, MetricType::Counter, counter.get());
        return counter;
    });
}

Histogram& Metrics::get_histogram(std::string_view name, const std::string& description,
                                  const std::vector<double>& buckets) {
    return find_or_make(histograms_, name, TELEMETRY_INSTRUMENTS_LOCK, [&](const std::string& key) {
        return std::make_unique<Histogram>(key, description, buckets, otel_sink(key, MetricType::Histogram));
    });
}

Gauge& Metrics::get_gauge(std::string_view name, const std::string& description) {
    return find_or_make(gauges_, name, TELEMETRY_INSTRUMENTS_LOCK, [&](const std::string& key) {
        auto gauge = std::make_unique<Gauge>(key, description);
        gauge->observer_ = otel_observer(key, MetricType::Gauge, gauge.get());
        return gauge;
    });
}

#undef TELEMETRY_INSTRUMENTS_LOCK

void Metrics::record_dynamic(const std::string& name, MetricType type, double value) {
    switch (type) {
        case MetricType::Counter:   counter(name, value); break;
        case MetricType::Histogram: histogram(name, value); break;
        case MetricType::Gauge:     gauge(name, value); break;
    }
}

/**
 * @brief Writes a metric value to the configured MCAP writer.
 *
 * @param name Metric name.
 * @param type Metric type.
 * @param value Metric value.
 */
void Metrics::write_to_mcap(std::string_view name, MetricType type, double value) {
    if (!mcap_writer_) return;
    mcap_writer_->write_metric(std::string(name), metric_type_to_string(type), value, platform::get_timestamp_ns());
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
    if (type != MetricType::Histogram) return {};
    std::shared_ptr<opentelemetry::metrics::Histogram<double>> h(meter->CreateDoubleHistogram(name).release());
    return [h](double v) { h->Record(v, opentelemetry::context::Context{}); };
#else
    (void)name;
    (void)type;
    return {};
#endif
}

#if TELEMETRY_USE_OTEL
namespace {
template <class T>
void observe(opentelemetry::metrics::ObserverResult result, void* instrument) {
    using Observer = opentelemetry::nostd::shared_ptr<opentelemetry::metrics::ObserverResultT<double>>;
    if (auto* observer = opentelemetry::nostd::get_if<Observer>(&result)) {
        (*observer)->Observe(static_cast<const T*>(instrument)->value());
    }
}
}  // namespace
#endif

/**
 * @brief Registers a counter or gauge with OpenTelemetry as an asynchronous
 * instrument that reads its value on each collection, the last one at
 * shutdown included; empty without an exporter.
 *
 * Dropping what it returns removes the callback, waiting out a collection
 * that is reading the instrument.
 *
 * @param name Metric name.
 * @param type MetricType::Counter or MetricType::Gauge.
 * @param instrument The Counter or Gauge to read.
 */
std::shared_ptr<void> Metrics::otel_observer(const std::string& name, MetricType type, void* instrument) {
#if TELEMETRY_USE_OTEL
    if (!otel_exporter_) return {};
    auto meter = otel_exporter_->get_meter();
    if (!meter) return {};
    using Instrument = opentelemetry::nostd::shared_ptr<opentelemetry::metrics::ObservableInstrument>;
    const bool counter = type == MetricType::Counter;
    auto observable = std::make_shared<Instrument>(counter ? meter->CreateDoubleObservableCounter(name)
                                                           : meter->CreateDoubleObservableGauge(name));
    const auto callback = counter ? &observe<Counter> : &observe<Gauge>;
    (*observable)->AddCallback(callback, instrument);
    return std::shared_ptr<void>(observable.get(), [observable, callback, instrument](void*) {
        (*observable)->RemoveCallback(callback, instrument);
    });
#else
    (void)name;
    (void)type;
    (void)instrument;
    return {};
#endif
}

}  // namespace telemetry::metrics
