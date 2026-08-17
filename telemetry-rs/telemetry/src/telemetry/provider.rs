//! OpenTelemetry telemetry provider.
//!
//! This module provides a unified telemetry provider that manages
//! OpenTelemetry logging and metrics exporters.

use crate::logging::OtelLogger;
use crate::options::{ServiceOptions, OpenTelemetryOptions};
use anyhow::{Result, anyhow};
use opentelemetry::KeyValue;
use opentelemetry::logs::LoggerProvider as _;
use opentelemetry::trace::TracerProvider as _;
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_otlp::{LogExporter, MetricExporter, WithHttpConfig, WithTonicConfig};
use opentelemetry_sdk::Resource;
use opentelemetry_sdk::logs::SdkLoggerProvider;
use opentelemetry_sdk::metrics::SdkMeterProvider;
use opentelemetry_sdk::trace::SdkTracerProvider;
use std::collections::HashMap;
use std::sync::Arc;
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
    /// # fn example(
    /// #     service_opts: &ServiceOptions,
    /// #     telemetry_opts: &OpenTelemetryOptions,
    /// # ) -> Result<(), Box<dyn std::error::Error>> {
    /// let provider = TelemetryProvider::new(service_opts, telemetry_opts)?;
    /// # let _ = provider;
    /// # Ok(())
    /// # }
    /// ```
    pub fn new(service_opts: &ServiceOptions, telemetry_opts: &OpenTelemetryOptions) -> Result<Self> {
        let telemetry_enabled = telemetry_opts.enabled && telemetry_opts.otlp.enabled;
        let logging_enabled = telemetry_enabled && telemetry_opts.logging.enabled;
        let metrics_enabled = telemetry_enabled && telemetry_opts.metrics.enabled;
        let tracing_enabled = telemetry_enabled && telemetry_opts.tracing.enabled;

        let logger_provider = if logging_enabled {
            let exporter = if telemetry_opts.otlp.use_http {
                // Use HTTP protocol
                let mut headers = HashMap::new();
                if let Some(ref token) = telemetry_opts.otlp.auth_token {
                    headers.insert("Authorization".to_string(), format!("Bearer {}", token));
                }
                for (key, value) in &telemetry_opts.otlp.headers {
                    headers.insert(key.clone(), value.clone());
                }

                LogExporter::builder()
                    .with_http()
                    .with_endpoint(format!("{}/v1/logs", telemetry_opts.otlp.endpoint_url()))
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

                LogExporter::builder()
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

            Some(
                SdkLoggerProvider::builder()
                    .with_batch_exporter(exporter)
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

            Some(Arc::new(
                SdkMeterProvider::builder()
                    .with_periodic_exporter(exporter)
                    .with_resource(resource)
                    .build(),
            ))
        } else {
            None
        };

        let tracer_provider = if tracing_enabled {
            let exporter = if telemetry_opts.otlp.use_http {
                let mut headers = HashMap::new();
                if let Some(ref token) = telemetry_opts.otlp.auth_token {
                    headers.insert("Authorization".to_string(), format!("Bearer {}", token));
                }
                for (key, value) in &telemetry_opts.otlp.headers {
                    headers.insert(key.clone(), value.clone());
                }

                opentelemetry_otlp::SpanExporter::builder()
                    .with_http()
                    .with_endpoint(format!("{}/v1/traces", telemetry_opts.otlp.endpoint_url()))
                    .with_headers(headers)
                    .build()?
            } else {
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

                opentelemetry_otlp::SpanExporter::builder()
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

            let provider = SdkTracerProvider::builder()
                .with_batch_exporter(exporter)
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
