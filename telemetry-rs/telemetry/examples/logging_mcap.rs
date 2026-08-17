use telemetry::{Environment, Telemetry, logger};
use serde::Serialize;

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

/// Runs the chat-service MCAP logging example and writes generated chat messages to the configured MCAP file.
///
/// # Examples
///
/// ```no_run
/// // Run the example binary to generate chat messages and write them to MCAP.
/// ```
///
/// # Returns
///
/// `Ok(())` after all messages are logged, or an error if telemetry initialization fails.
async fn main() -> anyhow::Result<()> {
    // Auto-discovers telemetry.toml config file, then override with MCAP
    let _telemetry = Telemetry::new()
        .with_service("chat-service-mcap", "1.0.0")
        .description("Chat service with MCAP logging")
        .environment(Environment::Development)
        .with_mcap("examples/chat-logs.mcap")
        .build()?;

    logger::info!("MCAP + Logging Example Started");
    logger::info!("Logs will be written to examples/chat-logs.mcap");

    let users = ["user-alice", "user-carlos", "user-yuki", "user-marie"];
    let languages = ["en", "es", "ja", "fr"];
    let message_types = ["text", "speech", "llm_response"];

    for i in 0..20 {
        let user_idx = i % users.len();

        let msg = ChatMessage {
            message_id: format!("msg-mcap-{:03}", i),
            room_id: "room-ai-chat".to_string(),
            user_id: users[user_idx].to_string(),
            language: languages[user_idx].to_string(),
            message_type: message_types[i % message_types.len()].to_string(),
            content: format!("Message {} from {}", i, users[user_idx]),
            timestamp: chrono::Utc::now().timestamp(),
        };

        match i % 5 {
            0 => {
                logger::debug!("DEBUG: Chat message").with_data(&msg);
            }
            1 => {
                logger::info!("INFO: Chat message").with_data(&msg);
            }
            2 => {
                logger::warn!("WARN: Chat message").with_data(&msg);
            }
            3 => {
                logger::error!("ERROR: Chat message").with_data(&msg);
            }
            _ => {
                logger::info!("INFO: Chat message").with_data(&msg);
            }
        }

        std::thread::sleep(std::time::Duration::from_millis(100));
    }

    logger::info!("MCAP example completed!");
    logger::info!("Check MCAP file: examples/chat-logs.mcap");

    Ok(())
}
