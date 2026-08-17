#pragma once

#include "telemetry/config.hpp"
#include "telemetry/logging/logger.hpp"
#include "telemetry/mcap/writer.hpp"
#include "telemetry/metrics/metrics.hpp"
#include "telemetry/tracing/tracer.hpp"

#if TELEMETRY_USE_OTEL
#include "telemetry/otel/exporter.hpp"
#endif

#include <memory>
#include <string>
#include <optional>

namespace telemetry {

class TelemetryBuilder;

class Telemetry {
public:
    static TelemetryBuilder builder(const std::string& name, const std::string& version);

    Telemetry(const ServiceOptions& service_opts, const TelemetryOptions& telemetry_opts);
    ~Telemetry();

    /**
 * @brief Prevents copying a telemetry instance.
 */
Telemetry(const Telemetry&) = delete;
    Telemetry& operator=(const Telemetry&) = delete;
    Telemetry(Telemetry&&) noexcept;
    Telemetry& operator=(Telemetry&&) noexcept;

    /**
 * @brief Provides access to the telemetry logger.
 *
 * @return logging::Logger& Reference to the logger owned by this telemetry instance.
 */
logging::Logger& logger() { return *logger_; }
    /**
 * @brief Provides access to the metrics instance.
 *
 * @return metrics::Metrics& Reference to the metrics instance.
 */
metrics::Metrics& metrics() { return *metrics_; }
    /**
 * @brief Provides access to the telemetry tracer.
 *
 * @return Reference to the tracer.
 */
tracing::Tracer& tracer() { return *tracer_; }

    /**
 * @brief Provides access to the configured MCAP writer.
 *
 * @return std::shared_ptr<mcap::McapWriter> The MCAP writer, or an empty pointer when MCAP writing is unavailable.
 */
std::shared_ptr<mcap::McapWriter> mcap_writer() { return mcap_writer_; }

#if TELEMETRY_USE_OTEL
    /**
 * @brief Provides access to the OpenTelemetry exporter.
 *
 * @return otel::OtelExporter* The configured exporter, or `nullptr` when exporting is disabled.
 */
otel::OtelExporter* otel_exporter() { return otel_exporter_.get(); }
#endif

    void flush();
    void close();

private:
    std::unique_ptr<logging::Logger> logger_;
    std::unique_ptr<metrics::Metrics> metrics_;
    std::unique_ptr<tracing::Tracer> tracer_;
    std::shared_ptr<mcap::McapWriter> mcap_writer_;
#if TELEMETRY_USE_OTEL
    std::unique_ptr<otel::OtelExporter> otel_exporter_;
#endif
    bool closed_ = false;
};

class TelemetryBuilder {
public:
    TelemetryBuilder(const std::string& name, const std::string& version);

    TelemetryBuilder& description(const std::string& desc);
    TelemetryBuilder& environment(Environment env);
    TelemetryBuilder& with_otlp(const std::string& host, uint16_t port);
    TelemetryBuilder& with_mcap(const std::string& path);

    Telemetry build();

private:
    std::string name_;
    std::string version_;
    std::optional<std::string> description_;
    Environment environment_ = Environment::Development;
    std::optional<std::string> otlp_host_;
    std::optional<uint16_t> otlp_port_;
    std::optional<std::string> mcap_path_;
};

}  // namespace telemetry
