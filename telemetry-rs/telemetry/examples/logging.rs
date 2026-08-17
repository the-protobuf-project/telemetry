use serde::Serialize;
use telemetry::{Environment, Telemetry, logger};

#[derive(Debug, Serialize)]
struct ChatMessage {
    message_id: String,
    room_id: String,
    user_id: String,
    language: String,
    content: String,
    timestamp: i64,
}

/// Runs the chat service demonstration and emits localized message, status, and processing logs.
///
/// # Errors
///
/// Returns an error if telemetry initialization fails.
///
/// # Examples
///
/// ```
/// main().expect("chat service should start successfully");
/// ```
#[tokio::main]
async fn main() -> anyhow::Result<()> {
    // Auto-discovers telemetry.toml config file
    let _telemetry = Telemetry::new()
        .with_service("chat-service", "1.0.0")
        .description("Simple chat service with logging")
        .environment(Environment::Development)
        .build()?;

    logger::info!("Chat service started");
    logger::debug!("Debug mode enabled");
    logger::info!("OpenTelemetry logging example");

    // Example: Format specifiers
    let active_rooms = 3;
    let total_users = 42;
    logger::info!(
        "Service initialized with {} active rooms and {} users",
        active_rooms,
        total_users
    );

    let msg1 = ChatMessage {
        message_id: "msg-001".to_string(),
        room_id: "room-ai-chat".to_string(),
        user_id: "user-alice".to_string(),
        language: "en".to_string(),
        content: "Hello! Can you help me with Rust programming?".to_string(),
        timestamp: chrono::Utc::now().timestamp(),
    };

    // Example: Format specifiers with structured data
    logger::info!(
        "User {} sent message in room {}",
        msg1.user_id,
        msg1.room_id
    );
    logger::info!("User message received").with_data(&msg1);
    logger::debug!(
        "Processing message from {} ({})",
        msg1.user_id,
        msg1.language
    )
    .with_data(&msg1);

    let msg2 = ChatMessage {
        message_id: "msg-002".to_string(),
        room_id: "room-ai-chat".to_string(),
        user_id: "user-carlos".to_string(),
        language: "es".to_string(),
        content: "¿Puedes explicarme cómo funcionan los traits en Rust?".to_string(),
        timestamp: chrono::Utc::now().timestamp(),
    };

    // Example: Format specifiers for different languages
    logger::info!(
        "Voice message from {} in language: {}",
        msg2.user_id,
        msg2.language
    );
    logger::info!("Voice message received").with_data(&msg2);

    let msg3 = ChatMessage {
        message_id: "msg-003".to_string(),
        room_id: "room-ai-chat".to_string(),
        user_id: "user-yuki".to_string(),
        language: "ja".to_string(),
        content: "Rustのライフタイムについて教えてください".to_string(),
        timestamp: chrono::Utc::now().timestamp(),
    };

    // Example: Format specifiers with warnings and errors
    let rate_limit_percent = 85.5;
    logger::warn!(
        "Rate limit at {:.1}% for user {}",
        rate_limit_percent,
        msg3.user_id
    );
    logger::warn!("Rate limit approaching").with_data(&msg3);

    let error_code = 500;
    logger::error!(
        "Failed to process message (error code: {}) for user {}",
        error_code,
        msg3.user_id
    );
    logger::error!("Failed to process message").with_data(&msg3);

    // Example: Format specifiers with statistics
    let total_messages = 3;
    let processing_time_ms = 123.45;
    logger::info!(
        "Processed {} messages in {:.2}ms",
        total_messages,
        processing_time_ms
    );
    logger::info!("Chat service shutting down");

    Ok(())
}
