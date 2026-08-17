//! Unified MCAP writer for all message types.
//!
//! This module provides a unified writer that can handle multiple message types
//! (logs, metrics, traces) in a single MCAP file.

use super::schemas::SchemaRegistry;
use crate::options::ServiceOptions;
use anyhow::{Context, Result};
use mcap::{Writer, records::MessageHeader};
use std::collections::BTreeMap;
use std::fs::File;
use std::io::BufWriter;
use std::path::Path;

/// Unified MCAP writer for multiple message types.
///
/// Manages schemas and channels for writing various message types
/// to a single MCAP file.
pub struct UnifiedMcapWriter {
    writer: Writer<BufWriter<File>>,
    file_path: String,
    registry: SchemaRegistry,
    channels: BTreeMap<String, u16>,
    schemas: BTreeMap<String, u16>,
    closed: bool,
}

impl UnifiedMcapWriter {
    /// Creates a new unified MCAP writer.
    ///
    /// # Arguments
    ///
    /// * `_service_opts` - Service configuration (currently unused)
    /// * `mcap_path` - Path where the MCAP file will be created
    ///
    /// # Examples
    ///
    /// ```no_run
    /// use telemetry::foxglove::UnifiedMcapWriter;
    /// use telemetry::options::ServiceOptions;
    ///
    /// let service_opts = ServiceOptions::new("my-service", "1.0.0");
    /// let writer = UnifiedMcapWriter::new(&service_opts, "output.mcap").unwrap();
    /// ```
    pub fn new(_service_opts: &ServiceOptions, mcap_path: impl AsRef<Path>) -> Result<Self> {
        let path = mcap_path.as_ref();

        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent).context("Failed to create directory for MCAP file")?;
        }

        let file = File::create(path).context("Failed to create MCAP file")?;

        let buf_writer = BufWriter::new(file);
        let writer = Writer::new(buf_writer).context("Failed to create MCAP writer")?;

        Ok(Self {
            writer,
            file_path: path.to_string_lossy().to_string(),
            registry: SchemaRegistry::new(),
            channels: BTreeMap::new(),
            schemas: BTreeMap::new(),
            closed: false,
        })
    }

    /// Gets or creates a schema, returning its ID.
    fn get_or_create_schema(&mut self, schema_name: &str) -> Result<u16> {
        if let Some(&schema_id) = self.schemas.get(schema_name) {
            return Ok(schema_id);
        }

        let schema_data = self
            .registry
            .get(schema_name)
            .context(format!("Schema '{}' not found in registry", schema_name))?;

        let schema_id = self
            .writer
            .add_schema(schema_name, "jsonschema", schema_data.as_bytes())
            .context("Failed to add schema")?;

        self.schemas.insert(schema_name.to_string(), schema_id);
        Ok(schema_id)
    }

    /// Creates a channel for a specific topic and schema.
    ///
    /// # Arguments
    ///
    /// * `topic` - Topic name for the channel
    /// * `schema_name` - Name of the schema to use
    ///
    /// # Returns
    ///
    /// Channel ID that can be used to write messages
    pub fn create_channel(&mut self, topic: &str, schema_name: &str) -> Result<u16> {
        if let Some(&channel_id) = self.channels.get(topic) {
            return Ok(channel_id);
        }

        let schema_id = self.get_or_create_schema(schema_name)?;

        let channel_id = self
            .writer
            .add_channel(schema_id, topic, "json", &BTreeMap::new())
            .context("Failed to create channel")?;

        self.channels.insert(topic.to_string(), channel_id);
        Ok(channel_id)
    }

    /// Writes a message to a channel.
    ///
    /// # Arguments
    ///
    /// * `channel_id` - Channel ID to write to
    /// * `data` - Message data as bytes
    /// * `log_time` - Time when the event occurred (nanoseconds)
    /// * `publish_time` - Time when the message was published (nanoseconds)
    pub fn write_message(
        &mut self,
        channel_id: u16,
        data: &[u8],
        log_time: u64,
        publish_time: u64,
    ) -> Result<()> {
        if self.closed {
            anyhow::bail!("Writer is closed");
        }

        self.writer
            .write_to_known_channel(
                &MessageHeader {
                    channel_id,
                    sequence: 0,
                    log_time,
                    publish_time,
                },
                data,
            )
            .context("Failed to write message")?;

        Ok(())
    }

    /// Closes the MCAP writer and finalizes the file.
    pub fn close(&mut self) -> Result<()> {
        if self.closed {
            return Ok(());
        }

        self.writer
            .finish()
            .context("Failed to close MCAP writer")?;
        self.closed = true;
        Ok(())
    }

    /// Checks if the writer has been closed.
    pub fn is_closed(&self) -> bool {
        self.closed
    }

    /// Returns the file path of the MCAP file.
    pub fn file_path(&self) -> &str {
        &self.file_path
    }
}
