//! MCAP writer for metrics recording.
//!
//! This module provides functionality to write metrics to MCAP files
//! using a custom metric schema.

use anyhow::{Context, Result};
use chrono::Utc;
use serde_json::json;
use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

use crate::foxglove::UnifiedMcapWriter;
use crate::options::ServiceOptions;

/// Writer for recording metrics to MCAP files.
pub struct MetricMcapWriter {
    writer: Arc<Mutex<UnifiedMcapWriter>>,
    channels: BTreeMap<String, u16>,
    service_name: String,
}

impl MetricMcapWriter {
    /// Creates a metric MCAP writer for the configured service.
    ///
    /// # Arguments
    ///
    /// * `service_opts` - Service configuration containing the service name.
    /// * `writer` - Shared MCAP writer instance.
    ///
    /// # Returns
    ///
    /// A configured metric MCAP writer.
    pub fn new(
        service_opts: &ServiceOptions,
        writer: Arc<Mutex<UnifiedMcapWriter>>,
    ) -> Result<Self> {
        Ok(Self {
            writer,
            channels: BTreeMap::new(),
            service_name: service_opts.name.clone(),
        })
    }

    /// Records a counter metric with its name and value.
    ///
    /// # Arguments
    ///
    /// * `name` - Name of the metric.
    /// * `value` - Value of the metric.
    pub fn write_counter(&mut self, name: &str, value: f64) -> Result<()> {
        self.write_metric(name, value)
    }

    /// Records a histogram metric with its name and value.
    ///
    /// # Arguments
    ///
    /// * `name` - Name of the metric.
    /// * `value` - Value of the metric.
    pub fn write_histogram(&mut self, name: &str, value: f64) -> Result<()> {
        self.write_metric(name, value)
    }

    /// Records a gauge metric.
    ///
    /// # Errors
    ///
    /// Returns an error if the metric cannot be serialized or written to the MCAP file.
    pub fn write_gauge(&mut self, name: &str, value: f64) -> Result<()> {
        self.write_metric(name, value)
    }

    /// Writes a timestamped metric value to the MCAP output.
    ///
    /// # Errors
    ///
    /// Returns an error if the metric cannot be serialized or written.
    ///
    /// # Arguments
    ///
    /// * `name` - The metric name.
    /// * `value` - The metric value.
    ///
    /// # Returns
    ///
    /// `Ok(())` when the metric is written successfully.
    fn write_metric(&mut self, name: &str, value: f64) -> Result<()> {
        let channel_id = self.get_or_create_channel(name)?;

        let now = Utc::now();
        let metric = json!({
            "timestamp": {
                "sec": now.timestamp(),
                "nsec": now.timestamp_subsec_nanos()
            },
            "name": name,
            "value": value
        });

        let data = serde_json::to_vec(&metric)?;
        let log_time =
            (now.timestamp() as u64) * 1_000_000_000 + (now.timestamp_subsec_nanos() as u64);

        let mut writer = self.writer.lock().unwrap();
        writer.write_message(channel_id, &data, log_time, log_time)?;

        Ok(())
    }

    /// Retrieves the cached channel for a metric or creates one using the service-qualified metric topic.
    ///
    /// Dots in the metric name are converted to path separators when constructing the topic.
    ///
    /// # Errors
    ///
    /// Returns an error if the channel cannot be created.
    fn get_or_create_channel(&mut self, metric_name: &str) -> Result<u16> {
        if let Some(&channel_id) = self.channels.get(metric_name) {
            return Ok(channel_id);
        }

        let topic = format!(
            "/metrics/{}/{}",
            self.service_name,
            metric_name.replace('.', "/")
        );

        let channel_id = {
            let mut writer = self.writer.lock().unwrap();
            writer
                .create_channel(&topic, "the-protobuf-project.metric")
                .context(format!(
                    "Failed to create channel for metric {}",
                    metric_name
                ))?
        };

        self.channels.insert(metric_name.to_string(), channel_id);
        Ok(channel_id)
    }
}
