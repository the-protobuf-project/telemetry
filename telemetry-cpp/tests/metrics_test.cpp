#include "telemetry/metrics/metrics.hpp"
#include "telemetry/telemetry.hpp"

#include <catch2/catch_test_macros.hpp>

#include <atomic>
#include <cstdlib>
#include <string>
#include <thread>
#include <utility>
#include <vector>

#if TELEMETRY_USE_OTEL
#include <arpa/inet.h>
#include <netinet/in.h>
#include <poll.h>
#include <sys/socket.h>
#include <unistd.h>

#include <mutex>
#endif

using telemetry::metrics::Counter;
using telemetry::metrics::Gauge;
using telemetry::metrics::Metrics;

TEST_CASE("a counter adds every increment from every thread") {
    Metrics metrics(telemetry::ServiceOptions("metrics-test", "1.0.0"));
    std::vector<std::thread> threads;
    for (int t = 0; t < 8; ++t) {
        threads.emplace_back([&] {
            for (int i = 0; i < 10000; ++i) metrics.counter("ops", 1.0);
        });
    }
    for (auto& t : threads) t.join();
    REQUIRE(metrics.get_counter("ops").value() == 80000.0);
}

TEST_CASE("a counter adds fractions and whole numbers from every thread, by name or by handle") {
    Metrics metrics(telemetry::ServiceOptions("metrics-test", "1.0.0"));
    auto& handle = metrics.get_counter("bytes");
    std::vector<std::thread> threads;
    for (int t = 0; t < 16; ++t) {
        threads.emplace_back([&] {
            for (int i = 0; i < 1000; ++i) {
                handle.add(0.5);
                metrics.counter("bytes", 2.0);
            }
        });
    }
    for (auto& t : threads) t.join();
    REQUIRE(&metrics.get_counter(std::string("bytes")) == &handle);
    REQUIRE(handle.value() == 16 * 1000 * 2.5);
}

TEST_CASE("a name cached by a thread does not reach a registry made later") {
    for (int round = 0; round < 3; ++round) {
        // Made where the last one was, often; the cache must not mistake it.
        Metrics metrics(telemetry::ServiceOptions("metrics-test", "1.0.0"));
        metrics.counter("ops");
        metrics.gauge("depth", round);
        REQUIRE(metrics.get_counter("ops").value() == 1.0);
        REQUIRE(metrics.get_gauge("depth").value() == round);
    }
}

TEST_CASE("a registry moved to keeps its instruments, found by name") {
    Metrics from(telemetry::ServiceOptions("metrics-test", "1.0.0"));
    from.counter("ops", 3);
    Metrics to(std::move(from));
    to.counter("ops", 4);
    REQUIRE(to.get_counter("ops").value() == 7.0);
}

TEST_CASE("a gauge set by name holds the last value set") {
    Metrics metrics(telemetry::ServiceOptions("metrics-test", "1.0.0"));
    metrics.gauge("depth", 5);
    metrics.gauge("depth", 5);
    REQUIRE(metrics.get_gauge("depth").value() == 5);
    metrics.gauge("depth", 2);
    REQUIRE(metrics.get_gauge("depth").value() == 2);
}

TEST_CASE("a gauge sends changes, so what is behind it holds the last value set") {
    double exported = 0;
    Gauge gauge("depth", "", [&](double delta) { exported += delta; });
    gauge.set(5);
    gauge.set(5);
    REQUIRE(exported == 5);
    gauge.set(2);
    gauge.increment(3);
    gauge.decrement(1);
    REQUIRE(gauge.value() == 4);
    REQUIRE(exported == 4);
}

TEST_CASE("a counter passes each increment to its sink") {
    double exported = 0;
    Counter counter("ops", "", [&](double v) { exported += v; });
    counter.add(2);
    counter.add(3);
    REQUIRE(counter.value() == 5);
    REQUIRE(exported == 5);
}

#if TELEMETRY_USE_OTEL
namespace {
// Answers every OTLP/HTTP request with 200 and keeps the bodies sent to
// /v1/metrics, which the exporter sends as JSON when told to.
class MetricsReceiver {
public:
    MetricsReceiver() {
        listener_ = socket(AF_INET, SOCK_STREAM, 0);
        sockaddr_in addr{};
        addr.sin_family = AF_INET;
        addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
        bind(listener_, reinterpret_cast<sockaddr*>(&addr), sizeof(addr));
        socklen_t len = sizeof(addr);
        getsockname(listener_, reinterpret_cast<sockaddr*>(&addr), &len);
        port_ = ntohs(addr.sin_port);
        listen(listener_, 16);
        thread_ = std::thread([this] { serve(); });
    }

