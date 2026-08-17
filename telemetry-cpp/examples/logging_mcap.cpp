#include "telemetry/telemetry.hpp"

#include <string>
#include <chrono>
#include <thread>
#include <vector>

struct ChatMessage {
    std::string message_id;
    std::string room_id;
    std::string user_id;
    std::string language;
    std::string message_type;
    std::string content;
    int64_t timestamp;

    /**
     * @brief Serializes the chat message fields to a JSON object string.
     *
     * @return std::string JSON object containing the message metadata, content, and timestamp.
     */
    std::string to_json() const {
        return "{\"message_id\":\"" + message_id + "\","
               "\"room_id\":\"" + room_id + "\","
               "\"user_id\":\"" + user_id + "\","
               "\"language\":\"" + language + "\","
               "\"message_type\":\"" + message_type + "\","
               "\"content\":\"" + content + "\","
               "\"timestamp\":" + std::to_string(timestamp) + "}";
    }
};

/**
 * @brief Generates sample chat messages and writes telemetry logs to an MCAP file.
 *
 * @return 0 on successful completion.
 */
int main() {
    auto telemetry = telemetry::Telemetry::builder("chat-service-mcap", "1.0.0")
        .description("Chat service with MCAP logging")
        .environment(telemetry::Environment::Development)
        .with_mcap("examples/chat-logs.mcap")
        .build();

    TELEMETRY_LOG_INFO("MCAP + Logging Example Started");
    TELEMETRY_LOG_INFO("Logs will be written to examples/chat-logs.mcap");

    std::vector<std::string> users = {"user-alice", "user-carlos", "user-yuki", "user-marie"};
    std::vector<std::string> languages = {"en", "es", "ja", "fr"};
    std::vector<std::string> message_types = {"text", "speech", "llm_response"};

    for (int i = 0; i < 20; ++i) {
        size_t user_idx = i % users.size();

        auto now = std::chrono::system_clock::now();
        auto timestamp = std::chrono::duration_cast<std::chrono::seconds>(
            now.time_since_epoch()).count();

        ChatMessage msg{
            "msg-mcap-" + std::to_string(i),
            "room-ai-chat",
            users[user_idx],
            languages[user_idx],
            message_types[i % message_types.size()],
            "Message " + std::to_string(i) + " from " + users[user_idx],
            timestamp
        };

        switch (i % 5) {
            case 0:
                TELEMETRY_LOG_DEBUG_DATA("DEBUG: Chat message", msg);
                break;
            case 1:
                TELEMETRY_LOG_INFO_DATA("INFO: Chat message", msg);
                break;
            case 2:
                TELEMETRY_LOG_WARN_DATA("WARN: Chat message", msg);
                break;
            case 3:
                TELEMETRY_LOG_ERROR_DATA("ERROR: Chat message", msg);
                break;
            default:
                TELEMETRY_LOG_INFO_DATA("INFO: Chat message", msg);
                break;
        }

        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    TELEMETRY_LOG_INFO("MCAP example completed!");
    TELEMETRY_LOG_INFO("Check MCAP file: examples/chat-logs.mcap");

    return 0;
}
