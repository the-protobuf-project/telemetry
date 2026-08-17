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
    /// Creates a new MCAP log writer.
    ///
    /// # Arguments
    ///
    /// * `service_opts` - Service configuration options
    /// * `writer` - Shared MCAP writer instance
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

    /// Writes a log entry to the MCAP file.
    ///
    /// # Arguments
    ///
    /// * `level` - Log level (debug, info, warn, error, fatal)
    /// * `message` - Log message
    /// * `file` - Source file path
    /// * `line` - Line number in source file
    /// * `data` - Optional structured data
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
