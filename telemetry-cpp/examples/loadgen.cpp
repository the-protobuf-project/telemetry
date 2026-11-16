// Writes n log records, spans or counter increments through this C++ SDK to
// an OTLP/HTTP collector and prints one JSON line with how long it took.
// Driven by bench/bench_all.py, which counts what reached the collector.
//
//   bazel run -c opt //examples:loadgen -- <signal> <n> <host:port>
#include "telemetry/telemetry.hpp"

#include <fcntl.h>
#include <unistd.h>

#include <algorithm>
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <string>
#include <thread>
#include <vector>

namespace {

long env_long(const char* name, long fallback) {
    const char* v = std::getenv(name);
    return v ? std::atol(v) : fallback;
}

// Calls fn n times across workers threads.
template <class F>
void fan_out(long n, long workers, F fn) {
    std::vector<std::thread> threads;
    const long per = n / workers;
    for (long w = 0; w < workers; ++w) {
        const long count = w == workers - 1 ? n - per * (workers - 1) : per;
        threads.emplace_back([count, &fn] {
            for (long i = 0; i < count; ++i) fn(i);
        });
    }
    for (auto& t : threads) t.join();
}

}  // namespace

int main(int argc, char** argv) {
    const std::string signal = argc > 1 ? argv[1] : "logs";
    const long n = argc > 2 ? std::atol(argv[2]) : 1000000;
    const std::string endpoint = argc > 3 ? argv[3] : "127.0.0.1:4318";
    const auto colon = endpoint.rfind(':');
    const std::string host = endpoint.substr(0, colon);
    const auto port = static_cast<uint16_t>(std::atoi(endpoint.substr(colon + 1).c_str()));

    // The console logger writes every record to stdout; the terminal is not
    // what is measured, so stdout points at /dev/null until the result.
    std::fflush(stdout);
    const int saved_stdout = dup(STDOUT_FILENO);
    const int devnull = open("/dev/null", O_WRONLY);
    dup2(devnull, STDOUT_FILENO);

    // LOADGEN_LOSSLESS=1 waits for room instead of dropping, LOADGEN_EXPORTERS
    // sets the exporters per signal, LOADGEN_WORKERS the threads writing, and
    // LOADGEN_CONSOLE=1 also prints every log line (off: measure the pipeline).
    const bool lossless = env_long("LOADGEN_LOSSLESS", 0) == 1;
    const long workers = std::max(env_long("LOADGEN_WORKERS", 1), 1L);
    telemetry::ExportOptions export_options;
    export_options.lossless = lossless;
    export_options.queue_size = static_cast<size_t>(env_long("LOADGEN_QUEUE", lossless ? 65536 : 2048));
    export_options.batch_size = lossless ? 8192 : 512;
    export_options.exporters = static_cast<size_t>(std::max(env_long("LOADGEN_EXPORTERS", 1), 1L));
    auto telemetry = telemetry::Telemetry::builder("loadgen-cpp", "1.0.0")
                         .with_otlp(host, port)
                         .with_export(export_options)
                         .with_console(env_long("LOADGEN_CONSOLE", 0) == 1)
                         .build();

    using clock = std::chrono::steady_clock;
    auto start = clock::now();
    if (signal == "logs") {
        fan_out(n, workers, [&](long) { telemetry.logger().info("load"); });
    } else if (signal == "spans") {
        fan_out(n, workers, [&](long) {
            auto span = telemetry.tracer().start_span("load.op");
            span.end();
        });
    } else if (signal == "metrics") {
        fan_out(n, workers, [&](long) { telemetry.metrics().counter("ops", 1.0); });
    } else {
        std::fprintf(stderr, "unknown signal %s\n", signal.c_str());
        return 2;
    }
    const double emit_s = std::chrono::duration<double>(clock::now() - start).count();

    start = clock::now();
    telemetry.close();
    const double close_s = std::chrono::duration<double>(clock::now() - start).count();

    std::fflush(stdout);
    dup2(saved_stdout, STDOUT_FILENO);
    std::printf("{\"lang\":\"cpp\",\"signal\":\"%s\",\"n\":%ld,\"workers\":%ld,\"emit_s\":%.9f,\"close_s\":%.9f}\n",
                signal.c_str(), n, workers, emit_s, close_s);
    return 0;
}
