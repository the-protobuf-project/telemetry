#include "telemetry/tracing/tracer.hpp"
#include "telemetry/mcap/writer.hpp"

#if TELEMETRY_USE_OTEL
#include "telemetry/otel/exporter.hpp"
#include <opentelemetry/trace/tracer.h>
#include <opentelemetry/trace/span.h>
#include <opentelemetry/trace/span_context.h>
#endif

#include <algorithm>
#include <cctype>

namespace telemetry::tracing {

#if TELEMETRY_USE_OTEL
// The OpenTelemetry span a Span exports through: its ids are the ones
// propagated, so what a downstream service continues is what was exported.
struct Span::OtelState {
    opentelemetry::nostd::shared_ptr<opentelemetry::trace::Span> span;
};
#else
struct Span::OtelState {};
#endif

namespace {

bool is_lower_hex(const std::string& s) {
    return std::all_of(s.begin(), s.end(), [](char c) {
        return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f');
    });
}

bool all_zero(const std::string& s) {
    return std::all_of(s.begin(), s.end(), [](char c) { return c == '0'; });
}

std::string trim(const std::string& s) {
    auto b = s.find_first_not_of(" \t");
    auto e = s.find_last_not_of(" \t");
    return b == std::string::npos ? "" : s.substr(b, e - b + 1);
}

#if TELEMETRY_USE_OTEL
namespace trace_api = opentelemetry::trace;

template <size_t N>
void hex_to_bytes(const std::string& hex, uint8_t (&out)[N]) {
    for (size_t i = 0; i < N; ++i) {
        out[i] = static_cast<uint8_t>(std::stoul(hex.substr(2 * i, 2), nullptr, 16));
    }
}

trace_api::SpanKind to_otel(SpanKind kind) {
    switch (kind) {
        case SpanKind::Server: return trace_api::SpanKind::kServer;
        case SpanKind::Client: return trace_api::SpanKind::kClient;
        case SpanKind::Producer: return trace_api::SpanKind::kProducer;
        case SpanKind::Consumer: return trace_api::SpanKind::kConsumer;
        default: return trace_api::SpanKind::kInternal;
    }
}
#endif

}  // namespace

/**
 * @brief Initializes a tracing span with its identifiers and start timestamp.
 *
 * @param mcap_writer Optional MCAP writer used to record the span.
 */
Span::Span(const std::string& name, const std::string& trace_id,
           const std::string& span_id, std::shared_ptr<mcap::McapWriter> mcap_writer)
    : name_(name)
    , trace_id_(trace_id)
    , span_id_(span_id)
    , start_time_ns_(platform::get_timestamp_ns())
    , mcap_writer_(std::move(mcap_writer)) {
}

Span::~Span() {
    if (!ended_) {
        end();
    }
}

Span::Span(Span&& other) noexcept
    : name_(std::move(other.name_))
    , trace_id_(std::move(other.trace_id_))
    , span_id_(std::move(other.span_id_))
    , parent_span_id_(std::move(other.parent_span_id_))
    , start_time_ns_(other.start_time_ns_)
    , end_time_ns_(other.end_time_ns_)
    , status_(other.status_)
    , status_description_(std::move(other.status_description_))
    , attributes_(std::move(other.attributes_))
    , events_(std::move(other.events_))
    , baggage_(std::move(other.baggage_))
    , ended_(other.ended_)
    , mcap_writer_(std::move(other.mcap_writer_))
    , otel_(std::move(other.otel_)) {
    other.ended_ = true;
}

Span& Span::operator=(Span&& other) noexcept {
    if (this != &other) {
        if (!ended_) {
            end();
        }
        name_ = std::move(other.name_);
        trace_id_ = std::move(other.trace_id_);
        span_id_ = std::move(other.span_id_);
        parent_span_id_ = std::move(other.parent_span_id_);
        start_time_ns_ = other.start_time_ns_;
        end_time_ns_ = other.end_time_ns_;
        status_ = other.status_;
        status_description_ = std::move(other.status_description_);
        attributes_ = std::move(other.attributes_);
        events_ = std::move(other.events_);
        baggage_ = std::move(other.baggage_);
        ended_ = other.ended_;
        mcap_writer_ = std::move(other.mcap_writer_);
        otel_ = std::move(other.otel_);
        other.ended_ = true;
    }
    return *this;
}

void Span::record_attribute(const std::string& key, const std::string& value) {
    attributes_[key] = value;
}

void Span::set_attribute(const std::string& key, const std::string& value) {
    if (ended_) return;
    record_attribute(key, value);
#if TELEMETRY_USE_OTEL
    if (otel_) otel_->span->SetAttribute(key, value);
#endif
}

void Span::set_attribute(const std::string& key, int64_t value) {
    if (ended_) return;
    record_attribute(key, std::to_string(value));
#if TELEMETRY_USE_OTEL
    if (otel_) otel_->span->SetAttribute(key, value);
#endif
}

void Span::set_attribute(const std::string& key, double value) {
    if (ended_) return;
    record_attribute(key, std::to_string(value));
#if TELEMETRY_USE_OTEL
    if (otel_) otel_->span->SetAttribute(key, value);
#endif
}

void Span::set_attribute(const std::string& key, bool value) {
    if (ended_) return;
    record_attribute(key, value ? "true" : "false");
#if TELEMETRY_USE_OTEL
    if (otel_) otel_->span->SetAttribute(key, value);
#endif
}

void Span::add_event(const std::string& name) {
    add_event(name, {});
}

void Span::add_event(const std::string& name, const std::map<std::string, std::string>& attributes) {
    if (!ended_) {
        SpanEvent event;
        event.name = name;
        event.timestamp_ns = platform::get_timestamp_ns();
        event.attributes = attributes;
        events_.push_back(std::move(event));
#if TELEMETRY_USE_OTEL
        if (otel_) otel_->span->AddEvent(name, attributes);
#endif
    }
}

void Span::set_status(SpanStatus status, const std::string& description) {
    if (!ended_) {
        status_ = status;
        status_description_ = description;
#if TELEMETRY_USE_OTEL
        if (otel_) {
            otel_->span->SetStatus(status == SpanStatus::Ok      ? trace_api::StatusCode::kOk
                                   : status == SpanStatus::Error ? trace_api::StatusCode::kError
                                                                 : trace_api::StatusCode::kUnset,
                                   description);
        }
#endif
    }
}

void Span::record_error(const std::string& error_message) {
    set_status(SpanStatus::Error, error_message);
    add_event("exception", {{"exception.message", error_message}});
}

void Span::end() {
    if (ended_) return;

    end_time_ns_ = platform::get_timestamp_ns();
    ended_ = true;
#if TELEMETRY_USE_OTEL
    // Before this, spans were written to MCAP only: OTLP never saw a C++ span.
    if (otel_) otel_->span->End();
#endif

    if (mcap_writer_) {
        mcap_writer_->write_span(name_, trace_id_, span_id_,
                                  start_time_ns_, end_time_ns_,
                                  span_status_to_string(status_));
    }
}

Tracer::Tracer(const ServiceOptions& service_opts)
    : service_name_(service_opts.name)
    , service_version_(service_opts.version)
    , environment_(environment_to_string(service_opts.environment))
    , mutex_(platform::create_mutex()) {
    std::random_device rd;
    rng_.seed(rd());
}

/**
 * @brief Creates a tracer configured with service metadata and optional exporters.
 *
 * @param service_opts Service name, version, and environment configuration.
 * @param mcap_writer Optional MCAP writer for recording spans.
 * @param otel_exporter Optional OpenTelemetry exporter.
 */
Tracer::Tracer(const ServiceOptions& service_opts,
               std::shared_ptr<mcap::McapWriter> mcap_writer
#if TELEMETRY_USE_OTEL
               , otel::OtelExporter* otel_exporter
#endif
)
    : service_name_(service_opts.name)
    , service_version_(service_opts.version)
    , environment_(environment_to_string(service_opts.environment))
    , mcap_writer_(std::move(mcap_writer))
#if TELEMETRY_USE_OTEL
    , otel_exporter_(otel_exporter)
#endif
    , mutex_(platform::create_mutex()) {
    std::random_device rd;
    rng_.seed(rd());
}

/**
 * @brief Creates a tracer configured with service metadata and optional exporters.
 *
 * @param service_opts Service name, version, and environment metadata.
 * @param mcap_writer Optional writer for recording span data.
 * @param otlp_endpoint Endpoint used for OTLP export.
#if TELEMETRY_USE_OTEL
 * @param otel_exporter Optional OpenTelemetry exporter.
#endif
 */
Tracer::Tracer(const ServiceOptions& service_opts,
               std::shared_ptr<mcap::McapWriter> mcap_writer,
               const std::string& otlp_endpoint
#if TELEMETRY_USE_OTEL
               , otel::OtelExporter* otel_exporter
#endif
)
    : service_name_(service_opts.name)
    , service_version_(service_opts.version)
    , environment_(environment_to_string(service_opts.environment))
    , otlp_endpoint_(otlp_endpoint)
    , mcap_writer_(std::move(mcap_writer))
#if TELEMETRY_USE_OTEL
    , otel_exporter_(otel_exporter)
#endif
    , mutex_(platform::create_mutex()) {
    std::random_device rd;
    rng_.seed(rd());
}

Tracer::~Tracer() {
    platform::destroy_mutex(mutex_);
}

/**
 * @brief Transfers ownership of a tracer's configuration and runtime state.
 *
 * @param other Tracer whose state is transferred to this instance.
 */
Tracer::Tracer(Tracer&& other) noexcept
    : service_name_(std::move(other.service_name_))
    , service_version_(std::move(other.service_version_))
    , environment_(std::move(other.environment_))
    , otlp_endpoint_(std::move(other.otlp_endpoint_))
    , enabled_(other.enabled_)
    , mcap_writer_(std::move(other.mcap_writer_))
#if TELEMETRY_USE_OTEL
    , otel_exporter_(other.otel_exporter_)
#endif
    , rng_(std::move(other.rng_))
    , mutex_(platform::create_mutex()) {
}

/**
 * @brief Move-assigns tracer configuration and state from another tracer.
 *
 * @param other Tracer whose configuration and state are transferred.
 * @return Tracer& This tracer.
 */
Tracer& Tracer::operator=(Tracer&& other) noexcept {
    if (this != &other) {
        platform::destroy_mutex(mutex_);
        service_name_ = std::move(other.service_name_);
        service_version_ = std::move(other.service_version_);
        environment_ = std::move(other.environment_);
        otlp_endpoint_ = std::move(other.otlp_endpoint_);
        enabled_ = other.enabled_;
        mcap_writer_ = std::move(other.mcap_writer_);
#if TELEMETRY_USE_OTEL
        otel_exporter_ = other.otel_exporter_;
#endif
        rng_ = std::move(other.rng_);
        mutex_ = platform::create_mutex();
    }
    return *this;
}

Span Tracer::start_span(const std::string& name) {
    return start_span(name, SpanKind::Internal);
}

// Without an explicit parent, a span continues the trace of the span in
// scope on this thread (Span::attach, ScopedSpan), as in the other SDKs; with
// none in scope it starts a new trace.
Span Tracer::start_span(const std::string& name, SpanKind kind) {
    if (const auto& current = current_trace_context(); current.valid()) {
        return make_span(name, current.trace_id, current.span_id, false, true, {}, kind);
    }
    return make_span(name, "", "", false, true, {}, kind);
}

Span Tracer::start_span(const std::string& name, const Span& parent, SpanKind kind) {
    return make_span(name, parent.trace_id(), parent.span_id(), false, true, parent.baggage(), kind);
}

Span Tracer::start_span(const std::string& name, const RemoteContext& parent, SpanKind kind) {
    if (!parent.valid()) {
        return make_span(name, "", "", false, true, parent.baggage, kind);
    }
    return make_span(name, parent.trace_id, parent.span_id, true, parent.sampled, parent.baggage, kind);
}

Span Tracer::make_span(const std::string& name, const std::string& trace_id,
                       const std::string& parent_span_id, bool remote_parent, bool sampled,
                       const std::map<std::string, std::string>& baggage, SpanKind kind) {
#if TELEMETRY_USE_OTEL
    if (otel_exporter_ && otel_exporter_->is_enabled()) {
        trace_api::StartSpanOptions opts;
        opts.kind = to_otel(kind);
        if (!trace_id.empty() && !parent_span_id.empty()) {
            uint8_t tid[16];
            uint8_t sid[8];
            hex_to_bytes(trace_id, tid);
            hex_to_bytes(parent_span_id, sid);
            opts.parent = trace_api::SpanContext(
                trace_api::TraceId(tid), trace_api::SpanId(sid),
                trace_api::TraceFlags(sampled ? trace_api::TraceFlags::kIsSampled : 0),
                remote_parent);
        }
        auto otel_span = otel_exporter_->get_tracer()->StartSpan(name, opts);
        char tid_hex[32];
        char sid_hex[16];
        otel_span->GetContext().trace_id().ToLowerBase16(tid_hex);
        otel_span->GetContext().span_id().ToLowerBase16(sid_hex);
        Span span(name, std::string(tid_hex, 32), std::string(sid_hex, 16), mcap_writer_);
        span.parent_span_id_ = parent_span_id;
        span.baggage_ = baggage;
        span.otel_ = std::make_shared<Span::OtelState>(Span::OtelState{otel_span});
        return span;
    }
#endif
    (void)remote_parent;
    (void)sampled;
    (void)kind;
    Span span(name, trace_id.empty() ? generate_trace_id() : trace_id, generate_span_id(), mcap_writer_);
    span.parent_span_id_ = parent_span_id;
    span.baggage_ = baggage;
    return span;
}

void Tracer::inject(const Span& span, std::map<std::string, std::string>& headers) const {
    if (span.trace_id().size() != 32 || span.span_id().size() != 16) return;
    headers["traceparent"] = "00-" + span.trace_id() + "-" + span.span_id() + "-01";
    if (!span.baggage().empty()) {
        std::string b;
        for (const auto& [k, v] : span.baggage()) {
            if (!b.empty()) b += ",";
            b += k + "=" + v;
        }
        headers["baggage"] = b;
    }
}

RemoteContext Tracer::extract(const std::map<std::string, std::string>& headers) const {
    RemoteContext out;
    for (const auto& [raw_key, value] : headers) {
        std::string key = raw_key;
        std::transform(key.begin(), key.end(), key.begin(), [](unsigned char c) { return std::tolower(c); });
        if (key == "traceparent") {
            // version-traceid-spanid-flags, e.g. 00-4bf9...4736-00f0...02b7-01
            std::string tp = trim(value);
            if (tp.size() < 55 || tp[2] != '-' || tp[35] != '-' || tp[52] != '-') continue;
            std::string version = tp.substr(0, 2), tid = tp.substr(3, 32), sid = tp.substr(36, 16), flags = tp.substr(53, 2);
            if (version == "ff" || !is_lower_hex(version) || !is_lower_hex(tid) || !is_lower_hex(sid) ||
                !is_lower_hex(flags) || all_zero(tid) || all_zero(sid)) {
                continue;
            }
            out.trace_id = tid;
            out.span_id = sid;
            out.sampled = (std::stoul(flags, nullptr, 16) & 1) != 0;
        } else if (key == "baggage") {
            // key=value[;properties], comma separated
            size_t start = 0;
            while (start <= value.size()) {
                size_t end = value.find(',', start);
                std::string member = value.substr(start, end == std::string::npos ? std::string::npos : end - start);
                member = member.substr(0, member.find(';'));
                if (auto eq = member.find('='); eq != std::string::npos) {
                    std::string k = trim(member.substr(0, eq)), v = trim(member.substr(eq + 1));
                    if (!k.empty()) out.baggage[k] = v;
                }
                if (end == std::string::npos) break;
                start = end + 1;
            }
        }
    }
    return out;
}

std::string Tracer::generate_trace_id() {
    platform::ScopedLock lock(mutex_);
    uint64_t high = rng_();
    uint64_t low = rng_();

    std::ostringstream oss;
    oss << std::hex << std::setfill('0');
    oss << std::setw(16) << high << std::setw(16) << low;
    return oss.str();
}

std::string Tracer::generate_span_id() {
    platform::ScopedLock lock(mutex_);
    uint64_t id = rng_();

    std::ostringstream oss;
    oss << std::hex << std::setfill('0') << std::setw(16) << id;
    return oss.str();
}

ScopedSpan::ScopedSpan(Tracer& tracer, const std::string& name)
    : span_(tracer.start_span(name))
    , current_({span_.trace_id(), span_.span_id()}) {
}

/**
 * @brief Ends the scoped span.
 */
ScopedSpan::~ScopedSpan() {
    span_.end();
}

}  // namespace telemetry::tracing
