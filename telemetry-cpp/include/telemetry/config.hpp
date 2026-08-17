#pragma once

#include <string>
#include <cstdint>

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

struct OtlpOptions {
    bool enabled = false;
    std::string host = "localhost";
    uint16_t port = 4317;
};

struct FoxgloveOptions {
    bool enabled = false;
    std::string mcap_path;
};

struct OpenTelemetryOptions {
    OtlpOptions otlp;
};

struct TelemetryOptions {
    OpenTelemetryOptions telemetry;
    FoxgloveOptions foxglove;
};

}  // namespace telemetry
