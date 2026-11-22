#include "telemetry/telemetry.hpp"

#include <chrono>
#include <cstdlib>
#include <filesystem>
#include <iostream>
#include <string>

struct ChatMessage {
    std::string message_id;
    std::string room_id;
    std::string user_id;
    std::string language;
    std::string content;
    int64_t timestamp;

    std::string to_json() const {
        return "{\"message_id\":\"" + message_id +
               "\","
               "\"room_id\":\"" +
               room_id +
               "\","
               "\"user_id\":\"" +
               user_id +
               "\","
               "\"language\":\"" +
               language +
               "\","
               "\"content\":\"" +
               content +
               "\","
               "\"timestamp\":" +
               std::to_string(timestamp) + "}";
    }
};

/**
 * @brief Where to write the MCAP file: the test's scratch directory under
 * `bazel test`, the working directory otherwise.
 */
std::filesystem::path mcap_path() {
    const char* tmp = std::getenv("TEST_TMPDIR");
    return std::filesystem::path(tmp ? tmp : ".") / "chat-service.mcap";
}

/**
 * @brief Runs a chat service through logs, metrics and a span, recording to
 * MCAP so no collector is needed.
 *
 * @return int Zero when the MCAP file was written, one otherwise.
 */
int main() {
    const auto path = mcap_path();

    auto telemetry = telemetry::Telemetry::builder("chat-service", "1.0.0")
                         .description("Simple chat service with logging")
                         .environment(telemetry::Environment::Development)
                         .with_mcap(path.string())
                         .build();

    TELEMETRY_LOG_INFO("Chat service started");

    auto timestamp =
        std::chrono::duration_cast<std::chrono::seconds>(std::chrono::system_clock::now().time_since_epoch()).count();

    ChatMessage messages[] = {
        {"msg-001", "room-ai-chat", "user-alice", "en", "Hello! Can you help me with C++ programming?", timestamp},
        {"msg-002", "room-ai-chat", "user-carlos", "es", "¿Puedes explicarme cómo funcionan los templates en C++?",
         timestamp},
        {"msg-003", "room-ai-chat", "user-yuki", "ja", "C++のスマートポインタについて教えてください", timestamp},
    };

    for (const auto& msg : messages) {
        TELEMETRY_SPAN(telemetry.tracer(), "handle_message");
        TELEMETRY_LOG_INFO_DATA("User message received", msg);
        telemetry.metrics().counter("messages_total", 1.0);
        telemetry.metrics().histogram("message_length", static_cast<double>(msg.content.size()));
    }

    TELEMETRY_LOG_WARN_DATA("Rate limit approaching", messages[2]);
    TELEMETRY_LOG_ERROR_DATA("Failed to process message", messages[2]);
    TELEMETRY_LOG_INFO("Chat service shutting down");

    telemetry.flush();
    telemetry.close();

    std::error_code ec;
    const auto size = std::filesystem::file_size(path, ec);
    if (ec || size == 0) {
        std::cerr << "no MCAP output at " << path << std::endl;
        return 1;
    }
    std::cout << "wrote " << size << " bytes to " << path << std::endl;
    return 0;
}
