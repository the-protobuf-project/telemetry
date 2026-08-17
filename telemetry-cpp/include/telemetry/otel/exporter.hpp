#pragma once

#include "telemetry/config.hpp"

#if TELEMETRY_USE_OTEL

#include <opentelemetry/sdk/trace/tracer_provider.h>
#include <opentelemetry/sdk/logs/logger_provider.h>
#include <opentelemetry/sdk/metrics/meter_provider.h>
#include <opentelemetry/exporters/otlp/otlp_http_exporter.h>
#include <opentelemetry/exporters/otlp/otlp_http_log_record_exporter.h>
#include <opentelemetry/exporters/otlp/otlp_http_metric_exporter.h>
#include <opentelemetry/trace/provider.h>
#include <opentelemetry/logs/provider.h>
#include <opentelemetry/metrics/provider.h>
#include <opentelemetry/sdk/trace/simple_processor.h>
#include <opentelemetry/sdk/logs/simple_log_record_processor.h>
#include <opentelemetry/sdk/metrics/export/periodic_exporting_metric_reader.h>
#include <opentelemetry/sdk/resource/resource.h>

#include <memory>
#include <string>

namespace telemetry::otel {

namespace trace_api = opentelemetry::trace;
namespace trace_sdk = opentelemetry::sdk::trace;
namespace logs_api = opentelemetry::logs;
namespace logs_sdk = opentelemetry::sdk::logs;
namespace metrics_api = opentelemetry::metrics;
namespace metrics_sdk = opentelemetry::sdk::metrics;
namespace otlp = opentelemetry::exporter::otlp;
namespace resource = opentelemetry::sdk::resource;

class OtelExporter {
public:
    OtelExporter(const ServiceOptions& service_opts, const std::string& endpoint);
    ~OtelExporter();

    OtelExporter(const OtelExporter&) = delete;
    OtelExporter& operator=(const OtelExporter&) = delete;

    opentelemetry::nostd::shared_ptr<trace_api::Tracer> get_tracer();
    opentelemetry::nostd::shared_ptr<logs_api::Logger> get_logger();
    opentelemetry::nostd::shared_ptr<metrics_api::Meter> get_meter();

    void shutdown();

    bool is_enabled() const { return enabled_; }
    const std::string& endpoint() const { return endpoint_; }

private:
    void init_tracer(const resource::Resource& resource);
    void init_logger(const resource::Resource& resource);
    void init_metrics(const resource::Resource& resource);

    std::string service_name_;
    std::string service_version_;
    std::string endpoint_;
    bool enabled_ = false;

    std::shared_ptr<trace_sdk::TracerProvider> tracer_provider_;
    std::shared_ptr<logs_sdk::LoggerProvider> logger_provider_;
    std::shared_ptr<metrics_sdk::MeterProvider> meter_provider_;
};

}  // namespace telemetry::otel

#endif  // TELEMETRY_USE_OTEL
