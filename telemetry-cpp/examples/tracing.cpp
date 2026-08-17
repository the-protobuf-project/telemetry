#include "telemetry/telemetry.hpp"

#include <chrono>
#include <thread>
#include <cstdlib>

/**
 * @brief Resolves the OTLP exporter host and port from the environment.
 *
 * Uses `OTEL_EXPORTER_OTLP_ENDPOINT` when set and defaults the port to `4317`
 * when the endpoint does not include one. If the variable is unset, returns
 * `localhost` and port `4317`.
 *
 * @return A host and port pair for the OTLP exporter.
 */
std::pair<std::string, uint16_t> get_otel_endpoint() {
    const char* env = std::getenv("OTEL_EXPORTER_OTLP_ENDPOINT");
    if (env) {
        std::string endpoint(env);
        auto pos = endpoint.find(':');
        if (pos != std::string::npos) {
            return {endpoint.substr(0, pos),
                    static_cast<uint16_t>(std::stoi(endpoint.substr(pos + 1)))};
        }
        return {endpoint, 4317};
    }
    return {"localhost", 4317};
}

/**
 * @brief Executes a simple traced operation.
 *
 * @param tracer Tracer used to create the operation span.
 */
void simple_operation(telemetry::tracing::Tracer& tracer) {
    TELEMETRY_SPAN(tracer, "simple_operation");
    TELEMETRY_LOG_INFO("This is a simple traced operation");
    std::this_thread::sleep_for(std::chrono::milliseconds(100));
}

/**
 * @brief Runs the simple tracing example and exports a span to the configured OTLP endpoint.
 *
 * @return 0 after the tracing operation and export wait complete.
 */
int main() {
    auto [otel_host, otel_port] = get_otel_endpoint();

    auto telemetry = telemetry::Telemetry::builder("simple-trace-test", "1.0.0")
        .environment(telemetry::Environment::Development)
        .with_otlp(otel_host, otel_port)
        .build();

    TELEMETRY_LOG_INFO("=== SIMPLE TRACE TEST ===");
    TELEMETRY_LOG_INFO("Service: simple-trace-test");
    TELEMETRY_LOG_INFO("Sending ONE span to OTLP...");

    simple_operation(telemetry.tracer());

    TELEMETRY_LOG_INFO("Span created. Waiting 5 seconds for export...");
    std::this_thread::sleep_for(std::chrono::seconds(5));

    TELEMETRY_LOG_INFO("Done! Check Tempo for service: simple-trace-test");
    TELEMETRY_LOG_INFO("Look for span: simple_operation");

    return 0;
}
