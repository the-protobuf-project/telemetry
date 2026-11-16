#pragma once

#include <cstddef>
#include <cstdint>
#include <string>

namespace telemetry {

enum class Environment {
    Development,
    Staging,
    Production
};

inline const char* environment_to_string(Environment env) {
    switch (env) {
        case Environment::Development: return "development";
        case Environment::Staging: return "staging";
        case Environment::Production: return "production";
        default: return "unknown";
    }
}

struct ServiceOptions {
    std::string name;
    std::string version;
    std::string description;
    Environment environment = Environment::Development;

    ServiceOptions(const std::string& n, const std::string& v)
        : name(n), version(v) {}

    ServiceOptions& with_description(const std::string& desc) {
        description = desc;
        return *this;
    }

    ServiceOptions& with_environment(Environment env) {
        environment = env;
        return *this;
    }
};

/// How log records and spans queue for OTLP export, and what happens when a
/// program produces them faster than the collector takes them. By default a
/// full queue drops what does not fit; every drop is counted
/// (Telemetry::stats) and reported by Telemetry::close. With lossless, the
/// writer waits for room instead, and nothing is dropped.
struct ExportOptions {
    bool lossless = false;    ///< wait for room when a queue is full instead of dropping
    size_t queue_size = 2048; ///< records each exporter may hold awaiting export
    size_t batch_size = 512;  ///< records sent per export request
    /// Exporters sharing each signal's records, each with its own queue and
    /// connection: one sends one batch at a time, n send n at once. Records
    /// from different exporters may reach the collector out of order; each
    /// keeps its timestamp.
    size_t exporters = 1;
};

struct OtlpOptions {
    bool enabled = false;
    std::string host = "localhost";
    uint16_t port = 4317;
    ExportOptions export_options;
};

/// What happened to one signal's records.
struct SignalStats {
    uint64_t emitted = 0;  ///< handed to the pipeline by the program
    uint64_t exported = 0; ///< accepted by the collector
    uint64_t failed = 0;   ///< in an export request that failed
    /// Not yet exported: still queued while running, dropped once closed.
    uint64_t pending() const { return emitted > exported + failed ? emitted - exported - failed : 0; }
};

struct Stats {
    SignalStats logs;
    SignalStats spans;
};

struct FoxgloveOptions {
    bool enabled = false;
    std::string mcap_path;
};

struct OpenTelemetryOptions {
    OtlpOptions otlp;
};

/// Continuous CPU profiling to Pyroscope (see profiling::Profiler).
struct ProfilingConfig {
    bool enabled = false;
    std::string server_address = "http://localhost:4040";
};

struct TelemetryOptions {
    OpenTelemetryOptions telemetry;
    FoxgloveOptions foxglove;
    ProfilingConfig profiling;
    /// Print each log line to stdout. Off, records still go to OTLP and MCAP.
    bool console = true;
};

}  // namespace telemetry
