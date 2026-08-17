#include <telemetry/telemetry.hpp>
#include <iostream>
#include <thread>
#include <chrono>

/**
 * @brief Demonstrates telemetry collection with logs, metrics, tracing, and MCAP output.
 *
 * @return int Zero on successful completion.
 */
int main() {
    auto telemetry = telemetry::Telemetry::builder("example-service", "1.0.0")
        .description("Example service demonstrating telemetry-cpp")
        .environment(telemetry::Environment::Development)
        .with_mcap("output.mcap")
        .build();

    TELEMETRY_LOG_INFO("Application started");
    TELEMETRY_LOG_DEBUG("Debug message with context");

    telemetry.metrics().counter("requests_total", 1.0);
    telemetry.metrics().histogram("request_duration_ms", 42.5);
    telemetry.metrics().gauge("active_connections", 10.0);

    {
        auto span = telemetry.tracer().start_span("process_request");
        span.set_attribute("user_id", "12345");
        span.set_attribute("method", "GET");
        span.add_event("started_processing");

        std::this_thread::sleep_for(std::chrono::milliseconds(100));

        span.add_event("finished_processing");
        span.set_status(telemetry::tracing::SpanStatus::Ok);
        span.end();
    }

    {
        TELEMETRY_SPAN(telemetry.tracer(), "nested_operation");
        telemetry.metrics().counter("operations_total", 1.0);
        TELEMETRY_LOG_INFO("Performing nested operation");
    }

    telemetry.logger().info("Custom logger message", __FILE__, __LINE__);

    for (int i = 0; i < 5; ++i) {
        telemetry.metrics().counter("loop_iterations", 1.0);
        telemetry.metrics().histogram("iteration_value", static_cast<double>(i));
    }

    TELEMETRY_LOG_INFO("Application shutting down");

    telemetry.flush();
    telemetry.close();

    std::cout << "Example completed. Check output.mcap in Foxglove Studio." << std::endl;
    return 0;
}
