#pragma once

// Trace propagation over HTTP and gRPC, for any library.
//
// A trace crosses a request when the caller sends the W3C traceparent
// header and the callee continues it; every SDK here does both, so a trace
// runs end to end across services in any of their languages. C++ has no
// one HTTP or gRPC library to hook, and this SDK depends on none, so each
// request takes one scope, with the library's own headers:
//
//     // Server: a SERVER span continuing the caller's trace, current until
//     // the scope ends, so spans and logs inside join it.
//     telemetry::tracing::ServerScope scope(tracer, "orders.Orders/Place",
//                                           context->client_metadata());  // gRPC
//     telemetry::tracing::ServerScope scope(tracer, req.method, req.headers);  // cpp-httplib
//
//     // Client: a CLIENT span under the current one; its headers go out
//     // through the setter.
//     grpc::ClientContext ctx;
//     telemetry::tracing::ClientScope call(tracer, "orders.Orders/Place",
//         [&](const std::string& k, const std::string& v) { ctx.AddMetadata(k, v); });
//
// Headers are anything iterated as (name, value) pairs whose parts have
// data() and size(): std::map, std::multimap, cpp-httplib's Headers, and
// gRPC's client_metadata() (grpc::string_ref) among them.

#include "telemetry/tracing/tracer.hpp"

#include <map>
#include <string>
#include <utility>

namespace telemetry::tracing {

namespace detail {

template <class Text>
std::string to_string(const Text& text) {
    return std::string(text.data(), text.size());
}

/// The headers as the tracer reads them; extract matches names in any case.
template <class Headers>
std::map<std::string, std::string> header_map(const Headers& headers) {
    std::map<std::string, std::string> out;
    for (const auto& [name, value] : headers) {
        out.emplace(to_string(name), to_string(value));
    }
    return out;
}

}  // namespace detail

/// The server side of one request: a SERVER span that continues the trace
/// the caller sent in its headers (a new trace without one), current on
/// this thread until the scope ends. Set an error on span() for a failed
/// request; the span ends with the scope.
class ServerScope {
public:
    template <class Headers>
    ServerScope(Tracer& tracer, const std::string& name, const Headers& headers)
        : span_(tracer.start_span(name, tracer.extract(detail::header_map(headers)), SpanKind::Server)),
          current_(span_.attach()) {}

    ServerScope(const ServerScope&) = delete;
    ServerScope& operator=(const ServerScope&) = delete;

    Span& span() { return span_; }

private:
    Span span_;
    ScopedTraceContext current_;
};

/// The client side of one request: a CLIENT span under the span in scope on
/// this thread, whose trace headers (traceparent, and baggage when there is
/// any) go to set(name, value), to add to the outgoing request before it is
/// sent. Current until the scope ends; the span ends with it.
class ClientScope {
public:
    template <class Set>
    ClientScope(Tracer& tracer, const std::string& name, Set&& set)
        : span_(tracer.start_span(name, SpanKind::Client)), current_(span_.attach()) {
        std::map<std::string, std::string> headers;
        tracer.inject(span_, headers);
        for (const auto& [key, value] : headers) set(key, value);
    }

    ClientScope(const ClientScope&) = delete;
    ClientScope& operator=(const ClientScope&) = delete;

    Span& span() { return span_; }

private:
    Span span_;
    ScopedTraceContext current_;
};

}  // namespace telemetry::tracing
