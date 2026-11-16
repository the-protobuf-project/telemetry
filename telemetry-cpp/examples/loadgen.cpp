// Writes n log records, spans or counter increments through this C++ SDK to
// an OTLP/HTTP collector and prints one JSON line with how long it took.
// Driven by bench/bench_all.py, which counts what reached the collector.
//
//   bazel run -c opt //examples:loadgen -- <signal> <n> <host:port>
#include "telemetry/telemetry.hpp"

#include <fcntl.h>
#include <unistd.h>

#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <string>

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

    auto telemetry = telemetry::Telemetry::builder("loadgen-cpp", "1.0.0").with_otlp(host, port).build();

    using clock = std::chrono::steady_clock;
    auto start = clock::now();
    if (signal == "logs") {
        for (long i = 0; i < n; ++i) telemetry.logger().info("load");
    } else if (signal == "spans") {
        for (long i = 0; i < n; ++i) {
            auto span = telemetry.tracer().start_span("load.op");
            span.end();
        }
    } else if (signal == "metrics") {
        for (long i = 0; i < n; ++i) telemetry.metrics().counter("ops", 1.0);
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
    std::printf("{\"lang\":\"cpp\",\"signal\":\"%s\",\"n\":%ld,\"workers\":1,\"emit_s\":%.9f,\"close_s\":%.9f}\n",
                signal.c_str(), n, emit_s, close_s);
    return 0;
}
