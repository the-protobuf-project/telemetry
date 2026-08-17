//! Logs to console + OTLP logs to local collector (`localhost:6009`).
use telemetry::{Environment, logger};
use serde::Serialize;

#[derive(Debug, Serialize)]
struct ChatMessage {
    message_id: String,
    room_id: String,
    user_id: String,
    language: String,
    content: String,
    timestamp: i64,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let _telemetry = telemetry::telemetry_local_otel!()
        .with_service("chat-service", "1.0.0")
        .description("OTLP logs → localhost:6009")
        .environment(Environment::Development)
        .build()?;

    logger::info!("Chat service started (OTLP logs to localhost:6009)");

    let msg1 = ChatMessage {
        message_id: "msg-001".to_string(),
        room_id: "room-ai-chat".to_string(),
        user_id: "user-alice".to_string(),
        language: "en".to_string(),
        content: "Hello!".to_string(),
        timestamp: chrono::Utc::now().timestamp(),
    };
    logger::info!("User message").with_data(&msg1);

    tokio::time::sleep(tokio::time::Duration::from_millis(500)).await;
    Ok(())
}
