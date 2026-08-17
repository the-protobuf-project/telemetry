//! MCAP + optional OTLP (`localhost:6009`).
use telemetry::{Environment, logger};
use serde::Serialize;
use std::path::PathBuf;

#[derive(Debug, Serialize)]
struct ChatMessage {
    message_id: String,
    room_id: String,
    user_id: String,
    language: String,
    message_type: String,
    content: String,
    timestamp: i64,
}

/// Initializes local telemetry and records a sample chat message.
///
/// # Examples
///
/// ```
/// #[tokio::test]
/// async fn records_sample_message() {
///     main().await.unwrap();
/// }
/// ```
///
/// # Errors
///
/// Returns an error if telemetry configuration or initialization fails.
async fn main() -> anyhow::Result<()> {
    let mcap_path = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("target/chat-logs.mcap");
    let _telemetry = telemetry::telemetry_local_otel!()
        .with_service("chat-service-mcap", "1.0.0")
        .environment(Environment::Development)
        .with_mcap(mcap_path.to_string_lossy())
        .build()?;

    logger::info!("MCAP + OTLP example");

    let msg = ChatMessage {
        message_id: "msg-mcap-001".to_string(),
        room_id: "room-ai-chat".to_string(),
        user_id: "user-alice".to_string(),
        language: "en".to_string(),
        message_type: "text".to_string(),
        content: "Hello MCAP".to_string(),
        timestamp: chrono::Utc::now().timestamp(),
    };
    logger::info!("Message").with_data(&msg);

    tokio::time::sleep(tokio::time::Duration::from_millis(300)).await;
    Ok(())
}
