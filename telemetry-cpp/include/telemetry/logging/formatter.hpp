#pragma once

#include "telemetry/logging/logger.hpp"
#include <string>
#include <sstream>
#include <iomanip>
#include <ctime>

namespace telemetry::logging {

class Formatter {
public:
    static std::string format(const LogEntry& entry,
                              const std::string& service_name,
                              const std::string& service_version,
                              const std::string& environment) {
        std::ostringstream oss;

        auto time_s = entry.timestamp_ns / 1000000000ULL;
        auto time_ms = (entry.timestamp_ns % 1000000000ULL) / 1000000ULL;

        std::time_t t = static_cast<std::time_t>(time_s);
        std::tm* tm = std::localtime(&t);

        oss << std::put_time(tm, "%Y-%m-%d %H:%M:%S");
        oss << "." << std::setfill('0') << std::setw(3) << time_ms;

        oss << " [" << level_to_string(entry.level) << "]";
        oss << " [" << service_name << "@" << service_version << "]";
        oss << " [" << environment << "]";

        if (!entry.file.empty()) {
            oss << " " << entry.file << ":" << entry.line;
        }

        oss << " " << entry.message;

        if (!entry.data_json.empty()) {
            oss << " | " << entry.data_json;
        }

        return oss.str();
    }

    static std::string level_color_code(Level level) {
#if TELEMETRY_PLATFORM_FREERTOS
        return "";
#else
        switch (level) {
            case Level::Trace: return "\033[90m";   // Gray
            case Level::Debug: return "\033[36m";   // Cyan
            case Level::Info:  return "\033[32m";   // Green
            case Level::Warn:  return "\033[33m";   // Yellow
            case Level::Error: return "\033[31m";   // Red
            case Level::Fatal: return "\033[35m";   // Magenta
            default: return "";
        }
#endif
    }

    static std::string reset_color() {
#if TELEMETRY_PLATFORM_FREERTOS
        return "";
#else
        return "\033[0m";
#endif
    }
};

}  // namespace telemetry::logging
