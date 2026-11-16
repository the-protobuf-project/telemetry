#pragma once

#include "telemetry/config.hpp"
#include "telemetry/logging/logger.hpp"
#include "telemetry/mcap/writer.hpp"
#include "telemetry/metrics/metrics.hpp"
#include "telemetry/profiling/profiler.hpp"
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
 * @brief Returns the MCAP writer owned by the telemetry instance.
 *
 * @return std::shared_ptr<mcap::McapWriter> The configured MCAP writer, or an empty pointer if MCAP writing is unavailable.
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
    /// Exports what is queued and shuts down; reports on stderr any log
    /// records or spans that never reached the collector.
    void close();

    /// What happened to every log record and span sent for export.
    Stats stats() const;

    /// The CPU profiler, or nullptr when profiling is off.
    profiling::Profiler* profiler() { return profiler_.get(); }

private:
    std::unique_ptr<logging::Logger> logger_;
    std::unique_ptr<metrics::Metrics> metrics_;
    std::unique_ptr<tracing::Tracer> tracer_;
    std::shared_ptr<mcap::McapWriter> mcap_writer_;
#if TELEMETRY_USE_OTEL
    std::unique_ptr<otel::OtelExporter> otel_exporter_;
#endif
    std::unique_ptr<profiling::Profiler> profiler_;
    bool closed_ = false;
};

class TelemetryBuilder {
public:
    TelemetryBuilder(const std::string& name, const std::string& version);

    TelemetryBuilder& description(const std::string& desc);
    TelemetryBuilder& environment(Environment env);
    TelemetryBuilder& with_otlp(const std::string& host, uint16_t port);
    TelemetryBuilder& with_mcap(const std::string& path);
    /// Tunes the OTLP export queues for logs and spans.
    TelemetryBuilder& with_export(const ExportOptions& options);
    /// Prints each log line to stdout, or not; on by default. Off, records
    /// still go to OTLP and MCAP.
    TelemetryBuilder& with_console(bool console);
    /// Profiles CPU continuously and sends it to the Pyroscope server at
    /// server_address, as the other SDKs' with_profiling does.
    TelemetryBuilder& with_profiling(const std::string& server_address = "http://localhost:4040");

    Telemetry build();

private:
    std::string name_;
    std::string version_;
    std::optional<std::string> description_;
    Environment environment_ = Environment::Development;
    std::optional<std::string> otlp_host_;
    std::optional<uint16_t> otlp_port_;
    std::optional<std::string> mcap_path_;
    ExportOptions export_options_;
    bool console_ = true;
    std::optional<std::string> profiling_address_;
};

}  // namespace telemetry
