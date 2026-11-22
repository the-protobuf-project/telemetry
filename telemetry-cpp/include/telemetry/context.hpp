#pragma once

#include <map>
#include <string>
#include <utility>

namespace telemetry {

/// The trace a thread is working on: the span currently in scope, set by
/// tracing::Span::attach (and ScopedSpan / TELEMETRY_SPAN) and read by the
/// logger, so a log record written under a span carries its trace and span
/// id — distributed logging: a trace finds the logs of every service it
/// crossed.
struct TraceContext {
    std::string trace_id;  ///< 32 lowercase hex digits
    std::string span_id;   ///< 16 lowercase hex digits
    /// W3C baggage of the span in scope: spans started under it carry it on.
    std::map<std::string, std::string> baggage;

    bool valid() const { return trace_id.size() == 32 && span_id.size() == 16; }
};

namespace detail {
inline thread_local TraceContext current_trace_context;
}  // namespace detail

/// The trace context of the span in scope on this thread; not valid() when none is.
inline const TraceContext& current_trace_context() {
    return detail::current_trace_context;
}

/// Makes a trace context current on this thread until destroyed, then
/// restores the one before it, so scopes nest.
class ScopedTraceContext {
public:
    explicit ScopedTraceContext(TraceContext context)
        : previous_(std::exchange(detail::current_trace_context, std::move(context))) {}
    ~ScopedTraceContext() { detail::current_trace_context = std::move(previous_); }

    ScopedTraceContext(const ScopedTraceContext&) = delete;
    ScopedTraceContext& operator=(const ScopedTraceContext&) = delete;

private:
    TraceContext previous_;
};

}  // namespace telemetry
