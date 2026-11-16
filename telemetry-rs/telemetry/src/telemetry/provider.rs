//! OpenTelemetry telemetry provider.
//!
//! This module provides a unified telemetry provider that manages
//! OpenTelemetry logging and metrics exporters.

use crate::logging::OtelLogger;
use crate::options::{OpenTelemetryOptions, ServiceOptions};
use anyhow::{Result, anyhow};
use opentelemetry::KeyValue;
use opentelemetry::logs::LoggerProvider as _;
use opentelemetry::trace::TracerProvider as _;
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_otlp::{LogExporter, MetricExporter, WithHttpConfig, WithTonicConfig};
use opentelemetry_sdk::Resource;
use opentelemetry_sdk::logs::SdkLoggerProvider;
use opentelemetry_sdk::metrics::{Instrument, SdkMeterProvider, Stream};
use opentelemetry_sdk::trace::SdkTracerProvider;
use std::collections::HashMap;
use std::sync::Arc;

use super::accounting::{
    AccountedLogProcessor, AccountedSpanProcessor, Counters, CountingLogExporter,
    CountingSpanExporter, Shards,
};
use tonic::metadata::{MetadataKey, MetadataMap, MetadataValue};

/// Telemetry provider for OpenTelemetry integration.
///
/// Manages logger and meter providers for sending telemetry data
/// to OpenTelemetry collectors via OTLP.
pub struct TelemetryProvider {
    logger_provider: Option<SdkLoggerProvider>,
    meter_provider: Option<Arc<SdkMeterProvider>>,
    /// Clone shares the same processor pipeline as the global provider (OTEL 0.31+).
    tracer_provider: Option<SdkTracerProvider>,
    /// What happened to every log record and span sent for export.
    logs: Arc<Counters>,
    spans: Arc<Counters>,
}

