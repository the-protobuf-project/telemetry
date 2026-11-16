#pragma once

#include "telemetry/config.hpp"

#include <atomic>
#include <chrono>
#include <condition_variable>
#include <cstdint>
#include <map>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

namespace telemetry::profiling {

/// Continuous CPU profiling to Pyroscope, as Go's WithProfiling and the other
/// SDKs' with_profiling do: the running thread's stack is sampled sample_hz
/// times a second of CPU, and the stacks seen are sent every upload_interval
/// to Pyroscope's ingest API, under "<service>.<environment>" with service,
/// version and environment tags. Pyroscope has no C++ client library; this
/// is one, for Linux and macOS.
struct ProfilingOptions {
    bool enabled = false;
    std::string server_address = "http://localhost:4040";
    std::map<std::string, std::string> tags;
    int sample_hz = 100;
    std::chrono::milliseconds upload_interval{10000};
    std::string basic_auth_user;
    std::string basic_auth_password;
};

/// One process may profile at a time: the sampling timer is process-wide.
class Profiler {
public:
    Profiler(const ServiceOptions& service, ProfilingOptions options);
    ~Profiler();

    Profiler(const Profiler&) = delete;
    Profiler& operator=(const Profiler&) = delete;

    /// Whether sampling started: false on platforms without a profiling
    /// timer, or when another Profiler is already running.
    bool running() const { return running_; }

    /// Stops sampling and sends what is left.
    void stop();

    /// Uploads that reached Pyroscope, and failed.
    uint64_t uploads() const { return uploads_.load(); }
    uint64_t failed_uploads() const { return failed_.load(); }

private:
    void run();
    void drain();
    void upload(std::chrono::system_clock::time_point from, std::chrono::system_clock::time_point until);
    std::string folded();
    const std::string& symbol(void* pc);

    ProfilingOptions options_;
    std::string name_;  // "<app>.cpu{tag=value,...}", as Pyroscope's ingest API takes it
    bool running_ = false;

    std::map<std::vector<void*>, uint64_t> stacks_;  // since the last upload
    std::map<void*, std::string> symbols_;

    std::thread thread_;
    std::mutex mutex_;
    std::condition_variable wake_;
    bool stopping_ = false;
    std::atomic<uint64_t> uploads_{0};
    std::atomic<uint64_t> failed_{0};
};

}  // namespace telemetry::profiling
