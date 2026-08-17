#pragma once

#include "telemetry/platform.hpp"
#include "telemetry/config.hpp"

#include <string>
#include <memory>
#include <vector>
#include <map>
#include <cstdint>
#include <random>
#include <sstream>
#include <iomanip>

namespace telemetry {
namespace mcap {
class McapWriter;
}
namespace otel {
class OtelExporter;
}
}

namespace telemetry::tracing {

enum class SpanStatus {
    Unset,
    Ok,
    Error
};

inline const char* span_status_to_string(SpanStatus status) {
    switch (status) {
        case SpanStatus::Ok: return "ok";
        case SpanStatus::Error: return "error";
        case SpanStatus::Unset: return "unset";
        default: return "unset";
    }
}

struct SpanEvent {
    std::string name;
    uint64_t timestamp_ns;
    std::map<std::string, std::string> attributes;
};

class Span {
public:
    Span(const std::string& name, const std::string& trace_id,
         const std::string& span_id, std::shared_ptr<mcap::McapWriter> mcap_writer);
    ~Span();

    Span(const Span&) = delete;
    Span& operator=(const Span&) = delete;
    Span(Span&&) noexcept;
    Span& operator=(Span&&) noexcept;

    void set_attribute(const std::string& key, const std::string& value);
    void set_attribute(const std::string& key, int64_t value);
    void set_attribute(const std::string& key, double value);
    void set_attribute(const std::string& key, bool value);

    void add_event(const std::string& name);
    void add_event(const std::string& name, const std::map<std::string, std::string>& attributes);

    void set_status(SpanStatus status, const std::string& description = "");
    void record_error(const std::string& error_message);

    void end();

    const std::string& name() const { return name_; }
    const std::string& trace_id() const { return trace_id_; }
    const std::string& span_id() const { return span_id_; }
    bool is_ended() const { return ended_; }

private:
    std::string name_;
    std::string trace_id_;
    std::string span_id_;
    std::string parent_span_id_;
    uint64_t start_time_ns_;
    uint64_t end_time_ns_ = 0;
    SpanStatus status_ = SpanStatus::Unset;
    std::string status_description_;
    std::map<std::string, std::string> attributes_;
    std::vector<SpanEvent> events_;
    bool ended_ = false;

    std::shared_ptr<mcap::McapWriter> mcap_writer_;
};

class Tracer {
public:
    Tracer(const ServiceOptions& service_opts);
    Tracer(const ServiceOptions& service_opts,
           std::shared_ptr<mcap::McapWriter> mcap_writer
#if TELEMETRY_USE_OTEL
           , otel::OtelExporter* otel_exporter = nullptr
#endif
    );
    Tracer(const ServiceOptions& service_opts,
           std::shared_ptr<mcap::McapWriter> mcap_writer,
           const std::string& otlp_endpoint
#if TELEMETRY_USE_OTEL
           , otel::OtelExporter* otel_exporter = nullptr
#endif
    );
    ~Tracer();

    Tracer(const Tracer&) = delete;
    Tracer& operator=(const Tracer&) = delete;
    Tracer(Tracer&&) noexcept;
    Tracer& operator=(Tracer&&) noexcept;

    Span start_span(const std::string& name);
    Span start_span(const std::string& name, const Span& parent);

    bool is_enabled() const { return enabled_; }
    const std::string& service_name() const { return service_name_; }

private:
    std::string generate_trace_id();
    std::string generate_span_id();

    std::string service_name_;
    std::string service_version_;
    std::string environment_;
    std::string otlp_endpoint_;
    bool enabled_ = true;

    std::shared_ptr<mcap::McapWriter> mcap_writer_;
#if TELEMETRY_USE_OTEL
    otel::OtelExporter* otel_exporter_ = nullptr;
#endif

    std::mt19937_64 rng_;
    platform::Mutex mutex_;
};

class ScopedSpan {
public:
    ScopedSpan(Tracer& tracer, const std::string& name);
    ~ScopedSpan();

    ScopedSpan(const ScopedSpan&) = delete;
    ScopedSpan& operator=(const ScopedSpan&) = delete;

    Span& span() { return span_; }

    void set_attribute(const std::string& key, const std::string& value) {
        span_.set_attribute(key, value);
    }

    void add_event(const std::string& name) {
        span_.add_event(name);
    }

    void set_ok() {
        span_.set_status(SpanStatus::Ok);
    }

    void set_error(const std::string& message) {
        span_.record_error(message);
    }

private:
    Span span_;
};

}  // namespace telemetry::tracing

#define TELEMETRY_SPAN(tracer, name) \
    ::telemetry::tracing::ScopedSpan _telemetry_span_##__LINE__(tracer, name)

#define TELEMETRY_SPAN_OK(tracer, name) \
    ::telemetry::tracing::ScopedSpan _telemetry_span_##__LINE__(tracer, name); \
    _telemetry_span_##__LINE__.set_ok()
