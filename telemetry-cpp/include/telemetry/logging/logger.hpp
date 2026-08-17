#pragma once

#include "telemetry/platform.hpp"
#include "telemetry/config.hpp"

#include <string>
#include <memory>
#include <functional>
#include <cstdint>

#if !TELEMETRY_PLATFORM_FREERTOS
#include <spdlog/spdlog.h>
#include <spdlog/sinks/stdout_color_sinks.h>
#include <spdlog/sinks/basic_file_sink.h>
#endif

#if TELEMETRY_USE_OTEL
#include <opentelemetry/logs/logger.h>
#endif

namespace telemetry {
namespace mcap {
class McapWriter;
}
namespace otel {
class OtelExporter;
}
}

namespace telemetry::logging {

enum class Level {
    Trace = 0,
    Debug = 1,
    Info = 2,
    Warn = 3,
    Error = 4,
    Fatal = 5,
    Off = 6
};

inline const char* level_to_string(Level level) {
    switch (level) {
        case Level::Trace: return "trace";
        case Level::Debug: return "debug";
        case Level::Info: return "info";
        case Level::Warn: return "warn";
        case Level::Error: return "error";
        case Level::Fatal: return "fatal";
        case Level::Off: return "off";
        default: return "unknown";
    }
}

struct LogEntry {
    Level level;
    std::string message;
    std::string file;
    uint32_t line;
    uint64_t timestamp_ns;
    std::string data_json;
};

using LogSink = std::function<void(const LogEntry&)>;

class Logger {
public:
    Logger(const std::string& service_name,
           const std::string& service_version,
           const std::string& environment);

    Logger(const std::string& service_name,
           const std::string& service_version,
           const std::string& environment,
           std::shared_ptr<mcap::McapWriter> mcap_writer
#if TELEMETRY_USE_OTEL
           , otel::OtelExporter* otel_exporter = nullptr
#endif
    );

    ~Logger();

    Logger(const Logger&) = delete;
    Logger& operator=(const Logger&) = delete;
    Logger(Logger&&) noexcept;
    Logger& operator=(Logger&&) noexcept;

    void set_level(Level level);
    Level get_level() const { return level_; }

    void add_sink(LogSink sink);

    void trace(const char* message, const char* file = "", uint32_t line = 0);
    void debug(const char* message, const char* file = "", uint32_t line = 0);
    void info(const char* message, const char* file = "", uint32_t line = 0);
    void warn(const char* message, const char* file = "", uint32_t line = 0);
    void error(const char* message, const char* file = "", uint32_t line = 0);
    void fatal(const char* message, const char* file = "", uint32_t line = 0);

    template<typename T>
    void trace(const char* message, const T& data, const char* file = "", uint32_t line = 0);
    template<typename T>
    void debug(const char* message, const T& data, const char* file = "", uint32_t line = 0);
    template<typename T>
    void info(const char* message, const T& data, const char* file = "", uint32_t line = 0);
    template<typename T>
    void warn(const char* message, const T& data, const char* file = "", uint32_t line = 0);
    template<typename T>
    void error(const char* message, const T& data, const char* file = "", uint32_t line = 0);
    template<typename T>
    void fatal(const char* message, const T& data, const char* file = "", uint32_t line = 0);

    const std::string& service_name() const { return service_name_; }
    const std::string& service_version() const { return service_version_; }
    const std::string& environment() const { return environment_; }

private:
    void log(Level level, const char* message, const char* file, uint32_t line,
             const std::string& data_json = "");
    void log_to_console(const LogEntry& entry);
    void log_to_mcap(const LogEntry& entry);
#if TELEMETRY_USE_OTEL
    void log_to_otel(const LogEntry& entry);
#endif

    std::string service_name_;
    std::string service_version_;
    std::string environment_;
    Level level_ = Level::Info;

