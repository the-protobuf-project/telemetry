#include "telemetry/logging/logger.hpp"
#include "telemetry/logging/formatter.hpp"
#include "telemetry/mcap/writer.hpp"

#if TELEMETRY_USE_OTEL
#include "telemetry/otel/exporter.hpp"
#include <opentelemetry/logs/severity.h>
#endif

#include <iostream>

namespace telemetry::logging {

std::unique_ptr<Logger> GlobalLogger::instance_ = nullptr;

Logger::Logger(const std::string& service_name,
               const std::string& service_version,
               const std::string& environment)
    : service_name_(service_name)
    , service_version_(service_version)
    , environment_(environment)
    , mutex_(platform::create_mutex()) {

#if !TELEMETRY_PLATFORM_FREERTOS
    static int logger_counter = 0;
    std::string logger_name = service_name + "_" + std::to_string(++logger_counter);
    spdlog_logger_ = spdlog::stdout_color_mt(logger_name);
    spdlog_logger_->set_pattern("[%Y-%m-%d %H:%M:%S.%e] [%^%l%$] [" + service_name + "@" + service_version + "] [" + environment + "] %v");
#endif
}

Logger::Logger(const std::string& service_name,
               const std::string& service_version,
               const std::string& environment,
               std::shared_ptr<mcap::McapWriter> mcap_writer
#if TELEMETRY_USE_OTEL
               , otel::OtelExporter* otel_exporter
#endif
)
    : service_name_(service_name)
    , service_version_(service_version)
    , environment_(environment)
    , mcap_writer_(std::move(mcap_writer))
#if TELEMETRY_USE_OTEL
    , otel_exporter_(otel_exporter)
#endif
    , mutex_(platform::create_mutex()) {

#if !TELEMETRY_PLATFORM_FREERTOS
    static int logger_counter_mcap = 0;
    std::string logger_name = service_name + "_mcap_" + std::to_string(++logger_counter_mcap);
    spdlog_logger_ = spdlog::stdout_color_mt(logger_name);
    spdlog_logger_->set_pattern("[%Y-%m-%d %H:%M:%S.%e] [%^%l%$] [" + service_name + "@" + service_version + "] [" + environment + "] %v");
#endif

#if TELEMETRY_USE_OTEL
    if (otel_exporter_) {
        otel_logger_ = otel_exporter_->get_logger();
    }
#endif
}

Logger::~Logger() {
    platform::destroy_mutex(mutex_);
}

Logger::Logger(Logger&& other) noexcept
    : service_name_(std::move(other.service_name_))
    , service_version_(std::move(other.service_version_))
    , environment_(std::move(other.environment_))
    , level_(other.level_)
    , mcap_writer_(std::move(other.mcap_writer_))
    , sinks_(std::move(other.sinks_))
#if !TELEMETRY_PLATFORM_FREERTOS
    , spdlog_logger_(std::move(other.spdlog_logger_))
#endif
#if TELEMETRY_USE_OTEL
    , otel_exporter_(other.otel_exporter_)
    , otel_logger_(std::move(other.otel_logger_))
#endif
    , mutex_(platform::create_mutex()) {
}

Logger& Logger::operator=(Logger&& other) noexcept {
    if (this != &other) {
        platform::destroy_mutex(mutex_);
        service_name_ = std::move(other.service_name_);
        service_version_ = std::move(other.service_version_);
        environment_ = std::move(other.environment_);
        level_ = other.level_;
        mcap_writer_ = std::move(other.mcap_writer_);
        sinks_ = std::move(other.sinks_);
#if !TELEMETRY_PLATFORM_FREERTOS
        spdlog_logger_ = std::move(other.spdlog_logger_);
#endif
#if TELEMETRY_USE_OTEL
        otel_exporter_ = other.otel_exporter_;
        otel_logger_ = std::move(other.otel_logger_);
#endif
        mutex_ = platform::create_mutex();
    }
    return *this;
}

void Logger::set_level(Level level) {
    platform::ScopedLock lock(mutex_);
    level_ = level;

#if !TELEMETRY_PLATFORM_FREERTOS
    if (spdlog_logger_) {
        switch (level) {
            case Level::Trace: spdlog_logger_->set_level(spdlog::level::trace); break;
            case Level::Debug: spdlog_logger_->set_level(spdlog::level::debug); break;
            case Level::Info:  spdlog_logger_->set_level(spdlog::level::info); break;
            case Level::Warn:  spdlog_logger_->set_level(spdlog::level::warn); break;
            case Level::Error: spdlog_logger_->set_level(spdlog::level::err); break;
            case Level::Fatal: spdlog_logger_->set_level(spdlog::level::critical); break;
            case Level::Off:   spdlog_logger_->set_level(spdlog::level::off); break;
        }
    }
#endif
}

void Logger::add_sink(LogSink sink) {
    platform::ScopedLock lock(mutex_);
    sinks_.push_back(std::move(sink));
}

void Logger::trace(const char* message, const char* file, uint32_t line) {
    log(Level::Trace, message, file, line);
}

void Logger::debug(const char* message, const char* file, uint32_t line) {
    log(Level::Debug, message, file, line);
}

void Logger::info(const char* message, const char* file, uint32_t line) {
    log(Level::Info, message, file, line);
}

void Logger::warn(const char* message, const char* file, uint32_t line) {
    log(Level::Warn, message, file, line);
}

void Logger::error(const char* message, const char* file, uint32_t line) {
    log(Level::Error, message, file, line);
}

void Logger::fatal(const char* message, const char* file, uint32_t line) {
    log(Level::Fatal, message, file, line);
}

void Logger::log(Level level, const char* message, const char* file, uint32_t line,
                 const std::string& data_json) {
    if (static_cast<int>(level) < static_cast<int>(level_)) {
        return;
    }

    LogEntry entry;
    entry.level = level;
    entry.message = message;
    entry.file = file;
    entry.line = line;
    entry.timestamp_ns = platform::get_timestamp_ns();
    entry.data_json = data_json;

    log_to_console(entry);
    log_to_mcap(entry);
#if TELEMETRY_USE_OTEL
    log_to_otel(entry);
#endif

    platform::ScopedLock lock(mutex_);
    for (const auto& sink : sinks_) {
        sink(entry);
    }
}

void Logger::log_to_console(const LogEntry& entry) {
#if !TELEMETRY_PLATFORM_FREERTOS
    if (!spdlog_logger_) return;

    switch (entry.level) {
        case Level::Trace: spdlog_logger_->trace("{}", entry.message); break;
        case Level::Debug: spdlog_logger_->debug("{}", entry.message); break;
        case Level::Info:  spdlog_logger_->info("{}", entry.message); break;
        case Level::Warn:  spdlog_logger_->warn("{}", entry.message); break;
        case Level::Error: spdlog_logger_->error("{}", entry.message); break;
        case Level::Fatal: spdlog_logger_->critical("{}", entry.message); break;
        default: break;
    }

    if (!entry.data_json.empty()) {
        spdlog_logger_->info("  └─ {}", entry.data_json);
    }
#else
    std::string formatted = Formatter::format(entry, service_name_, service_version_, environment_);
    std::cout << formatted << std::endl;
#endif
}

void Logger::log_to_mcap(const LogEntry& entry) {
    if (!mcap_writer_) return;
    mcap_writer_->write_log(entry);
}

#if TELEMETRY_USE_OTEL
void Logger::log_to_otel(const LogEntry& entry) {
    if (!otel_logger_) return;

    namespace logs_api = opentelemetry::logs;

    logs_api::Severity severity;
    switch (entry.level) {
        case Level::Trace: severity = logs_api::Severity::kTrace; break;
        case Level::Debug: severity = logs_api::Severity::kDebug; break;
        case Level::Info:  severity = logs_api::Severity::kInfo; break;
        case Level::Warn:  severity = logs_api::Severity::kWarn; break;
        case Level::Error: severity = logs_api::Severity::kError; break;
        case Level::Fatal: severity = logs_api::Severity::kFatal; break;
        default: severity = logs_api::Severity::kInfo; break;
    }

    otel_logger_->EmitLogRecord(
        severity,
        entry.message
    );
}
#endif

void GlobalLogger::init(std::unique_ptr<Logger> logger) {
    instance_ = std::move(logger);
}

Logger* GlobalLogger::get() {
    return instance_.get();
}

void GlobalLogger::shutdown() {
    instance_.reset();
}

}  // namespace telemetry::logging