impl TelemetryProvider {
    /// Creates a telemetry provider configured for the enabled OpenTelemetry signals.
    ///
    /// Disabled signals do not create providers. Enabled signals are configured for
    /// OTLP export using the service and telemetry settings.
    ///
    /// # Arguments
    ///
    /// * `service_opts` - Service identity, environment, version, and custom labels.
    /// * `telemetry_opts` - OpenTelemetry enablement, exporter, endpoint, and
    ///   authentication settings.
    ///
    /// # Errors
    ///
    /// Returns an error if an enabled OTLP exporter cannot be built.
    ///
    /// # Examples
    ///
    /// ```
    /// # use telemetry::options::{OpenTelemetryOptions, ServiceOptions};
    /// # use telemetry::telemetry::TelemetryProvider;
    /// # fn example(
    /// #     service_opts: &ServiceOptions,
    /// #     telemetry_opts: &OpenTelemetryOptions,
    /// # ) -> Result<(), Box<dyn std::error::Error>> {
    /// let provider = TelemetryProvider::new(service_opts, telemetry_opts)?;
    /// # let _ = provider;
    /// # Ok(())
    /// # }
    /// ```
    pub fn new(
        service_opts: &ServiceOptions,
        telemetry_opts: &OpenTelemetryOptions,
    ) -> Result<Self> {
        let telemetry_enabled = telemetry_opts.enabled && telemetry_opts.otlp.enabled;
        let logging_enabled = telemetry_enabled && telemetry_opts.logging.enabled;
        let metrics_enabled = telemetry_enabled && telemetry_opts.metrics.enabled;
        let tracing_enabled = telemetry_enabled && telemetry_opts.tracing.enabled;
        let logs = Arc::new(Counters::default());
        let spans = Arc::new(Counters::default());

        let logger_provider = if logging_enabled {
            let exporters = (0..telemetry_opts.export.exporters.max(1))
                .map(|_| log_exporter(telemetry_opts))
                .collect::<Result<Vec<_>>>()?;

            // Build resource attributes including custom service labels
            let mut attrs = vec![
                KeyValue::new("service.version", service_opts.version.clone()),
                KeyValue::new(
                    "deployment.environment",
                    service_opts.environment.to_string(),
                ),
            ];
            for (key, value) in &service_opts.labels {
                attrs.push(KeyValue::new(key.clone(), value.clone()));
            }

            let resource = Resource::builder()
                .with_service_name(service_opts.name.clone())
                .with_attributes(attrs)
                .build();

            let config = || {
                opentelemetry_sdk::logs::BatchConfigBuilder::default()
                    .with_max_queue_size(telemetry_opts.export.queue_size)
                    .with_max_export_batch_size(
                        telemetry_opts
                            .export
                            .batch_size
                            .min(telemetry_opts.export.queue_size),
                    )
                    .build()
            };
            let shards = exporters
                .into_iter()
                .map(|exporter| {
                    let counters = Arc::new(Counters::part_of(&logs));
                    let batch = opentelemetry_sdk::logs::BatchLogProcessor::builder(
                        CountingLogExporter::new(exporter, Arc::clone(&counters)),
                    )
                    .with_batch_config(config())
                    .build();
                    AccountedLogProcessor::new(batch, counters, &telemetry_opts.export)
                })
                .collect();
            Some(
                SdkLoggerProvider::builder()
                    .with_log_processor(Shards::new(shards))
                    .with_resource(resource)
                    .build(),
            )
        } else {
            None
        };

        let meter_provider = if metrics_enabled {
            let exporter = if telemetry_opts.otlp.use_http {
                // Use HTTP protocol
                let mut headers = HashMap::new();
                if let Some(ref token) = telemetry_opts.otlp.auth_token {
                    headers.insert("Authorization".to_string(), format!("Bearer {}", token));
                }
                for (key, value) in &telemetry_opts.otlp.headers {
                    headers.insert(key.clone(), value.clone());
                }

                MetricExporter::builder()
                    .with_http()
                    .with_endpoint(format!("{}/v1/metrics", telemetry_opts.otlp.endpoint_url()))
                    .with_headers(headers)
                    .build()?
            } else {
                // Use gRPC protocol
                let mut metadata = MetadataMap::new();
                if let Some(ref token) = telemetry_opts.otlp.auth_token {
                    metadata.insert(
                        "authorization",
                        format!("Bearer {}", token).parse().unwrap(),
                    );
                }
                for (key, value) in &telemetry_opts.otlp.headers {
                    if let (Ok(k), Ok(v)) = (
                        key.parse::<MetadataKey<_>>(),
                        value.parse::<MetadataValue<_>>(),
                    ) {
                        metadata.insert(k, v);
                    }
                }

                MetricExporter::builder()
                    .with_tonic()
                    .with_endpoint(telemetry_opts.otlp.endpoint_url())
                    .with_metadata(metadata)
                    .build()?
            };

            // Build resource attributes including custom service labels
            let mut attrs = vec![
                KeyValue::new("service.version", service_opts.version.clone()),
                KeyValue::new(
                    "deployment.environment",
                    service_opts.environment.to_string(),
                ),
            ];
            for (key, value) in &service_opts.labels {
                attrs.push(KeyValue::new(key.clone(), value.clone()));
            }

            let resource = Resource::builder()
                .with_service_name(service_opts.name.clone())
                .with_attributes(attrs)
                .build();

            // One stream per attribute set, up to the configured limit: the
            // SDK's own default folds everything past 2000 into one series.
            let limit = telemetry_opts.metrics.cardinality_limit;
            Some(Arc::new(
                SdkMeterProvider::builder()
                    .with_periodic_exporter(exporter)
                    .with_resource(resource)
                    .with_view(move |_: &Instrument| {
                        Stream::builder().with_cardinality_limit(limit).build().ok()
                    })
                    .build(),
            ))
        } else {
            None
        };

        let tracer_provider = if tracing_enabled {
            let exporters = (0..telemetry_opts.export.exporters.max(1))
                .map(|_| span_exporter(telemetry_opts))
                .collect::<Result<Vec<_>>>()?;

            // Build resource attributes including custom service labels
            let mut attrs = vec![
                KeyValue::new("service.version", service_opts.version.clone()),
                KeyValue::new(
                    "deployment.environment",
                    service_opts.environment.to_string(),
                ),
            ];
            for (key, value) in &service_opts.labels {
                attrs.push(KeyValue::new(key.clone(), value.clone()));
            }

            let resource = Resource::builder()
                .with_service_name(service_opts.name.clone())
                .with_attributes(attrs)
                .build();

            let config = || {
                opentelemetry_sdk::trace::BatchConfigBuilder::default()
                    .with_max_queue_size(telemetry_opts.export.queue_size)
                    .with_max_export_batch_size(
                        telemetry_opts
                            .export
                            .batch_size
                            .min(telemetry_opts.export.queue_size),
                    )
                    .build()
            };
            let shards = exporters
                .into_iter()
                .map(|exporter| {
                    let counters = Arc::new(Counters::part_of(&spans));
                    let batch = opentelemetry_sdk::trace::BatchSpanProcessor::builder(
                        CountingSpanExporter::new(exporter, Arc::clone(&counters)),
                    )
                    .with_batch_config(config())
                    .build();
                    AccountedSpanProcessor::new(batch, counters, &telemetry_opts.export)
                })
                .collect();
            let provider = SdkTracerProvider::builder()
                .with_span_processor(Shards::new(shards))
                .with_resource(resource)
                .build();
            opentelemetry::global::set_tracer_provider(provider.clone());
            Some(provider)
        } else {
            None
        };

        Ok(Self {
            logger_provider,
            meter_provider,
            tracer_provider,
            logs,
            spans,
        })
    }

