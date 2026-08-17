//! Foxglove integration for MCAP file recording.
//!
//! This module provides functionality for writing logs, metrics, and traces
//! to MCAP files that can be visualized in Foxglove Studio.

pub mod schemas;
pub mod writer;

pub use schemas::SchemaRegistry;
pub use writer::UnifiedMcapWriter;
