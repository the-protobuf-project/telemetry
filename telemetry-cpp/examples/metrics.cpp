#include "telemetry/telemetry.hpp"

#include <chrono>
#include <thread>
#include <vector>
#include <cstdlib>

/**
 * @brief Resolves the OTLP exporter endpoint from the environment.
 *
 * Uses the `OTEL_EXPORTER_OTLP_ENDPOINT` value when set, applying port
 * `4317` when the value contains only a host. Defaults to `localhost:4317`
 * when the environment variable is unset.
 *
 * @return Pair containing the endpoint host and port.
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

struct LlmMetrics : public telemetry::metrics::RecordMetrics {
    uint64_t request_count = 0;
    double latency_ms = 0.0;
    double cache_hit_rate = 0.0;

    LlmMetrics() = default;
    /**
         * @brief Creates LLM metrics with request, latency, and cache hit-rate values.
         *
         * @param count Number of requests.
         * @param latency Response latency in milliseconds.
         * @param hit_rate Cache hit rate.
         */
        LlmMetrics(uint64_t count, double latency, double hit_rate)
        : request_count(count), latency_ms(latency), cache_hit_rate(hit_rate) {}

    /**
     * @brief Defines the metrics reported for LLM activity.
     *
     * @return Vector containing request count, response latency, and cache hit rate metrics.
     */
    std::vector<telemetry::metrics::MetricField> metric_fields() const override {
        return {
            {"llm.requests.total", telemetry::metrics::MetricType::Counter,
             "Total number of LLM requests", static_cast<double>(request_count)},
            {"llm.response.latency_ms", telemetry::metrics::MetricType::Histogram,
             "LLM response latency in milliseconds", latency_ms},
            {"llm.cache.hit_rate", telemetry::metrics::MetricType::Gauge,
             "LLM cache hit rate percentage", cache_hit_rate}
        };
    }
};

/**
 * @brief Records example API and LLM metrics for 30 seconds.
 *
 * @return 0 on successful completion.
 */
int main() {
    auto [otel_host, otel_port] = get_otel_endpoint();

    auto telemetry = telemetry::Telemetry::builder("metrics-example", "1.0.0")
        .description("Metrics example service")
        .environment(telemetry::Environment::Development)
        .with_mcap("examples/metrics.mcap")
        .with_otlp(otel_host, otel_port)
        .build();

    TELEMETRY_LOG_INFO("Metrics Example Started");
    TELEMETRY_LOG_INFO("Sending metrics to OTEL collector at localhost:4317");

    LlmMetrics llm_metrics{42, 123.5, 0.85};
    telemetry.metrics().record(llm_metrics);
    TELEMETRY_LOG_INFO("Recorded LLM metrics from struct");

    TELEMETRY_LOG_INFO("Recording metrics for 30 seconds...");

    for (int iteration = 0; iteration < 30; ++iteration) {
        for (int i = 0; i < 10; ++i) {
            telemetry.metrics().counter("api.requests", 1.0);
            telemetry.metrics().histogram("api.latency_ms", static_cast<double>(i) * 10.0 + 50.0);
            telemetry.metrics().gauge("api.active_connections", static_cast<double>(10 - i));

            telemetry.metrics().record(llm_metrics);

            TELEMETRY_LOG_DEBUG("Recorded API metrics");
            std::this_thread::sleep_for(std::chrono::milliseconds(100));
        }
    }

    TELEMETRY_LOG_INFO("Metrics recording completed");
    TELEMETRY_LOG_INFO("MCAP file will be finalized automatically");

    return 0;
}