    /// Gets a logger instance with the specified name.
    ///
    /// # Arguments
    ///
    /// * `name` - Logger name
    pub fn get_logger(&self, name: &str) -> Option<OtelLogger> {
        self.logger_provider
            .as_ref()
            .map(|provider| OtelLogger::new(provider.logger(name.to_string())))
    }

    /// Returns the meter provider for creating metrics.
    pub fn meter_provider(&self) -> Option<Arc<SdkMeterProvider>> {
        self.meter_provider.clone()
    }

    /// Counters for log records and spans, readable after shutdown.
    pub fn counters(&self) -> (Arc<Counters>, Arc<Counters>) {
        (Arc::clone(&self.logs), Arc::clone(&self.spans))
    }

    /// Returns a tracer for manual span creation.
    pub fn get_tracer(&self, name: &str) -> Option<opentelemetry_sdk::trace::Tracer> {
        self.tracer_provider
            .as_ref()
            .map(|provider| provider.tracer(name.to_string()))
    }

    /// Flushes all pending telemetry data.
    pub fn flush(&self) -> Result<()> {
        let mut errors = Vec::new();

        if let Some(ref provider) = self.logger_provider
            && let Err(err) = provider.force_flush()
        {
            errors.push(format!("logger force flush: {err}"));
        }
        if let Some(ref provider) = self.meter_provider
            && let Err(err) = provider.force_flush()
        {
            errors.push(format!("meter force flush: {err}"));
        }
        if let Some(ref provider) = self.tracer_provider
            && let Err(err) = provider.force_flush()
        {
            errors.push(format!("tracer force flush: {err}"));
        }

        if errors.is_empty() {
            Ok(())
        } else {
            Err(anyhow!(errors.join("; ")))
        }
    }

    /// Shuts down the telemetry provider and flushes remaining data.
    pub fn shutdown(self) -> Result<()> {
        let mut errors = Vec::new();

        if let Err(err) = self.flush() {
            errors.push(err.to_string());
        }

        if let Some(provider) = self.logger_provider
            && let Err(err) = provider.shutdown()
        {
            errors.push(format!("logger shutdown: {err}"));
        }
        if let Some(provider) = self.meter_provider
            && let Err(err) = provider.shutdown()
        {
            errors.push(format!("meter shutdown: {err}"));
        }
        if let Some(provider) = self.tracer_provider
            && let Err(err) = provider.shutdown()
        {
            errors.push(format!("tracer shutdown: {err}"));
        }

        if errors.is_empty() {
            Ok(())
        } else {
            Err(anyhow!(errors.join("; ")))
        }
    }
}

/// The OTLP headers for an HTTP exporter: the bearer token and any extras.
fn http_headers(opts: &OpenTelemetryOptions) -> HashMap<String, String> {
    let mut headers = HashMap::new();
    if let Some(ref token) = opts.otlp.auth_token {
        headers.insert("Authorization".to_string(), format!("Bearer {}", token));
    }
    for (key, value) in &opts.otlp.headers {
        headers.insert(key.clone(), value.clone());
    }
    headers
}

/// The gRPC metadata for a tonic exporter: the bearer token and any extras.
fn grpc_metadata(opts: &OpenTelemetryOptions) -> MetadataMap {
    let mut metadata = MetadataMap::new();
    if let Some(ref token) = opts.otlp.auth_token {
        metadata.insert(
            "authorization",
            format!("Bearer {}", token).parse().unwrap(),
        );
    }
    for (key, value) in &opts.otlp.headers {
        if let (Ok(k), Ok(v)) = (
            key.parse::<MetadataKey<_>>(),
            value.parse::<MetadataValue<_>>(),
        ) {
            metadata.insert(k, v);
        }
    }
    metadata
}

/// One OTLP log exporter, over HTTP or gRPC as configured.
fn log_exporter(opts: &OpenTelemetryOptions) -> Result<LogExporter> {
    Ok(if opts.otlp.use_http {
        LogExporter::builder()
            .with_http()
            .with_endpoint(format!("{}/v1/logs", opts.otlp.endpoint_url()))
            .with_headers(http_headers(opts))
            .build()?
    } else {
        LogExporter::builder()
            .with_tonic()
            .with_endpoint(opts.otlp.endpoint_url())
            .with_metadata(grpc_metadata(opts))
            .build()?
    })
}

/// One OTLP span exporter, over HTTP or gRPC as configured.
fn span_exporter(opts: &OpenTelemetryOptions) -> Result<opentelemetry_otlp::SpanExporter> {
    Ok(if opts.otlp.use_http {
        opentelemetry_otlp::SpanExporter::builder()
            .with_http()
            .with_endpoint(format!("{}/v1/traces", opts.otlp.endpoint_url()))
            .with_headers(http_headers(opts))
            .build()?
    } else {
        opentelemetry_otlp::SpanExporter::builder()
            .with_tonic()
            .with_endpoint(opts.otlp.endpoint_url())
            .with_metadata(grpc_metadata(opts))
            .build()?
    })
}
