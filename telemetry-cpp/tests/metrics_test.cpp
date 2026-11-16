#include "telemetry/metrics/metrics.hpp"

#include <catch2/catch_test_macros.hpp>

#include <thread>
#include <vector>

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