    ~MetricsReceiver() {
        stop_ = true;
        thread_.join();
        close(listener_);
    }

    uint16_t port() const { return port_; }

    std::vector<std::string> bodies() {
        std::lock_guard<std::mutex> lock(mutex_);
        return bodies_;
    }

private:
    void serve() {
        while (!stop_) {
            pollfd fd{listener_, POLLIN, 0};
            if (poll(&fd, 1, 20) <= 0) continue;
            const int conn = accept(listener_, nullptr, nullptr);
            if (conn < 0) continue;
            std::string request;
            char buf[65536];
            size_t header_end = std::string::npos;
            size_t length = 0;
            while (true) {
                const ssize_t n = recv(conn, buf, sizeof(buf), 0);
                if (n <= 0) break;
                request.append(buf, static_cast<size_t>(n));
                if (header_end == std::string::npos && (header_end = request.find("\r\n\r\n")) != std::string::npos) {
                    header_end += 4;
                    for (const char* key : {"Content-Length:", "content-length:"}) {
                        if (const auto at = request.find(key); at != std::string::npos && at < header_end) {
                            length = std::strtoul(request.c_str() + at + std::string(key).size(), nullptr, 10);
                        }
                    }
                }
                if (header_end != std::string::npos && request.size() >= header_end + length) break;
            }
            if (header_end != std::string::npos && request.find(" /v1/metrics ") != std::string::npos) {
                std::lock_guard<std::mutex> lock(mutex_);
                bodies_.push_back(request.substr(header_end));
            }
            const std::string ok = "HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n";
            send(conn, ok.data(), ok.size(), 0);
            close(conn);
        }
    }

    int listener_ = -1;
    uint16_t port_ = 0;
    std::atomic<bool> stop_{false};
    std::mutex mutex_;
    std::vector<std::string> bodies_;
    std::thread thread_;
};

// The value of the first data point of the metric named name, from the last
// body that has it: cumulative, so the last one holds the total.
double last_value(const std::vector<std::string>& bodies, const std::string& name) {
    for (auto body = bodies.rbegin(); body != bodies.rend(); ++body) {
        const auto at = body->find("\"name\":\"" + name + "\"");
        if (at == std::string::npos) continue;
        // The fields come in any order, so look within the metric's object.
        size_t begin = at;
        for (int depth = 0; begin > 0; --begin) {
            if ((*body)[begin] == '}') ++depth;
            if ((*body)[begin] == '{' && depth-- == 0) break;
        }
        size_t end = begin + 1;
        for (int depth = 0; end < body->size(); ++end) {
            if ((*body)[end] == '{') ++depth;
            if ((*body)[end] == '}' && depth-- == 0) break;
        }
        const auto value = body->find("\"asDouble\":", begin);
        if (value == std::string::npos || value > end) continue;
        return std::strtod(body->c_str() + value + 11, nullptr);
    }
    return -1;
}
}  // namespace

TEST_CASE("OpenTelemetry receives every increment and the last gauge value, at shutdown included") {
    MetricsReceiver receiver;
    setenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "http/json", 1);
    auto telemetry = telemetry::Telemetry::builder("metrics-test", "1.0.0").with_otlp("127.0.0.1", receiver.port()).build();
    auto& metrics = telemetry.metrics();
    std::vector<std::thread> threads;
    for (int t = 0; t < 8; ++t) {
        threads.emplace_back([&] {
            for (int i = 0; i < 25000; ++i) metrics.counter("ops.total");
        });
    }
    for (auto& t : threads) t.join();
    metrics.gauge("queue.depth", 9);
    metrics.gauge("queue.depth", 4);
    telemetry.close();
    unsetenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL");

    const auto bodies = receiver.bodies();
    REQUIRE_FALSE(bodies.empty());
    CHECK(last_value(bodies, "ops.total") == 200000.0);
    CHECK(last_value(bodies, "queue.depth") == 4.0);
    CHECK(metrics.get_counter("ops.total").value() == 200000.0);
    // The metrics carry the service's resource, not unknown_service.
    CHECK(bodies.back().find("\"stringValue\":\"metrics-test\"") != std::string::npos);
    CHECK(bodies.back().find("unknown_service") == std::string::npos);
}
#endif
