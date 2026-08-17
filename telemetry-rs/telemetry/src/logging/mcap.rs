//! MCAP writer for logging to Foxglove-compatible MCAP files.
//!
//! This module provides functionality to write log entries to MCAP files
//! using the Foxglove Log schema.

use crate::foxglove::UnifiedMcapWriter;
use crate::options::ServiceOptions;
use anyhow::Result;
use chrono::Utc;
use serde_json::{Value, json};
use std::sync::{Arc, Mutex};

/// Writer for logging to MCAP files.
///
/// Writes log entries to MCAP files using the Foxglove Log schema,
/// which can be visualized in Foxglove Studio.
pub struct LogMcapWriter {
    writer: Arc<Mutex<UnifiedMcapWriter>>,
    channel_id: u16,
    service_name: String,
    service_version: String,
    service_environment: String,
}

impl LogMcapWriter {
    /// Creates an MCAP log writer configured with service metadata and a `/logs` channel.
    ///
    /// # Examples
    ///
    /// ```no_run
    /// # use std::sync::{Arc, Mutex};
    /// # use telemetry::foxglove::UnifiedMcapWriter;
    /// # use telemetry::logging::LogMcapWriter;
    /// # use telemetry::options::ServiceOptions;
    /// # fn example(
    /// #     service_opts: &ServiceOptions,
    /// #     writer: Arc<Mutex<UnifiedMcapWriter>>,
    /// # ) -> anyhow::Result<()> {
    /// let log_writer = LogMcapWriter::new(&service_opts, writer)?;
    /// # Ok::<(), anyhow::Error>(())
    /// # }
    /// ```
    ///
    /// `service_opts` supplies the service metadata included in log records, and `writer`
    /// is the shared MCAP writer used to create the log channel and publish messages.
    pub fn new(
        service_opts: &ServiceOptions,
        writer: Arc<Mutex<UnifiedMcapWriter>>,
    ) -> Result<Self> {
        let channel_id = {
            let mut w = writer.lock().unwrap();
            w.create_channel("/logs", "foxglove.Log")?
        };

        Ok(Self {
            writer,
            channel_id,
            service_name: service_opts.name.clone(),
            service_version: service_opts.version.clone(),
            service_environment: service_opts.environment.to_string(),
        })
    }

    /// Writes a structured log entry to the MCAP file.
    ///
    /// Log levels are mapped to Foxglove severity values from 1 (`debug`) through
    /// 5 (`fatal`). Unknown levels use the `info` severity. Missing structured
    /// data is recorded as an empty object.
    ///
    /// # Examples
    ///
    /// ```no_run
    /// # use telemetry::logging::LogMcapWriter;
    /// # fn example() -> anyhow::Result<()> {
    /// # let logger: LogMcapWriter = todo!();
    /// logger.write_log("info", "Service started", file!(), line!(), None)?;
    /// # Ok::<(), anyhow::Error>(())
    /// # }
    /// ```
    ///
    /// # Errors
    ///
    /// Returns an error if the log entry cannot be serialized or written to the
    /// MCAP file.
    pub fn write_log(
        &self,
        level: &str,
        message: &str,
        file: &str,
        line: u32,
        data: Option<Value>,
    ) -> Result<()> {
        let now = Utc::now();
        let timestamp = json!({
            "sec": now.timestamp(),
            "nsec": now.timestamp_subsec_nanos()
        });

        let level_num = match level.to_lowercase().as_str() {
            "debug" => 1,
            "info" => 2,
            "warn" => 3,
            "error" => 4,
            "fatal" => 5,
            _ => 2,
        };

        let log_entry = json!({
            "timestamp": timestamp,
            "level": level_num,
            "message": message,
            "name": self.service_name,
            "file": file,
            "line": line,
            "service_version": self.service_version,
            "service_environment": self.service_environment,
            "data": data.unwrap_or(json!({}))
        });

        let data_bytes = serde_json::to_vec(&log_entry)?;
        let log_time =
            (now.timestamp() as u64) * 1_000_000_000 + (now.timestamp_subsec_nanos() as u64);

        let mut writer = self.writer.lock().unwrap();
        writer.write_message(self.channel_id, &data_bytes, log_time, log_time)?;

        Ok(())
    }

    /// Checks if the underlying MCAP writer is closed.
    pub fn is_closed(&self) -> bool {
        self.writer.lock().unwrap().is_closed()
    }
}