    std::shared_ptr<mcap::McapWriter> mcap_writer_;
    std::vector<LogSink> sinks_;

#if !TELEMETRY_PLATFORM_FREERTOS
    std::shared_ptr<spdlog::logger> spdlog_logger_;
#endif

#if TELEMETRY_USE_OTEL
    otel::OtelExporter* otel_exporter_ = nullptr;
    opentelemetry::nostd::shared_ptr<opentelemetry::logs::Logger> otel_logger_;
#endif

    platform::Mutex mutex_;
};

class GlobalLogger {
public:
    static void init(std::unique_ptr<Logger> logger);
    static Logger* get();
    static void shutdown();

private:
    static std::unique_ptr<Logger> instance_;
};

template<typename T>
void Logger::trace(const char* message, const T& data, const char* file, uint32_t line) {
    log(Level::Trace, message, file, line, data.to_json());
}

template<typename T>
void Logger::debug(const char* message, const T& data, const char* file, uint32_t line) {
    log(Level::Debug, message, file, line, data.to_json());
}

template<typename T>
void Logger::info(const char* message, const T& data, const char* file, uint32_t line) {
    log(Level::Info, message, file, line, data.to_json());
}

template<typename T>
void Logger::warn(const char* message, const T& data, const char* file, uint32_t line) {
    log(Level::Warn, message, file, line, data.to_json());
}

template<typename T>
void Logger::error(const char* message, const T& data, const char* file, uint32_t line) {
    log(Level::Error, message, file, line, data.to_json());
}

template<typename T>
/**
 * @brief Logs a fatal message with structured data.
 *
 * @param data Object providing a `to_json()` method for serialization.
 * @param file Source file associated with the log entry.
 * @param line Source line associated with the log entry.
 */
void Logger::fatal(const char* message, const T& data, const char* file, uint32_t line) {
    log(Level::Fatal, message, file, line, data.to_json());
}

}  // namespace telemetry::logging

#define TELEMETRY_LOG_TRACE(msg) \
    if (auto* l = ::telemetry::logging::GlobalLogger::get()) l->trace(msg, __FILE__, __LINE__)
#define TELEMETRY_LOG_DEBUG(msg) \
    if (auto* l = ::telemetry::logging::GlobalLogger::get()) l->debug(msg, __FILE__, __LINE__)
#define TELEMETRY_LOG_INFO(msg) \
    if (auto* l = ::telemetry::logging::GlobalLogger::get()) l->info(msg, __FILE__, __LINE__)
#define TELEMETRY_LOG_WARN(msg) \
    if (auto* l = ::telemetry::logging::GlobalLogger::get()) l->warn(msg, __FILE__, __LINE__)
#define TELEMETRY_LOG_ERROR(msg) \
    if (auto* l = ::telemetry::logging::GlobalLogger::get()) l->error(msg, __FILE__, __LINE__)
#define TELEMETRY_LOG_FATAL(msg) \
    if (auto* l = ::telemetry::logging::GlobalLogger::get()) l->fatal(msg, __FILE__, __LINE__)

#define TELEMETRY_LOG_TRACE_DATA(msg, data) \
    if (auto* l = ::telemetry::logging::GlobalLogger::get()) l->trace(msg, data, __FILE__, __LINE__)
#define TELEMETRY_LOG_DEBUG_DATA(msg, data) \
    if (auto* l = ::telemetry::logging::GlobalLogger::get()) l->debug(msg, data, __FILE__, __LINE__)
#define TELEMETRY_LOG_INFO_DATA(msg, data) \
    if (auto* l = ::telemetry::logging::GlobalLogger::get()) l->info(msg, data, __FILE__, __LINE__)
#define TELEMETRY_LOG_WARN_DATA(msg, data) \
    if (auto* l = ::telemetry::logging::GlobalLogger::get()) l->warn(msg, data, __FILE__, __LINE__)
#define TELEMETRY_LOG_ERROR_DATA(msg, data) \
    if (auto* l = ::telemetry::logging::GlobalLogger::get()) l->error(msg, data, __FILE__, __LINE__)
#define TELEMETRY_LOG_FATAL_DATA(msg, data) \
    if (auto* l = ::telemetry::logging::GlobalLogger::get()) l->fatal(msg, data, __FILE__, __LINE__)
