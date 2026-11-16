// Writes the load examples/loadgen.cpp writes — n log records, spans or
// counter increments — through the upstream OpenTelemetry C++ SDK alone, with
// its batch processors and OTLP/HTTP exporters, and prints one JSON line with
// how long it took. bench/bench_all.py runs it beside loadgen to compare this
// SDK with plain OpenTelemetry. The batch processors read the standard
// OTEL_BSP_* and OTEL_BLRP_* variables, which is how the bench tunes them;
// OTEL_BASELINE_WORKERS sets the threads writing.
//
//   bazel run -c opt //examples:otel_baseline -- <signal> <n> <host:port>
#include <opentelemetry/exporters/otlp/otlp_http_exporter.h>
#include <opentelemetry/exporters/otlp/otlp_http_log_record_exporter.h>
#include <opentelemetry/exporters/otlp/otlp_http_metric_exporter.h>
#include <opentelemetry/sdk/logs/batch_log_record_processor.h>
#include <opentelemetry/sdk/logs/logger_provider.h>
#include <opentelemetry/sdk/metrics/export/periodic_exporting_metric_reader.h>
#include <opentelemetry/sdk/metrics/meter_provider.h>
#include <opentelemetry/sdk/trace/batch_span_processor.h>
#include <opentelemetry/sdk/trace/tracer_provider.h>

#include <algorithm>
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <memory>
#include <string>
#include <thread>
#include <vector>

namespace otlp = opentelemetry::exporter::otlp;
namespace logs_sdk = opentelemetry::sdk::logs;
namespace metrics_sdk = opentelemetry::sdk::metrics;
namespace trace_sdk = opentelemetry::sdk::trace;

int main(int argc, char** argv) {
    const std::string signal = argc > 1 ? argv[1] : "logs";
    const long n = argc > 2 ? std::atol(argv[2]) : 1000000;
    const std::string base = std::string("http://") + (argc > 3 ? argv[3] : "127.0.0.1:4318");

    const char* workers_env = std::getenv("OTEL_BASELINE_WORKERS");
    const long workers = workers_env ? std::max(std::atol(workers_env), 1L) : 1;

    using clock = std::chrono::steady_clock;
    double emit_s = 0;
    double close_s = 0;
    auto timed = [&](auto&& emit, auto&& shutdown) {
        auto start = clock::now();
        std::vector<std::thread> threads;
        const long per = n / workers;
        for (long w = 0; w < workers; ++w) {
            const long count = w == workers - 1 ? n - per * (workers - 1) : per;
            threads.emplace_back([count, &emit] {
                for (long i = 0; i < count; ++i) emit(i);
            });
        }
        for (auto& t : threads) t.join();
        emit_s = std::chrono::duration<double>(clock::now() - start).count();
        start = clock::now();
        shutdown();
        close_s = std::chrono::duration<double>(clock::now() - start).count();
    };

    if (signal == "logs") {
        otlp::OtlpHttpLogRecordExporterOptions opts;
        opts.url = base + "/v1/logs";
        auto provider = std::make_shared<logs_sdk::LoggerProvider>(
            std::make_unique<logs_sdk::BatchLogRecordProcessor>(
                std::make_unique<otlp::OtlpHttpLogRecordExporter>(opts), logs_sdk::BatchLogRecordProcessorOptions{}));
        auto logger = provider->GetLogger("otel-baseline", "otel-baseline");
        timed(
            [&](long i) {
                auto record = logger->CreateLogRecord();
                record->SetSeverity(opentelemetry::logs::Severity::kInfo);
                record->SetBody("load");
                record->SetAttribute("i", static_cast<int64_t>(i));
                logger->EmitLogRecord(std::move(record));
            },
            [&] { provider->Shutdown(); });
    } else if (signal == "spans") {
        otlp::OtlpHttpExporterOptions opts;
        opts.url = base + "/v1/traces";
        auto provider = std::make_shared<trace_sdk::TracerProvider>(std::make_unique<trace_sdk::BatchSpanProcessor>(
            std::make_unique<otlp::OtlpHttpExporter>(opts), trace_sdk::BatchSpanProcessorOptions{}));
        auto tracer = provider->GetTracer("otel-baseline");
        timed([&](long) { tracer->StartSpan("load.op")->End(); }, [&] { provider->Shutdown(); });
    } else if (signal == "metrics") {
        otlp::OtlpHttpMetricExporterOptions opts;
        opts.url = base + "/v1/metrics";
        auto provider = std::make_shared<metrics_sdk::MeterProvider>();
        provider->AddMetricReader(std::make_shared<metrics_sdk::PeriodicExportingMetricReader>(
            std::make_unique<otlp::OtlpHttpMetricExporter>(opts), metrics_sdk::PeriodicExportingMetricReaderOptions{}));
        auto counter = provider->GetMeter("otel-baseline")->CreateDoubleCounter("ops");
        timed([&](long) { counter->Add(1.0); }, [&] { provider->Shutdown(); });
    } else {
        std::fprintf(stderr, "unknown signal %s\n", signal.c_str());
        return 2;
    }

    std::printf("{\"lang\":\"cpp\",\"signal\":\"%s\",\"n\":%ld,\"workers\":%ld,\"emit_s\":%.9f,\"close_s\":%.9f}\n",
                signal.c_str(), n, workers, emit_s, close_s);
    return 0;
}
