#define MCAP_COMPRESSION_NO_LZ4
#define MCAP_COMPRESSION_NO_ZSTD
#define MCAP_IMPLEMENTATION
#include "telemetry/mcap/writer.hpp"
#include "telemetry/mcap/schemas.hpp"

#include <sstream>
#include <iomanip>
#include <ctime>

namespace telemetry::mcap {

namespace {

std::string timestamp_to_iso8601(uint64_t timestamp_ns) {
    auto time_s = timestamp_ns / 1000000000ULL;
    auto time_ns = timestamp_ns % 1000000000ULL;

    std::time_t t = static_cast<std::time_t>(time_s);
    std::tm* tm = std::gmtime(&t);

    std::ostringstream oss;
    oss << std::put_time(tm, "%Y-%m-%dT%H:%M:%S");
    oss << "." << std::setfill('0') << std::setw(9) << time_ns << "Z";
    return oss.str();
}

}  // namespace

McapWriter::McapWriter(const ServiceOptions& service_opts, const std::string& path)
    : path_(path)
    , service_name_(service_opts.name)
    , service_version_(service_opts.version)
    , environment_(environment_to_string(service_opts.environment))
    , mutex_(platform::create_mutex()) {

#if !TELEMETRY_PLATFORM_FREERTOS
    file_.open(path, std::ios::binary);
    if (!file_.is_open()) {
        return;
    }

    ::mcap::McapWriterOptions options("");
    options.library = "telemetry-cpp";
    writer_.open(file_, options);

    is_open_ = true;
    register_schemas();

    log_channel_id_ = ensure_channel("/telemetry/logs", "Log");
    metric_channel_id_ = ensure_channel("/telemetry/metrics", "Metric");
    span_channel_id_ = ensure_channel("/telemetry/spans", "Span");
#endif
}

McapWriter::~McapWriter() {
    close();
    platform::destroy_mutex(mutex_);
}

void McapWriter::register_schemas() {
    schemas_["Log"] = {"Log", "jsonschema", schemas::LOG_SCHEMA};
    schemas_["Metric"] = {"Metric", "jsonschema", schemas::METRIC_SCHEMA};
    schemas_["Span"] = {"Span", "jsonschema", schemas::SPAN_SCHEMA};
}

uint16_t McapWriter::get_or_create_schema(const std::string& schema_name) {
#if !TELEMETRY_PLATFORM_FREERTOS
    auto it = schema_ids_.find(schema_name);
    if (it != schema_ids_.end()) {
        return it->second;
    }

    auto schema_it = schemas_.find(schema_name);
    if (schema_it == schemas_.end()) {
        return 0;
    }

    ::mcap::Schema schema;
    schema.name = schema_it->second.name;
    schema.encoding = schema_it->second.encoding;
    schema.data.assign(
        reinterpret_cast<const std::byte*>(schema_it->second.data.data()),
        reinterpret_cast<const std::byte*>(schema_it->second.data.data() + schema_it->second.data.size())
    );

    writer_.addSchema(schema);
    schema_ids_[schema_name] = schema.id;
    return schema.id;
#else
    return 0;
#endif
}

uint16_t McapWriter::ensure_channel(const std::string& topic, const std::string& schema_name) {
#if !TELEMETRY_PLATFORM_FREERTOS
    auto it = channel_ids_.find(topic);
    if (it != channel_ids_.end()) {
        return it->second;
    }

    uint16_t schema_id = get_or_create_schema(schema_name);

    ::mcap::Channel channel;
    channel.topic = topic;
    channel.messageEncoding = "json";
    channel.schemaId = schema_id;

    writer_.addChannel(channel);
    channel_ids_[topic] = channel.id;
    return channel.id;
#else
    return 0;
#endif
}

uint16_t McapWriter::create_channel(const std::string& topic, const std::string& schema_name) {
    platform::ScopedLock lock(mutex_);
    return ensure_channel(topic, schema_name);
}

void McapWriter::write_message(uint16_t channel_id, const uint8_t* data, size_t size,
                                uint64_t log_time, uint64_t publish_time) {
#if !TELEMETRY_PLATFORM_FREERTOS
    if (!is_open_ || closed_) return;

    platform::ScopedLock lock(mutex_);

    ::mcap::Message msg;
    msg.channelId = channel_id;
    msg.sequence = 0;
    msg.logTime = log_time;
    msg.publishTime = publish_time;
    msg.data = reinterpret_cast<const std::byte*>(data);
    msg.dataSize = size;

    writer_.write(msg);
#endif
}

void McapWriter::write_log(const logging::LogEntry& entry) {
#if !TELEMETRY_PLATFORM_FREERTOS
    if (!is_open_ || closed_) return;

    std::ostringstream json;
    json << "{";
    json << "\"timestamp\":\"" << timestamp_to_iso8601(entry.timestamp_ns) << "\",";
    json << "\"level\":\"" << logging::level_to_string(entry.level) << "\",";
    json << "\"message\":\"" << entry.message << "\",";
    json << "\"name\":\"" << service_name_ << "\",";
    json << "\"file\":\"" << entry.file << "\",";
    json << "\"line\":" << entry.line;
    if (!entry.data_json.empty()) {
        json << ",\"data\":" << entry.data_json;
    }
    json << "}";

    std::string data = json.str();
    write_message(log_channel_id_,
                  reinterpret_cast<const uint8_t*>(data.data()),
                  data.size(),
                  entry.timestamp_ns, entry.timestamp_ns);
#endif
}

void McapWriter::write_metric(const std::string& name, const std::string& type,
                               double value, uint64_t timestamp_ns) {
#if !TELEMETRY_PLATFORM_FREERTOS
    if (!is_open_ || closed_) return;

    std::ostringstream json;
    json << "{";
    json << "\"timestamp\":\"" << timestamp_to_iso8601(timestamp_ns) << "\",";
    json << "\"name\":\"" << name << "\",";
    json << "\"type\":\"" << type << "\",";
    json << "\"value\":" << value;
    json << "}";

    std::string data = json.str();
    write_message(metric_channel_id_,
                  reinterpret_cast<const uint8_t*>(data.data()),
                  data.size(),
                  timestamp_ns, timestamp_ns);
#endif
}

void McapWriter::write_span(const std::string& name, const std::string& trace_id,
                             const std::string& span_id, uint64_t start_ns,
                             uint64_t end_ns, const std::string& status) {
#if !TELEMETRY_PLATFORM_FREERTOS
    if (!is_open_ || closed_) return;

    std::ostringstream json;
    json << "{";
    json << "\"trace_id\":\"" << trace_id << "\",";
    json << "\"span_id\":\"" << span_id << "\",";
    json << "\"name\":\"" << name << "\",";
    json << "\"start_time\":\"" << timestamp_to_iso8601(start_ns) << "\",";
    json << "\"end_time\":\"" << timestamp_to_iso8601(end_ns) << "\",";
    json << "\"status\":\"" << status << "\"";
    json << "}";

    std::string data = json.str();
    write_message(span_channel_id_,
                  reinterpret_cast<const uint8_t*>(data.data()),
                  data.size(),
                  start_ns, end_ns);
#endif
}

void McapWriter::flush() {
#if !TELEMETRY_PLATFORM_FREERTOS
    if (!is_open_ || closed_) return;
    platform::ScopedLock lock(mutex_);
    file_.flush();
#endif
}

void McapWriter::close() {
#if !TELEMETRY_PLATFORM_FREERTOS
    if (closed_) return;

    platform::ScopedLock lock(mutex_);
    if (is_open_) {
        writer_.close();
        file_.close();
        is_open_ = false;
    }
    closed_ = true;
#endif
}

}  // namespace telemetry::mcap
