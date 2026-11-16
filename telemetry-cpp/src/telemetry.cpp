#include "telemetry/telemetry.hpp"

#include <cstdio>
#include <utility>

namespace telemetry {

/**
 * @brief Initializes telemetry components for a service.
 *
 * Configures logging, metrics, tracing, and optional MCAP and OTLP output, then
 * installs the resulting logger as the global logger.
 *
 * @param service_opts Service identity and environment configuration.
 * @param telemetry_opts Telemetry output configuration.
 */
Telemetry::Telemetry(const ServiceOptions& service_opts, const TelemetryOptions& telemetry_opts) {
    if (telemetry_opts.foxglove.enabled && !telemetry_opts.foxglove.mcap_path.empty()) {
        mcap_writer_ = std::make_shared<mcap::McapWriter>(service_opts, telemetry_opts.foxglove.mcap_path);
    }

#if TELEMETRY_USE_OTEL
    if (telemetry_opts.telemetry.otlp.enabled) {
        std::string endpoint = "http://" + telemetry_opts.telemetry.otlp.host + ":" +
                               std::to_string(telemetry_opts.telemetry.otlp.port);
        otel_exporter_ = std::make_unique<otel::OtelExporter>(service_opts, endpoint,
                                                              telemetry_opts.telemetry.otlp.export_options);
    }
#endif

    logger_ = std::make_unique<logging::Logger>(
        service_opts.name,
        service_opts.version,
        environment_to_string(service_opts.environment),
        mcap_writer_
#if TELEMETRY_USE_OTEL
        , otel_exporter_.get()
#endif
    );

    logger_->set_console(telemetry_opts.console);

    metrics_ = std::make_unique<metrics::Metrics>(service_opts, mcap_writer_
#if TELEMETRY_USE_OTEL
        , otel_exporter_.get()
#endif
    );

    if (telemetry_opts.telemetry.otlp.enabled) {
        std::string endpoint = "http://" + telemetry_opts.telemetry.otlp.host + ":" +
                               std::to_string(telemetry_opts.telemetry.otlp.port);
        tracer_ = std::make_unique<tracing::Tracer>(service_opts, mcap_writer_, endpoint
#if TELEMETRY_USE_OTEL
            , otel_exporter_.get()
#endif
        );
    } else {
        tracer_ = std::make_unique<tracing::Tracer>(service_opts, mcap_writer_
#if TELEMETRY_USE_OTEL
            , otel_exporter_.get()
#endif
        );
    }

    auto global_logger = std::make_unique<logging::Logger>(
        service_opts.name,
        service_opts.version,
        environment_to_string(service_opts.environment),
        mcap_writer_
#if TELEMETRY_USE_OTEL
        , otel_exporter_.get()
#endif
    );
    global_logger->set_console(telemetry_opts.console);
    logging::GlobalLogger::init(std::move(global_logger));
}

/**
 * @brief Closes the telemetry instance and releases its resources.
 */
Telemetry::~Telemetry() {
    close();
}

/**
 * @brief Transfers telemetry ownership from another instance.
 *
 * @param other Instance whose telemetry components are transferred. It is marked closed after the transfer.
 */
Telemetry::Telemetry(Telemetry&& other) noexcept
    : logger_(std::move(other.logger_))
    , metrics_(std::move(other.metrics_))
    , tracer_(std::move(other.tracer_))
    , mcap_writer_(std::move(other.mcap_writer_))
#if TELEMETRY_USE_OTEL
    , otel_exporter_(std::move(other.otel_exporter_))
#endif
    , closed_(other.closed_) {
    other.closed_ = true;
}

/**
 * @brief Replaces this telemetry instance with the contents of another instance.
 *
 * @param other Telemetry instance whose resources and state are transferred.
 * @return Telemetry& Reference to this instance.
 */
Telemetry& Telemetry::operator=(Telemetry&& other) noexcept {
    if (this != &other) {
        close();
        logger_ = std::move(other.logger_);
        metrics_ = std::move(other.metrics_);
        tracer_ = std::move(other.tracer_);
        mcap_writer_ = std::move(other.mcap_writer_);
#if TELEMETRY_USE_OTEL
        otel_exporter_ = std::move(other.otel_exporter_);
#endif
        closed_ = other.closed_;
        other.closed_ = true;
    }
    return *this;
}

/**
 * @brief Creates a telemetry builder for a service.
 *
 * @param name Service name.
 * @param version Service version.
 * @return TelemetryBuilder Configured with the specified service name and version.
 */
TelemetryBuilder Telemetry::builder(const std::string& name, const std::string& version) {
    return TelemetryBuilder(name, version);
}

/**
 * @brief Flushes buffered telemetry data to the MCAP output.
 */
void Telemetry::flush() {
    if (mcap_writer_) {
        mcap_writer_->flush();
    }
}

/**
 * @brief Closes telemetry resources and shuts down configured exporters.
 *
 * Calling this function multiple times has no additional effect.
 */
void Telemetry::close() {
    if (closed_) return;

    logging::GlobalLogger::shutdown();

#if TELEMETRY_USE_OTEL
    if (otel_exporter_) {
        otel_exporter_->shutdown();
        const auto s = otel_exporter_->stats();
        for (const auto& [name, sig] : {std::pair{"log records", s.logs}, std::pair{"spans", s.spans}}) {
            if (sig.pending() > 0) {
                std::fprintf(stderr,
                             "telemetry: %llu of %llu %s dropped (export queue full); use "
                             "with_export(ExportOptions{.lossless = true}) or a larger queue_size to keep them\n",
                             static_cast<unsigned long long>(sig.pending()),
                             static_cast<unsigned long long>(sig.emitted), name);
            }
            if (sig.failed > 0) {
                std::fprintf(stderr, "telemetry: %llu of %llu %s failed to export\n",
                             static_cast<unsigned long long>(sig.failed),
                             static_cast<unsigned long long>(sig.emitted), name);
            }
        }
    }
#endif

    if (mcap_writer_) {
        mcap_writer_->close();
    }

    closed_ = true;
}

Stats Telemetry::stats() const {
#if TELEMETRY_USE_OTEL
    if (otel_exporter_) return otel_exporter_->stats();
#endif
    return {};
}

/**
 * @brief Creates a telemetry builder for a service.
 *
 * @param name Service name.
 * @param version Service version.
 */
TelemetryBuilder::TelemetryBuilder(const std::string& name, const std::string& version)
    : name_(name)
    , version_(version) {
}

/**
 * @brief Sets the service description.
 *
 * @param desc Description associated with the service.
 * @return This builder.
 */
TelemetryBuilder& TelemetryBuilder::description(const std::string& desc) {
    description_ = desc;
    return *this;
}

/**
 * @brief Sets the deployment environment for the telemetry configuration.
 *
 * @param env Environment associated with the service.
 * @return TelemetryBuilder& This builder.
 */
TelemetryBuilder& TelemetryBuilder::environment(Environment env) {
    environment_ = env;
    return *this;
}

/**
 * @brief Configures the OTLP exporter endpoint.
 *
 * @param host OTLP exporter host.
 * @param port OTLP exporter port.
 * @return This builder for method chaining.
 */
TelemetryBuilder& TelemetryBuilder::with_otlp(const std::string& host, uint16_t port) {
    otlp_host_ = host;
    otlp_port_ = port;
    return *this;
}

/**
 * @brief Configures the MCAP output path.
 *
 * @param path Path where MCAP telemetry data will be written.
 * @return This builder.
 */
TelemetryBuilder& TelemetryBuilder::with_mcap(const std::string& path) {
    mcap_path_ = path;
    return *this;
}

TelemetryBuilder& TelemetryBuilder::with_export(const ExportOptions& options) {
    export_options_ = options;
    return *this;
}

TelemetryBuilder& TelemetryBuilder::with_console(bool console) {
    console_ = console;
    return *this;
}

/**
 * @brief Builds a telemetry instance from the configured service and exporter settings.
 *
 * @return Telemetry Configured telemetry instance.
 */
Telemetry TelemetryBuilder::build() {
    ServiceOptions service_opts(name_, version_);
    service_opts.with_environment(environment_);

    if (description_) {
        service_opts.with_description(*description_);
    }

    TelemetryOptions telemetry_opts;
    telemetry_opts.console = console_;
    telemetry_opts.telemetry.otlp.export_options = export_options_;

    if (otlp_host_ && otlp_port_) {
        telemetry_opts.telemetry.otlp.enabled = true;
        telemetry_opts.telemetry.otlp.host = *otlp_host_;
        telemetry_opts.telemetry.otlp.port = *otlp_port_;
    }

    if (mcap_path_) {
        telemetry_opts.foxglove.enabled = true;
        telemetry_opts.foxglove.mcap_path = *mcap_path_;
    }

    return Telemetry(service_opts, telemetry_opts);
}

}  // namespace telemetry
