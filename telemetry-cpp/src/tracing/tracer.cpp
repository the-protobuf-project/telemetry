#include "telemetry/tracing/tracer.hpp"
#include "telemetry/mcap/writer.hpp"

#if TELEMETRY_USE_OTEL
#include "telemetry/otel/exporter.hpp"
#include <opentelemetry/trace/tracer.h>
#include <opentelemetry/trace/span.h>
#endif

namespace telemetry::tracing {

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
    , ended_(other.ended_)
    , mcap_writer_(std::move(other.mcap_writer_)) {
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
        ended_ = other.ended_;
        mcap_writer_ = std::move(other.mcap_writer_);
        other.ended_ = true;
    }
    return *this;
}

void Span::set_attribute(const std::string& key, const std::string& value) {
    if (!ended_) {
        attributes_[key] = value;
    }
}

void Span::set_attribute(const std::string& key, int64_t value) {
    set_attribute(key, std::to_string(value));
}

void Span::set_attribute(const std::string& key, double value) {
    set_attribute(key, std::to_string(value));
}

void Span::set_attribute(const std::string& key, bool value) {
    set_attribute(key, value ? "true" : "false");
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
    }
}

void Span::set_status(SpanStatus status, const std::string& description) {
    if (!ended_) {
        status_ = status;
        status_description_ = description;
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
    return Span(name, generate_trace_id(), generate_span_id(), mcap_writer_);
}

Span Tracer::start_span(const std::string& name, const Span& parent) {
    return Span(name, parent.trace_id(), generate_span_id(), mcap_writer_);
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
    : span_(tracer.start_span(name)) {
}

ScopedSpan::~ScopedSpan() {
    span_.end();
}

}  // namespace telemetry::tracing
