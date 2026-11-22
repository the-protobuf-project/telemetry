#pragma once

#include "telemetry/platform.hpp"
#include "telemetry/config.hpp"
#include "telemetry/context.hpp"

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
}  // namespace telemetry

namespace telemetry::tracing {

enum class SpanStatus { Unset, Ok, Error };

inline const char* span_status_to_string(SpanStatus status) {
    switch (status) {
        case SpanStatus::Ok:    return "ok";
        case SpanStatus::Error: return "error";
        case SpanStatus::Unset: return "unset";
        default:                return "unset";
    }
}

/// What side of an interaction a span records, as OpenTelemetry defines it.
enum class SpanKind { Internal, Server, Client, Producer, Consumer };

/// A caller's trace context, read from W3C headers by Tracer::extract: the
/// span a request or message came from, in whatever language it was written.
struct RemoteContext {
    std::string trace_id;  ///< 32 lowercase hex digits; empty when the headers carried none
    std::string span_id;   ///< 16 lowercase hex digits: the caller's span, parent of the next
    bool sampled = true;
    std::map<std::string, std::string> baggage;  ///< W3C baggage the caller sent

    bool valid() const { return trace_id.size() == 32 && span_id.size() == 16; }
};

struct SpanEvent {
    std::string name;
    uint64_t timestamp_ns;
    std::map<std::string, std::string> attributes;
};

class Span {
public:
    Span(const std::string& name, const std::string& trace_id, const std::string& span_id,
         std::shared_ptr<mcap::McapWriter> mcap_writer);
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
    /// The parent span's id: a local span's, or a remote caller's; empty for a root.
    const std::string& parent_span_id() const { return parent_span_id_; }
    /// W3C baggage this span carries on to the services it calls.
    const std::map<std::string, std::string>& baggage() const { return baggage_; }

    /// Adds key=value to the baggage this span, the spans started under it and
    /// the services they call carry (Tracer::inject sends it as the W3C
    /// baggage header), as Go's SetBaggage and the others' set_baggage do.
    /// Set it before attach() for spans started in that scope to see it.
    void set_baggage(const std::string& key, const std::string& value) { baggage_[key] = value; }
    bool is_ended() const { return ended_; }

    /// Makes this span current on this thread until the guard is destroyed:
    /// log records written meanwhile carry its trace and span id.
    ///
    ///     auto span = tracer.start_span("reserve", parent, SpanKind::Server);
    ///     auto current = span.attach();
    ///     logger.info("reserved");  // correlated with the span
    [[nodiscard]] ScopedTraceContext attach() const { return ScopedTraceContext({trace_id_, span_id_, baggage_}); }

private:
    friend class Tracer;
    struct OtelState;  // the OpenTelemetry span this one exports through, when OTLP is on

    void record_attribute(const std::string& key, const std::string& value);

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
    std::map<std::string, std::string> baggage_;
    bool ended_ = false;

    std::shared_ptr<mcap::McapWriter> mcap_writer_;
    std::shared_ptr<OtelState> otel_;
};

class Tracer {
public:
    Tracer(const ServiceOptions& service_opts);
    Tracer(const ServiceOptions& service_opts, std::shared_ptr<mcap::McapWriter> mcap_writer
#if TELEMETRY_USE_OTEL
           ,
           otel::OtelExporter* otel_exporter = nullptr
#endif
    );
    Tracer(const ServiceOptions& service_opts, std::shared_ptr<mcap::McapWriter> mcap_writer,
           const std::string& otlp_endpoint
#if TELEMETRY_USE_OTEL
           ,
           otel::OtelExporter* otel_exporter = nullptr
#endif
    );
    ~Tracer();

    Tracer(const Tracer&) = delete;
    Tracer& operator=(const Tracer&) = delete;
    Tracer(Tracer&&) noexcept;
    Tracer& operator=(Tracer&&) noexcept;

    /// Starts a span under the one in scope on this thread (see
    /// Span::attach), or a new trace when none is.
    Span start_span(const std::string& name);
    Span start_span(const std::string& name, SpanKind kind);
    Span start_span(const std::string& name, const Span& parent, SpanKind kind = SpanKind::Internal);
    /// Starts a span continuing a remote caller's trace (see extract); an
    /// invalid context starts a new trace.
    Span start_span(const std::string& name, const RemoteContext& parent, SpanKind kind = SpanKind::Server);

    /// Writes span's trace context into headers — W3C traceparent and, when
    /// the span carries any, baggage — for the service a request or message
    /// goes to, in any language, to continue the trace.
    void inject(const Span& span, std::map<std::string, std::string>& headers) const;

    /// Reads the trace context a caller sent in headers (header names in any
    /// case). Without a valid traceparent the result is not valid(), and a
    /// span started under it begins a new trace.
    RemoteContext extract(const std::map<std::string, std::string>& headers) const;

    bool is_enabled() const { return enabled_; }
    const std::string& service_name() const { return service_name_; }

private:
    Span make_span(const std::string& name, const std::string& trace_id, const std::string& parent_span_id,
                   bool remote_parent, bool sampled, const std::map<std::string, std::string>& baggage, SpanKind kind);
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

    void set_attribute(const std::string& key, const std::string& value) { span_.set_attribute(key, value); }

    void add_event(const std::string& name) { span_.add_event(name); }

    void set_ok() { span_.set_status(SpanStatus::Ok); }

    void set_error(const std::string& message) { span_.record_error(message); }

private:
    Span span_;
    ScopedTraceContext current_;  // logs inside the scope are correlated with span_
};

}  // namespace telemetry::tracing

// __LINE__ is expanded before it is pasted only through a second macro, so
// each TELEMETRY_SPAN gets a variable of its own and two can share a scope.
#define TELEMETRY_CONCAT_(a, b) a##b
#define TELEMETRY_CONCAT(a, b) TELEMETRY_CONCAT_(a, b)

#define TELEMETRY_SPAN(tracer, name) \
    ::telemetry::tracing::ScopedSpan TELEMETRY_CONCAT(_telemetry_span_, __LINE__)(tracer, name)

#define TELEMETRY_SPAN_OK(tracer, name)                                                          \
    ::telemetry::tracing::ScopedSpan TELEMETRY_CONCAT(_telemetry_span_, __LINE__)(tracer, name); \
    TELEMETRY_CONCAT(_telemetry_span_, __LINE__).set_ok()
