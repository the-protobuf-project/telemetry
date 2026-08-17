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
    /// Creates a new metric MCAP writer.
    ///
    /// # Arguments
    ///
    /// * `service_opts` - Service configuration
    /// * `writer` - Shared MCAP writer instance
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

    /// Writes a counter metric.
    ///
    /// # Arguments
    ///
    /// * `name` - Metric name
    /// * `value` - Metric value
    pub fn write_counter(&mut self, name: &str, value: f64) -> Result<()> {
        self.write_metric(name, value)
    }

    /// Writes a histogram metric.
    ///
    /// # Arguments
    ///
    /// * `name` - Metric name
    /// * `value` - Metric value
    pub fn write_histogram(&mut self, name: &str, value: f64) -> Result<()> {
        self.write_metric(name, value)
    }

    /// Writes a gauge metric.
    ///
    /// # Arguments
    ///
    /// * `name` - Metric name
    /// * `value` - Metric value
    pub fn write_gauge(&mut self, name: &str, value: f64) -> Result<()> {
        self.write_metric(name, value)
    }

    /// Internal method to write a metric to MCAP.
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

    /// Gets or creates a channel for the metric.
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
