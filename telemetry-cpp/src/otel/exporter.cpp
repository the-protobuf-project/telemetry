#include "telemetry/otel/exporter.hpp"

#if TELEMETRY_USE_OTEL

#include <opentelemetry/sdk/trace/batch_span_processor.h>
#include <opentelemetry/sdk/logs/batch_log_record_processor.h>

namespace telemetry::otel {

/**
 * @brief Initializes the OpenTelemetry exporter for a service.
 *
 * @param service_opts Service name and version used for telemetry resource attributes.
 * @param endpoint Base OTLP endpoint used to configure telemetry exporters.
 */
OtelExporter::OtelExporter(const ServiceOptions& service_opts, const std::string& endpoint)
    : service_name_(service_opts.name)
    , service_version_(service_opts.version)
    , endpoint_(endpoint) {

    auto resource_attributes = resource::ResourceAttributes{
        {"service.name", service_name_},
        {"service.version", service_version_},
    };
    auto resource = resource::Resource::Create(resource_attributes);

    init_tracer(resource);
    init_logger(resource);
    init_metrics(resource);

    enabled_ = true;
}

OtelExporter::~OtelExporter() {
    shutdown();
}

void OtelExporter::init_tracer(const resource::Resource& resource) {
    otlp::OtlpHttpExporterOptions opts;
    opts.url = endpoint_ + "/v1/traces";

    auto exporter = std::make_unique<otlp::OtlpHttpExporter>(opts);

    trace_sdk::BatchSpanProcessorOptions processor_opts;
    processor_opts.max_queue_size = 2048;
    processor_opts.max_export_batch_size = 512;

    auto processor = std::make_unique<trace_sdk::BatchSpanProcessor>(
        std::move(exporter), processor_opts);

    tracer_provider_ = std::make_shared<trace_sdk::TracerProvider>(
        std::move(processor), resource);

    trace_api::Provider::SetTracerProvider(
        opentelemetry::nostd::shared_ptr<trace_api::TracerProvider>(tracer_provider_));
}

void OtelExporter::init_logger(const resource::Resource& resource) {
    otlp::OtlpHttpLogRecordExporterOptions opts;
    opts.url = endpoint_ + "/v1/logs";

    auto exporter = std::make_unique<otlp::OtlpHttpLogRecordExporter>(opts);

    logs_sdk::BatchLogRecordProcessorOptions processor_opts;
    processor_opts.max_queue_size = 2048;
    processor_opts.max_export_batch_size = 512;

    auto processor = std::make_unique<logs_sdk::BatchLogRecordProcessor>(
        std::move(exporter), processor_opts);

    logger_provider_ = std::make_shared<logs_sdk::LoggerProvider>(
        std::move(processor), resource);

    logs_api::Provider::SetLoggerProvider(
        opentelemetry::nostd::shared_ptr<logs_api::LoggerProvider>(logger_provider_));
}

void OtelExporter::init_metrics(const resource::Resource& resource) {
    otlp::OtlpHttpMetricExporterOptions opts;
    opts.url = endpoint_ + "/v1/metrics";

    auto exporter = std::make_unique<otlp::OtlpHttpMetricExporter>(opts);

    metrics_sdk::PeriodicExportingMetricReaderOptions reader_opts;
    reader_opts.export_interval_millis = std::chrono::milliseconds(1000);
    reader_opts.export_timeout_millis = std::chrono::milliseconds(500);

    auto reader = std::make_unique<metrics_sdk::PeriodicExportingMetricReader>(
        std::move(exporter), reader_opts);

    meter_provider_ = std::make_shared<metrics_sdk::MeterProvider>();
    meter_provider_->AddMetricReader(std::move(reader));

    metrics_api::Provider::SetMeterProvider(
        opentelemetry::nostd::shared_ptr<metrics_api::MeterProvider>(meter_provider_));
}

opentelemetry::nostd::shared_ptr<trace_api::Tracer> OtelExporter::get_tracer() {
    if (!tracer_provider_) return opentelemetry::nostd::shared_ptr<trace_api::Tracer>();
    return tracer_provider_->GetTracer(service_name_, service_version_);
}

opentelemetry::nostd::shared_ptr<logs_api::Logger> OtelExporter::get_logger() {
    if (!logger_provider_) return opentelemetry::nostd::shared_ptr<logs_api::Logger>();
    return logger_provider_->GetLogger(service_name_, service_version_);
}

/**
 * @brief Retrieves the metrics meter for this service.
 *
 * @return A shared pointer to the service meter, or an empty pointer if the
 *         meter provider is unavailable.
 */
opentelemetry::nostd::shared_ptr<metrics_api::Meter> OtelExporter::get_meter() {
    if (!meter_provider_) return opentelemetry::nostd::shared_ptr<metrics_api::Meter>();
    return meter_provider_->GetMeter(service_name_, service_version_);
}

/**
 * @brief Shuts down all initialized OpenTelemetry providers and disables the exporter.
 */
void OtelExporter::shutdown() {
    if (!enabled_) return;

    if (tracer_provider_) {
        tracer_provider_->Shutdown();
    }
    if (logger_provider_) {
        logger_provider_->Shutdown();
    }
    if (meter_provider_) {
        meter_provider_->Shutdown();
    }

    enabled_ = false;
}

}  // namespace telemetry::otel

#endif  // TELEMETRY_USE_OTEL
