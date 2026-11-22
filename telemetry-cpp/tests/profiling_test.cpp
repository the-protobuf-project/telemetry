#include "telemetry/profiling/profiler.hpp"

#include <catch2/catch_test_macros.hpp>

#include <arpa/inet.h>
#include <netinet/in.h>
#include <sys/socket.h>
#include <unistd.h>

#include <atomic>
#include <chrono>
#include <cmath>
#include <cstring>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

namespace {

// A stand-in for Pyroscope: answers every request 200 and keeps what it got.
class Ingest {
public:
    Ingest() {
        fd_ = socket(AF_INET, SOCK_STREAM, 0);
        sockaddr_in addr{};
        addr.sin_family = AF_INET;
        addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
        bind(fd_, reinterpret_cast<sockaddr*>(&addr), sizeof addr);
        socklen_t len = sizeof addr;
        getsockname(fd_, reinterpret_cast<sockaddr*>(&addr), &len);
        port_ = ntohs(addr.sin_port);
        listen(fd_, 8);
        thread_ = std::thread([this] { serve(); });
    }
    ~Ingest() {
        stop_ = true;
        shutdown(fd_, SHUT_RDWR);
        close(fd_);
        thread_.join();
    }
    std::string address() const { return "http://127.0.0.1:" + std::to_string(port_); }
    std::vector<std::string> requests() {
        std::lock_guard<std::mutex> lock(mutex_);
        return requests_;
    }

private:
    void serve() {
        while (!stop_) {
            const int c = accept(fd_, nullptr, nullptr);
            if (c < 0) return;
            std::string request;
            char buf[65536];
            size_t want = std::string::npos;
            while (request.size() < want) {
                const ssize_t n = recv(c, buf, sizeof buf, 0);
                if (n <= 0) break;
                request.append(buf, static_cast<size_t>(n));
                const auto end = request.find("\r\n\r\n");
                if (end != std::string::npos && want == std::string::npos) {
                    const auto cl = request.find("Content-Length: ");
                    want = end + 4 + (cl == std::string::npos ? 0 : std::stoul(request.substr(cl + 16)));
                }
            }
            const char* ok = "HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n";
            send(c, ok, std::strlen(ok), 0);
            close(c);
            std::lock_guard<std::mutex> lock(mutex_);
            requests_.push_back(request);
        }
    }

    int fd_ = -1;
    uint16_t port_ = 0;
    std::atomic<bool> stop_{false};
    std::thread thread_;
    std::mutex mutex_;
    std::vector<std::string> requests_;
};

}  // namespace

// Not inlined, so it is a frame of its own in the samples.
__attribute__((noinline)) double burn_cpu_for_profile_test(std::chrono::milliseconds how_long) {
    volatile double x = 0;
    const auto end = std::chrono::steady_clock::now() + how_long;
    while (std::chrono::steady_clock::now() < end) {
        for (int i = 0; i < 10000; ++i) x = x + std::sqrt(static_cast<double>(i));
    }
    return x;
}

TEST_CASE("the profiler sends the stacks it samples to Pyroscope's ingest API") {
    Ingest ingest;
    telemetry::profiling::ProfilingOptions opts;
    opts.enabled = true;
    opts.server_address = ingest.address();
    opts.sample_hz = 250;
    opts.upload_interval = std::chrono::milliseconds(500);
    opts.tags = {{"team", "core"}};

    telemetry::profiling::Profiler profiler(telemetry::ServiceOptions("profiling-test", "1.2.3"), opts);
    REQUIRE(profiler.running());
    burn_cpu_for_profile_test(std::chrono::milliseconds(1500));
    profiler.stop();

    const auto requests = ingest.requests();
    REQUIRE_FALSE(requests.empty());
    CHECK(profiler.failed_uploads() == 0);
    CHECK(profiler.uploads() == requests.size());

    bool named = false;
    bool sampled = false;
    for (const auto& r : requests) {
        // name=profiling-test.development.cpu{environment=development,service=profiling-test,team=core,version=1.2.3}
        named =
            named || (r.rfind("POST /ingest?name=profiling-test.development.cpu%7B", 0) == 0 &&
                      r.find("team%3Dcore") != std::string::npos && r.find("version%3D1.2.3") != std::string::npos &&
                      r.find("format=folded") != std::string::npos);
        sampled = sampled || r.find("burn_cpu_for_profile_test") != std::string::npos;
    }
    CHECK(named);
    CHECK(sampled);
}

TEST_CASE("only one profiler samples at a time") {
    telemetry::profiling::ProfilingOptions opts;
    opts.enabled = true;
    opts.server_address = "http://127.0.0.1:1";
    telemetry::profiling::Profiler first(telemetry::ServiceOptions("a", "1"), opts);
    telemetry::profiling::Profiler second(telemetry::ServiceOptions("b", "1"), opts);
    CHECK(first.running());
    CHECK_FALSE(second.running());
}
