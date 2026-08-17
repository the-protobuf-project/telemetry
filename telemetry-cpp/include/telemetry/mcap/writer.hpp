#pragma once

#include "telemetry/platform.hpp"
#include "telemetry/config.hpp"
#include "telemetry/logging/logger.hpp"

#include <string>
#include <memory>
#include <map>
#include <cstdint>

#if !TELEMETRY_PLATFORM_FREERTOS
#include <mcap/writer.hpp>
#include <fstream>
#endif

namespace telemetry::mcap {

struct Schema {
    std::string name;
    std::string encoding;
    std::string data;
};

class McapWriter {
public:
    McapWriter(const ServiceOptions& service_opts, const std::string& path);
    ~McapWriter();

    McapWriter(const McapWriter&) = delete;
    McapWriter& operator=(const McapWriter&) = delete;

    bool is_open() const { return is_open_; }
    const std::string& path() const { return path_; }

    uint16_t create_channel(const std::string& topic, const std::string& schema_name);

    void write_message(uint16_t channel_id, const uint8_t* data, size_t size,
                       uint64_t log_time, uint64_t publish_time);

    void write_log(const logging::LogEntry& entry);
    void write_metric(const std::string& name, const std::string& type, double value,
                      uint64_t timestamp_ns);
    void write_span(const std::string& name, const std::string& trace_id,
                    const std::string& span_id, uint64_t start_ns, uint64_t end_ns,
                    const std::string& status);

    void flush();
    void close();

private:
    void register_schemas();
    uint16_t get_or_create_schema(const std::string& schema_name);
    uint16_t ensure_channel(const std::string& topic, const std::string& schema_name);

    std::string path_;
    std::string service_name_;
    std::string service_version_;
    std::string environment_;
    bool is_open_ = false;
    bool closed_ = false;

    std::map<std::string, Schema> schemas_;
    std::map<std::string, uint16_t> schema_ids_;
    std::map<std::string, uint16_t> channel_ids_;

    uint16_t log_channel_id_ = 0;
    uint16_t metric_channel_id_ = 0;
    uint16_t span_channel_id_ = 0;

    platform::Mutex mutex_;

#if !TELEMETRY_PLATFORM_FREERTOS
    std::ofstream file_;
    ::mcap::McapWriter writer_;
#endif
};

}  // namespace telemetry::mcap
