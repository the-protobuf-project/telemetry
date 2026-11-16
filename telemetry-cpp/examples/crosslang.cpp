// A hop of the cross-language tracing demo (bench/crosslang-demo.sh):
// continues, in C++, the trace an upstream service started, from the W3C
// headers it sent, and hands it on.
//
// The headers arrive on stdin as a JSON object; in a real service they are
// the incoming HTTP headers, gRPC metadata or message headers.
#include "telemetry/telemetry.hpp"

#include <nlohmann/json.hpp>

#include <iostream>
#include <iterator>
#include <map>
#include <string>

int main() {
    std::string input((std::istreambuf_iterator<char>(std::cin)), std::istreambuf_iterator<char>());
    std::map<std::string, std::string> headers;
    const auto json = nlohmann::json::parse(input);
    for (auto it = json.begin(); it != json.end(); ++it) {
        if (it.value().is_string()) headers[it.key()] = it.value().get<std::string>();
    }

    telemetry::tracing::Tracer tracer(telemetry::ServiceOptions("shipping-cpp", "1.0.0"));

    // The one call that joins the caller's trace.
    auto parent = tracer.extract(headers);
    auto span = tracer.start_span("shipping.schedule", parent, telemetry::tracing::SpanKind::Server);
    span.set_attribute("order.id", std::string("o-42"));

    std::map<std::string, std::string> downstream;
    tracer.inject(span, downstream);

    nlohmann::json out = {
        {"received_traceparent", headers.count("traceparent") ? headers["traceparent"] : ""},
        {"trace_id", span.trace_id()},
        {"span_id", span.span_id()},
        {"parent_span_id", span.parent_span_id()},
        {"downstream_traceparent", downstream["traceparent"]},
    };
    std::cout << out.dump() << std::endl;
    span.end();
    return 0;
}
