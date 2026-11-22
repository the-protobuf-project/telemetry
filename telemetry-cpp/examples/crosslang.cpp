// The shipping service of the cross-language tracing demo (`just crosslang`):
// a cpp-httplib server whose handler opens a ServerScope, which continues the
// trace the caller sent, and calls the next service under a ClientScope,
// which carries it on. Neither side has an inject or extract of its own.
//
// It listens on a free port and prints {"listening": <port>}; GET /hop
// answers with this hop's view of the trace, followed by the hops after it
// (CROSSLANG_NEXT, a URL, when there is one).
#include "telemetry/telemetry.hpp"
#include "telemetry/tracing/propagation.hpp"

#include <httplib.h>
#include <nlohmann/json.hpp>

#include <cstdlib>
#include <iostream>
#include <string>

using telemetry::tracing::ClientScope;
using telemetry::tracing::ServerScope;

int main() {
    telemetry::tracing::Tracer tracer(telemetry::ServiceOptions("shipping-cpp", "1.0.0"));
    const char* next = std::getenv("CROSSLANG_NEXT");

    httplib::Server server;
    server.Get("/hop", [&](const httplib::Request& req, httplib::Response& res) {
        ServerScope scope(tracer, "GET /hop", req.headers);
        nlohmann::json hops = nlohmann::json::array();
        hops.push_back({
            {"service", "shipping-cpp"},
            {"received_traceparent", req.get_header_value("traceparent")},
            {"trace_id", scope.span().trace_id()},
            {"span_id", scope.span().span_id()},
            {"parent_span_id", scope.span().parent_span_id()},
        });
        if (next != nullptr && *next != '\0') {
            httplib::Headers headers;
            ClientScope call(tracer, "GET", [&](const std::string& k, const std::string& v) { headers.emplace(k, v); });
            httplib::Client client(next);
            auto downstream = client.Get("/hop", headers);
            if (!downstream || downstream->status != 200) {
                call.span().record_error("the next service did not answer");
                scope.span().record_error("the next service did not answer");
                res.status = 502;
                return;
            }
            for (auto& hop : nlohmann::json::parse(downstream->body)) hops.push_back(hop);
        }
        res.set_content(hops.dump(), "application/json");
    });

    const int port = server.bind_to_any_port("127.0.0.1");
    std::cout << nlohmann::json{{"listening", port}}.dump() << std::endl;
    server.listen_after_bind();
    return 0;
}
