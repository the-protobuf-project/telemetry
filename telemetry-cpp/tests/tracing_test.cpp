// W3C trace-context propagation: a trace a Go, Rust or Python service sent in
// its headers continues here, and goes on to the next service.
#include "telemetry/context.hpp"
#include "telemetry/tracing/propagation.hpp"
#include "telemetry/tracing/tracer.hpp"

#include <catch2/catch_test_macros.hpp>

#include <map>
#include <string>
#include <string_view>

using telemetry::ServiceOptions;
using telemetry::tracing::SpanKind;
using telemetry::tracing::Tracer;

namespace {
// The W3C specification's example: what telemetry-go's Inject sends.
const std::string kTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01";

ServiceOptions service() { return ServiceOptions("tracing-test", "1.0.0"); }
}  // namespace

TEST_CASE("a span under extracted headers continues the caller's trace") {
    Tracer tracer(service());
    auto parent = tracer.extract({{"Traceparent", kTraceparent}, {"baggage", "tenant=acme; ttl=1, region = eu"}});
    REQUIRE(parent.valid());
    CHECK(parent.baggage.at("tenant") == "acme");
    CHECK(parent.baggage.at("region") == "eu");

    auto span = tracer.start_span("inventory.reserve", parent, SpanKind::Server);
    CHECK(span.trace_id() == "4bf92f3577b34da6a3ce929d0e0e4736");
    CHECK(span.parent_span_id() == "00f067aa0ba902b7");
    CHECK(span.span_id() != "00f067aa0ba902b7");

    std::map<std::string, std::string> out;
    tracer.inject(span, out);
    CHECK(out.at("traceparent") == "00-4bf92f3577b34da6a3ce929d0e0e4736-" + span.span_id() + "-01");
    CHECK(out.at("baggage") == "region=eu,tenant=acme");
}

TEST_CASE("a child span keeps the trace and names its parent") {
    Tracer tracer(service());
    auto root = tracer.start_span("root");
    auto child = tracer.start_span("child", root, SpanKind::Client);
    CHECK(child.trace_id() == root.trace_id());
    CHECK(child.parent_span_id() == root.span_id());
    CHECK(root.parent_span_id().empty());
}

TEST_CASE("malformed or missing traceparent starts a new trace") {
    Tracer tracer(service());
    for (const std::string& bad : {std::string(""), std::string("00-xyz"),
                                   std::string("00-00000000000000000000000000000000-00f067aa0ba902b7-01"),
                                   std::string("ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")}) {
        auto parent = tracer.extract({{"traceparent", bad}});
        CHECK_FALSE(parent.valid());
        auto span = tracer.start_span("root", parent);
        CHECK(span.trace_id().size() == 32);
        CHECK(span.trace_id() != "4bf92f3577b34da6a3ce929d0e0e4736");
        CHECK(span.parent_span_id().empty());
    }
}

TEST_CASE("an attached span is the thread's current trace context, and scopes nest") {
    Tracer tracer(service());
    CHECK_FALSE(telemetry::current_trace_context().valid());
    auto outer = tracer.start_span("outer");
    {
        auto current = outer.attach();
        CHECK(telemetry::current_trace_context().span_id == outer.span_id());
        {
            telemetry::tracing::ScopedSpan inner(tracer, "inner");
            CHECK(telemetry::current_trace_context().span_id == inner.span().span_id());
            CHECK(telemetry::current_trace_context().trace_id == outer.trace_id());
            CHECK(inner.span().parent_span_id() == outer.span_id());
        }
        CHECK(telemetry::current_trace_context().span_id == outer.span_id());
    }
    CHECK_FALSE(telemetry::current_trace_context().valid());
}

TEST_CASE("baggage set on a span reaches the spans under it and the next service") {
    Tracer tracer(service());
    auto root = tracer.start_span("checkout");
    root.set_baggage("tenant", "acme");

    auto explicit_child = tracer.start_span("charge", root);
    CHECK(explicit_child.baggage().at("tenant") == "acme");

    {
        auto scope = root.attach();
        auto implicit_child = tracer.start_span("reserve");
        CHECK(implicit_child.baggage().at("tenant") == "acme");

        std::map<std::string, std::string> headers;
        tracer.inject(implicit_child, headers);
        CHECK(headers.at("baggage") == "tenant=acme");
        CHECK(tracer.extract(headers).baggage.at("tenant") == "acme");
    }
    CHECK(tracer.start_span("after").baggage().empty());
}

TEST_CASE("a server scope continues the caller's trace and a client scope hands it on") {
    Tracer tracer(service());
    // As gRPC's client_metadata() iterates: a multimap of string views.
    const std::string name = "traceparent";
    std::multimap<std::string_view, std::string_view> metadata{{name, kTraceparent}};

    std::map<std::string, std::string> outgoing;
    std::string server_span;
    {
        telemetry::tracing::ServerScope server(tracer, "orders.Orders/Place", metadata);
        server_span = server.span().span_id();
        CHECK(server.span().trace_id() == "4bf92f3577b34da6a3ce929d0e0e4736");
        CHECK(server.span().parent_span_id() == "00f067aa0ba902b7");
        CHECK(telemetry::current_trace_context().span_id == server_span);  // logs inside join it

        telemetry::tracing::ClientScope client(
            tracer, "billing.Billing/Charge",
            [&](const std::string& k, const std::string& v) { outgoing.emplace(k, v); });
        CHECK(client.span().parent_span_id() == server_span);
        CHECK(outgoing.at("traceparent") ==
              "00-4bf92f3577b34da6a3ce929d0e0e4736-" + client.span().span_id() + "-01");
    }
    CHECK_FALSE(telemetry::current_trace_context().valid());
}

TEST_CASE("a server scope without a caller's trace starts one") {
    Tracer tracer(service());
    telemetry::tracing::ServerScope server(tracer, "GET", std::map<std::string, std::string>{});
    CHECK(server.span().trace_id().size() == 32);
    CHECK(server.span().parent_span_id().empty());
}
