// The shipping service of the cross-language tracing demo (`just crosslang`):
// it quotes a shipping rate for an order, prints its label, and hands it to
// notification. The cpp-httplib handler opens a ServerScope, which continues
// the trace the caller sent, and calls the next service under a ClientScope,
// which carries it on. Neither side has an inject or extract of its own.
//
// Each step is a span of its own under the request's, logs under it (so the
// logs carry its trace and span id) and counts or measures what it did, so
// the journey shows in Grafana as one trace with its logs and metrics.
//
// It listens on a free port and prints {"listening": <port>}; GET /hop
// answers with this hop's view of the trace, followed by the hops after it
// (CROSSLANG_NEXT, a URL, when there is one). Set
// TELEMETRY_DEMO_OTLP=localhost:4317 to export the spans to a collector.
#include "telemetry/telemetry.hpp"
#include "telemetry/tracing/propagation.hpp"

#include <httplib.h>
#include <nlohmann/json.hpp>

#include <csignal>
#include <chrono>
#include <cstdlib>
#include <iostream>
#include <pthread.h>
#include <string>
#include <thread>

using telemetry::tracing::ClientScope;
using telemetry::tracing::ServerScope;

int main() {
    // Stopped with SIGTERM or Ctrl-C: stop serving, then close Telemetry,
    // which exports the spans still queued. The signals are blocked before
    // any thread starts (Telemetry's exporters start some), so every thread
    // inherits the mask and only the waiting thread below receives them.
    sigset_t signals;
    sigemptyset(&signals);
    sigaddset(&signals, SIGTERM);
    sigaddset(&signals, SIGINT);
    pthread_sigmask(SIG_BLOCK, &signals, nullptr);

    auto builder = telemetry::Telemetry::builder("shipping-cpp", "1.0.0").with_console(false);
    if (const char* otlp = std::getenv("TELEMETRY_DEMO_OTLP"); otlp != nullptr && *otlp != '\0') {
        // This SDK exports OTLP over HTTP: the collector's gRPC port (4317)
        // becomes its HTTP one (4318), as in the other SDKs' HTTP fallback.
        std::string endpoint(otlp);
        const auto colon = endpoint.rfind(':');
        const std::string host = endpoint.substr(0, colon);
        int port = colon == std::string::npos ? 4318 : std::stoi(endpoint.substr(colon + 1));
        builder.with_otlp(host, static_cast<uint16_t>(port == 4317 ? 4318 : port));
    }
    auto telemetry = builder.build();
    auto& tracer = telemetry.tracer();
    auto& metrics = telemetry.metrics();
    const char* next = std::getenv("CROSSLANG_NEXT");

    httplib::Server server;
    server.Get("/hop", [&](const httplib::Request& req, httplib::Response& res) {
        ServerScope scope(tracer, "GET /hop", req.headers);
        const std::string order = req.get_param_value("order");

        {
            telemetry::tracing::ScopedSpan quote(tracer, "shipping.rate_quote");
            quote.set_attribute("order.id", order);
            std::this_thread::sleep_for(std::chrono::milliseconds(9));
            metrics.histogram("shipping_quote", 4.99);
            TELEMETRY_LOG_INFO(("quoted 4.99 to ship " + order).c_str());
        }
        {
            telemetry::tracing::ScopedSpan label(tracer, "shipping.label");
            label.set_attribute("order.id", order);
            std::this_thread::sleep_for(std::chrono::milliseconds(14));
            metrics.counter("shipping_labels_total");
            TELEMETRY_LOG_INFO(("label printed for " + order).c_str());
        }

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
            auto downstream = client.Get(httplib::append_query_params("/hop", req.params), headers);
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

    std::thread stopper([&] {
        int signal = 0;
        sigwait(&signals, &signal);
        server.stop();
    });

    const int port = server.bind_to_any_port("127.0.0.1");
    std::cout << nlohmann::json{{"listening", port}}.dump() << std::endl;
    server.listen_after_bind();
    stopper.join();
    telemetry.close();
    return 0;
}
