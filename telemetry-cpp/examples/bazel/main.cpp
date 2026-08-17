#include "telemetry/telemetry.hpp"

#include <string>
#include <chrono>
#include <cstdlib>

std::pair<std::string, uint16_t> get_otel_endpoint() {
    const char* env = std::getenv("OTEL_EXPORTER_OTLP_ENDPOINT");
    if (env) {
        std::string endpoint(env);
        auto pos = endpoint.find(':');
        if (pos != std::string::npos) {
            return {endpoint.substr(0, pos),
                    static_cast<uint16_t>(std::stoi(endpoint.substr(pos + 1)))};
        }
        return {endpoint, 4317};
    }
    return {"localhost", 4317};
}

struct ChatMessage {
    std::string message_id;
    std::string room_id;
    std::string user_id;
    std::string language;
    std::string content;
    int64_t timestamp;

    std::string to_json() const {
        return "{\"message_id\":\"" + message_id + "\","
               "\"room_id\":\"" + room_id + "\","
               "\"user_id\":\"" + user_id + "\","
               "\"language\":\"" + language + "\","
               "\"content\":\"" + content + "\","
               "\"timestamp\":" + std::to_string(timestamp) + "}";
    }
};

/**
 * @brief Runs the chat service telemetry and structured logging example.
 *
 * @return int Zero after completing the example.
 */
int main() {
    auto [otel_host, otel_port] = get_otel_endpoint();

    auto telemetry = telemetry::Telemetry::builder("chat-service", "1.0.0")
        .description("Simple chat service with logging")
        .environment(telemetry::Environment::Development)
        .with_otlp(otel_host, otel_port)
        .build();

    TELEMETRY_LOG_INFO("Chat service started");
    TELEMETRY_LOG_DEBUG("Debug mode enabled");
    TELEMETRY_LOG_INFO("OpenTelemetry logging example");

    int active_rooms = 3;
    int total_users = 42;
    std::string init_msg = "Service initialized with " + std::to_string(active_rooms) +
                           " active rooms and " + std::to_string(total_users) + " users";
    TELEMETRY_LOG_INFO(init_msg.c_str());

    auto now = std::chrono::system_clock::now();
    auto timestamp = std::chrono::duration_cast<std::chrono::seconds>(
        now.time_since_epoch()).count();

    ChatMessage msg1{
        "msg-001",
        "room-ai-chat",
        "user-alice",
        "en",
        "Hello! Can you help me with C++ programming?",
        timestamp
    };

    std::string user_msg = "User " + msg1.user_id + " sent message in room " + msg1.room_id;
    TELEMETRY_LOG_INFO(user_msg.c_str());
    TELEMETRY_LOG_INFO_DATA("User message received", msg1);

    ChatMessage msg2{
        "msg-002",
        "room-ai-chat",
        "user-carlos",
        "es",
        "¿Puedes explicarme cómo funcionan los templates en C++?",
        timestamp
    };

    std::string voice_msg = "Voice message from " + msg2.user_id + " in language: " + msg2.language;
    TELEMETRY_LOG_INFO(voice_msg.c_str());
    TELEMETRY_LOG_INFO_DATA("Voice message received", msg2);

    ChatMessage msg3{
        "msg-003",
        "room-ai-chat",
        "user-yuki",
        "ja",
        "C++のスマートポインタについて教えてください",
        timestamp
    };

    double rate_limit_percent = 85.5;
    std::string rate_msg = "Rate limit at " + std::to_string(rate_limit_percent) +
                           "% for user " + msg3.user_id;
    TELEMETRY_LOG_WARN(rate_msg.c_str());
    TELEMETRY_LOG_WARN_DATA("Rate limit approaching", msg3);

    int error_code = 500;
    std::string error_msg = "Failed to process message (error code: " +
                            std::to_string(error_code) + ") for user " + msg3.user_id;
    TELEMETRY_LOG_ERROR(error_msg.c_str());
    TELEMETRY_LOG_ERROR_DATA("Failed to process message", msg3);

    int total_messages = 3;
    double processing_time_ms = 123.45;
    std::string stats_msg = "Processed " + std::to_string(total_messages) +
                            " messages in " + std::to_string(processing_time_ms) + "ms";
    TELEMETRY_LOG_INFO(stats_msg.c_str());
    TELEMETRY_LOG_INFO("Chat service shutting down");

    return 0;
}
