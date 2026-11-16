# Benchmarks

Every SDK writes **1,000,000** log records, spans and counter increments over OTLP/gRPC to one
in-process counting collector, so the numbers show what each SDK **delivered**, not only how
fast the calls returned. Regenerate with `just bench`; redraw this page from the saved
numbers (`docs/benchmark.json`) with `just bench-render`.

Measured 2026-11-16 on Darwin arm64, 14 CPUs, Python 3.14.10t.
Console log output is off in every load generator (Go formats it to io.Discard), so the terminal is
not what is measured. **drop** is the default: a full export queue drops what does not fit,
counted and reported on close. **lossless** makes the writer wait for room instead.

## Starting point: the official SDKs

Each language's upstream OpenTelemetry SDK alone, with its default batch processor and
OTLP exporter, given the same burst. The calls return fast, and most of the records never
arrive: the batch processor's 2048-slot queue fills and drops the rest without a word.

```mermaid
xychart-beta
    title "Official SDKs: logs delivered of 1,000,000 (%)"
    x-axis ["Go", "Rust", "Python", "C++"]
    y-axis "% delivered" 0 --> 29.7472
    bar [27.0429, 11.3724, 14.4896, 20.7592]
```

```mermaid
xychart-beta
    title "Official SDKs: spans delivered of 1,000,000 (%)"
    x-axis ["Go", "Rust", "Python", "C++"]
    y-axis "% delivered" 0 --> 92.4803
    bar [42.5909, 14.3917, 28.5696, 84.073]
```

### Step by step, in Go

Each step changes one thing, starting from the official SDK. The last row delivers every
record at the official SDK's speed for logs and twice it for spans. The official run uses
one thread, so read this as what each SDK can be made to deliver, not a like-for-like race.

| Step | Logs delivered | Logs / s | Spans delivered | Spans / s |
| ---- | -------------: | -------: | --------------: | --------: |
| Official Go SDK alone | 270,429 | 1,712,027 | 425,909 | 1,922,065 |
| This SDK, default: drops counted and reported | 513,091 | 620,935 | 522,752 | 1,693,677 |
| Lossless: the writer waits for room | 1,000,000 | 332,789 | 1,000,000 | 1,050,896 |
| Lossless, one goroutine per CPU, batch 8192 | 1,000,000 | 394,537 | 1,000,000 | 1,496,277 |
| Lossless, plus 4 exporters in parallel | 1,000,000 | 1,206,449 | 1,000,000 | 3,937,499 |

```mermaid
xychart-beta
    title "Go logs: delivered (%) at each step"
    x-axis ["1", "2", "3", "4", "5"]
    y-axis "% delivered" 0 --> 110
    bar [27.0429, 51.3091, 100, 100, 100]
```

```mermaid
xychart-beta
    title "Go logs: written per second at each step"
    x-axis ["1", "2", "3", "4", "5"]
    y-axis "thousand / s" 0 --> 1883.23
    line [1712.03, 620.935, 332.789, 394.537, 1206.45]
```

```mermaid
xychart-beta
    title "Go spans: delivered (%) at each step"
    x-axis ["1", "2", "3", "4", "5"]
    y-axis "% delivered" 0 --> 110
    bar [42.5909, 52.2752, 100, 100, 100]
```

```mermaid
xychart-beta
    title "Go spans: written per second at each step"
    x-axis ["1", "2", "3", "4", "5"]
    y-axis "thousand / s" 0 --> 4331.25
    line [1922.06, 1693.68, 1050.9, 1496.28, 3937.5]
```

### Every language against its official SDK

This SDK's default and lossless modes beside the official SDK, one thread each. Above 1
means faster per call than the official SDK; the official SDK's speed is bought with the
records it drops.

```mermaid
xychart-beta
    title "Logs: calls per second vs the official SDK (x)"
    x-axis ["Go", "Rust", "Python", "C++"]
    y-axis "x official" 0 --> 4.91824
    bar [0.36269, 0.533667, 4.47112, 1.07777]
```

```mermaid
xychart-beta
    title "Spans: calls per second vs the official SDK (x)"
    x-axis ["Go", "Rust", "Python", "C++"]
    y-axis "x official" 0 --> 1.43448
    bar [0.881176, 0.520523, 1.30407, 0.990905]
```

```mermaid
xychart-beta
    title "Metrics: calls per second vs the official SDK (x)"
    x-axis ["Go", "Rust", "Python", "C++"]
    y-axis "x official" 0 --> 0.81813
    bar [0.422611, 0.249946, 0.743754, 0.342989]
```

| Language | Signal | Official: delivered | Official: rate | This SDK, drop: delivered | rate | Lossless: delivered | rate |
| -------- | ------ | ------------------: | -------------: | ------------------------: | ---: | ------------------: | ---: |
| Go | logs | 270,429 | 1,712,027/s | 513,091 | 620,935/s | 1,000,000 | 332,789/s |
| Rust | logs | 113,724 | 7,662,040/s | 170,540 | 4,088,980/s | 1,000,000 | 909,124/s |
| Python | logs | 144,896 | 121,144/s | 355,392 | 541,650/s | 1,000,000 | 252,319/s |
| C++ | logs | 207,592 | 4,665,579/s | 313,314 | 5,028,439/s | — | — |
| Go | spans | 425,909 | 1,922,065/s | 522,752 | 1,693,677/s | 1,000,000 | 1,050,896/s |
| Rust | spans | 143,917 | 7,959,605/s | 293,916 | 4,143,154/s | 1,000,000 | 1,824,392/s |
| Python | spans | 285,696 | 76,605/s | 1,000,000 | 99,898/s | 1,000,000 | 94,400/s |
| C++ | spans | 840,730 | 2,033,027/s | 908,298 | 2,014,538/s | — | — |
| Go | metrics | 1,000,000 | 19,191,678/s | 1,000,000 | 8,110,607/s | 1,000,000 | 8,162,349/s |
| Rust | metrics | 1,000,000 | 143,322,100/s | 1,000,000 | 35,822,767/s | 1,000,000 | 40,276,836/s |
| Python | metrics | 1,000,000 | 556,169/s | 1,000,000 | 413,653/s | 1,000,000 | 415,276/s |
| C++ | metrics | 1,000,000 | 75,778,145/s | 1,000,000 | 25,991,044/s | — | — |

## Head to head: the official SDKs tuned

Defaults are not the official SDKs' best. Here each one is tuned through the standard
`OTEL_BLRP_*` and `OTEL_BSP_*` variables to a queue that holds the whole burst and batches
of 8192, so it drops nothing, and this SDK runs lossless with several exporters per signal
(Go 4, Rust 8, Python 4, C++ 8). Both get the same 1,000,000 records from the same number of threads.
**Goodput** is records that reached the collector per second, emitting and flushing included;
each figure is the median of several runs.

```mermaid
xychart-beta
    title "Goodput vs the tuned official SDK, 1 thread (x)"
    x-axis ["Go logs", "Go metrics", "Go spans", "Rust logs", "Rust metrics", "Rust spans", "Python logs", "Python metrics", "Python spans", "C++ logs", "C++ metrics", "C++ spans"]
    y-axis "x official" 0 --> 29.8233
    bar [1.16794, 0.47698, 1.55141, 2.58289, 0.433197, 2.18988, 27.1121, 0.69303, 3.44642, 2.89871, 0.451528, 0.866928]
```

```mermaid
xychart-beta
    title "Goodput vs the tuned official SDK, 14 threads (x)"
    x-axis ["Go logs", "Go metrics", "Go spans", "Rust logs", "Rust metrics", "Rust spans", "Python logs", "Python metrics", "Python spans", "C++ logs", "C++ metrics", "C++ spans"]
    y-axis "x official" 0 --> 24.0702
    bar [2.16561, 0.939262, 3.32222, 2.79578, 0.831118, 2.64948, 21.882, 0.407884, 4.94902, 4.34061, 0.279257, 0.0670573]
```

| Language | Signal | Threads | Official, tuned | This SDK | Delivered (both) | vs official |
| -------- | ------ | ------: | --------------: | -------: | ---------------: | ----------: |
| Go | logs | 1 | 573,782/s | 670,146/s | 1,000,000 / 1,000,000 | **1.17x** |
| Go | metrics | 1 | 16,072,928/s | 7,666,470/s | 1,000,000 / 1,000,000 | **0.48x** |
| Go | spans | 1 | 1,377,204/s | 2,136,603/s | 1,000,000 / 1,000,000 | **1.55x** |
| Rust | logs | 1 | 1,250,337/s | 3,229,477/s | 1,000,000 / 1,000,000 | **2.58x** |
| Rust | metrics | 1 | 92,553,677/s | 40,094,019/s | 1,000,000 / 1,000,000 | **0.43x** |
| Rust | spans | 1 | 1,831,464/s | 4,010,688/s | 1,000,000 / 1,000,000 | **2.19x** |
| Python | logs | 1 | 17,950/s | 486,650/s | 638,976 / 1,000,000 | **27.11x** |
| Python | metrics | 1 | 576,917/s | 399,821/s | 1,000,000 / 1,000,000 | **0.69x** |
| Python | spans | 1 | 25,561/s | 88,093/s | 1,000,000 / 1,000,000 | **3.45x** |
| C++ | logs | 1 | 1,504,404/s | 4,360,825/s | 1,000,000 / 1,000,000 | **2.90x** |
| C++ | metrics | 1 | 53,890,803/s | 24,333,219/s | 1,000,000 / 1,000,000 | **0.45x** |
| C++ | spans | 1 | 2,674,140/s | 2,318,288/s | 1,000,000 / 1,000,000 | **0.87x** |
| Go | logs | 14 | 573,888/s | 1,242,820/s | 1,000,000 / 1,000,000 | **2.17x** |
| Go | metrics | 14 | 11,035,589/s | 10,365,308/s | 1,000,000 / 1,000,000 | **0.94x** |
| Go | spans | 14 | 1,041,592/s | 3,460,394/s | 1,000,000 / 1,000,000 | **3.32x** |
| Rust | logs | 14 | 1,033,579/s | 2,889,662/s | 1,000,000 / 1,000,000 | **2.80x** |
| Rust | metrics | 14 | 1,970,526/s | 1,637,740/s | 1,000,000 / 1,000,000 | **0.83x** |
| Rust | spans | 14 | 1,522,448/s | 4,033,702/s | 1,000,000 / 1,000,000 | **2.65x** |
| Python | logs | 14 | 17,671/s | 386,684/s | 704,512 / 1,000,000 | **21.88x** |
| Python | metrics | 14 | 162,051/s | 66,098/s | 1,000,000 / 1,000,000 | **0.41x** |
| Python | spans | 14 | 24,938/s | 123,420/s | 909,312 / 1,000,000 | **4.95x** |
| C++ | logs | 14 | 1,025,861/s | 4,452,865/s | 1,000,000 / 1,000,000 | **4.34x** |
| C++ | metrics | 14 | 29,523,600/s | 8,244,666/s | 1,000,000 / 1,000,000 | **0.28x** |
| C++ | spans | 14 | 1,423,678/s | 95,468/s | 1,000,000 / 1,000,000 | **0.07x** |

Under 2x: Go metrics, Rust metrics, Python metrics, C++ metrics, C++ spans.
C++ spans are bounded inside opentelemetry-cpp's tracer: every span copies
reference-counted pointers that all threads share, in the official SDK and this one alike.

## At a glance

### Logs

```mermaid
xychart-beta
    title "Logs delivered of 1,000,000 (%)"
    x-axis ["Go drop", "Go lossless", "Go lossless x14", "Go 4 exporters", "Rust drop", "Rust lossless", "Python drop", "Python lossless", "C++ drop", "Go OTel SDK", "Rust OTel SDK", "Python OTel SDK", "C++ OTel SDK"]
    y-axis "% delivered" 0 --> 110
    bar [51.3091, 100, 100, 100, 17.054, 100, 35.5392, 100, 31.3314, 27.0429, 11.3724, 14.4896, 20.7592]
```

```mermaid
xychart-beta
    title "Logs written per second"
    x-axis ["Go drop", "Go lossless", "Go lossless x14", "Go 4 exporters", "Rust drop", "Rust lossless", "Python drop", "Python lossless", "C++ drop", "Go OTel SDK", "Rust OTel SDK", "Python OTel SDK", "C++ OTel SDK"]
    y-axis "thousand / s" 0 --> 8428.24
    bar [620.935, 332.789, 394.537, 1206.45, 4088.98, 909.124, 541.65, 252.319, 5028.44, 1712.03, 7662.04, 121.144, 4665.58]
```

### Spans

```mermaid
xychart-beta
    title "Spans delivered of 1,000,000 (%)"
    x-axis ["Go drop", "Go lossless", "Go lossless x14", "Go 4 exporters", "Rust drop", "Rust lossless", "Python drop", "Python lossless", "C++ drop", "Go OTel SDK", "Rust OTel SDK", "Python OTel SDK", "C++ OTel SDK"]
    y-axis "% delivered" 0 --> 110
    bar [52.2752, 100, 100, 100, 29.3916, 100, 100, 100, 90.8298, 42.5909, 14.3917, 28.5696, 84.073]
```

```mermaid
xychart-beta
    title "Spans written per second"
    x-axis ["Go drop", "Go lossless", "Go lossless x14", "Go 4 exporters", "Rust drop", "Rust lossless", "Python drop", "Python lossless", "C++ drop", "Go OTel SDK", "Rust OTel SDK", "Python OTel SDK", "C++ OTel SDK"]
    y-axis "thousand / s" 0 --> 8755.57
    bar [1693.68, 1050.9, 1496.28, 3937.5, 4143.15, 1824.39, 99.898, 94.3995, 2014.54, 1922.06, 7959.61, 76.6046, 2033.03]
```

### Metrics

Every configuration delivers every increment: a counter exports its running total.

```mermaid
xychart-beta
    title "Counter increments per second"
    x-axis ["Go drop", "Go lossless", "Go lossless x14", "Go 4 exporters", "Rust drop", "Rust lossless", "Python drop", "Python lossless", "C++ drop", "Go OTel SDK", "Rust OTel SDK", "Python OTel SDK", "C++ OTel SDK"]
    y-axis "million / s" 0 --> 157.654
    bar [8.11061, 8.16235, 12.6262, 12.7442, 35.8228, 40.2768, 0.413653, 0.415276, 25.991, 19.1917, 143.322, 0.556169, 75.7781]
```

### Logs: speed against delivery

Right is faster (log scale, 1k/s to 10M/s); up is more of the records delivered.

```mermaid
quadrantChart
    title Logs: speed against delivery
    x-axis Slow --> Fast
    y-axis Loses records --> Delivers all
    quadrant-1 Fast and lossless
    quadrant-2 Lossless but slow
    quadrant-3 Slow and lossy
    quadrant-4 Fast but lossy
    Go drop: [0.698, 0.513]
    Go lossless: [0.631, 0.980]
    Go lossless x14: [0.649, 0.980]
    Go 4 exporters: [0.770, 0.980]
    Rust drop: [0.903, 0.171]
    Rust lossless: [0.740, 0.980]
    Python drop: [0.683, 0.355]
    Python lossless: [0.600, 0.980]
    C++ drop: [0.925, 0.313]
    Go OTel SDK: [0.808, 0.270]
    Rust OTel SDK: [0.971, 0.114]
    Python OTel SDK: [0.521, 0.145]
    C++ OTel SDK: [0.917, 0.208]
```

### Spans: speed against delivery

Right is faster (log scale, 1k/s to 10M/s); up is more of the records delivered.

```mermaid
quadrantChart
    title Spans: speed against delivery
    x-axis Slow --> Fast
    y-axis Loses records --> Delivers all
    quadrant-1 Fast and lossless
    quadrant-2 Lossless but slow
    quadrant-3 Slow and lossy
    quadrant-4 Fast but lossy
    Go drop: [0.807, 0.523]
    Go lossless: [0.755, 0.980]
    Go lossless x14: [0.794, 0.980]
    Go 4 exporters: [0.899, 0.980]
    Rust drop: [0.904, 0.294]
    Rust lossless: [0.815, 0.980]
    Python drop: [0.500, 0.980]
    Python lossless: [0.494, 0.980]
    C++ drop: [0.826, 0.908]
    Go OTel SDK: [0.821, 0.426]
    Rust OTel SDK: [0.975, 0.144]
    Python OTel SDK: [0.471, 0.286]
    C++ OTel SDK: [0.827, 0.841]
```

### Where the records went (default mode)

With the default drop-when-full queue, a burst faster than the collector loses what
does not fit; every SDK now counts it and reports it on close.

```mermaid
---
config:
  sankey:
    showValues: false
    linkColor: source
---
sankey-beta

Go drop logs,Delivered,513091
Go drop logs,Dropped,486909
Go drop spans,Delivered,522752
Go drop spans,Dropped,477248
Rust drop logs,Delivered,170540
Rust drop logs,Dropped,829460
Rust drop spans,Delivered,293916
Rust drop spans,Dropped,706084
Python drop logs,Delivered,355392
Python drop logs,Dropped,644608
C++ drop logs,Delivered,313314
C++ drop logs,Dropped,686686
C++ drop spans,Delivered,908298
C++ drop spans,Dropped,91702
Go OTel SDK logs,Delivered,270429
Go OTel SDK logs,Dropped,729571
Go OTel SDK spans,Delivered,425909
Go OTel SDK spans,Dropped,574091
Rust OTel SDK logs,Delivered,113724
Rust OTel SDK logs,Dropped,886276
Rust OTel SDK spans,Delivered,143917
Rust OTel SDK spans,Dropped,856083
Python OTel SDK logs,Delivered,144896
Python OTel SDK logs,Dropped,855104
Python OTel SDK spans,Delivered,285696
Python OTel SDK spans,Dropped,714304
C++ OTel SDK logs,Delivered,207592
C++ OTel SDK logs,Dropped,792408
C++ OTel SDK spans,Delivered,840730
C++ OTel SDK spans,Dropped,159270
```

```mermaid
pie showData title Rust OTel SDK: 1,000,000 logs
    "Delivered" : 113724
    "Dropped (queue full)" : 886276
```

### Go: more exporters, more throughput

Measured 2026-11-16: 2,000,000 records per signal, lossless, 14 goroutines, batch 8192, to the in-process collector. Upper line spans, lower line logs (thousand / s).

```mermaid
xychart-beta
    title "Lossless throughput by number of exporters"
    x-axis "exporters" [1, 2, 4, 8]
    y-axis "thousand / s" 0 --> 4865
    line [1486, 2745, 3631, 4423]
    line [372, 792, 1514, 1572]
```

### MCAP recording

```mermaid
xychart-beta
    title "MCAP records written per second"
    x-axis ["Go logs", "Go spans", "Go metrics", "Rust logs", "Rust spans", "Rust metrics", "Python logs", "Python spans", "Python metrics"]
    y-axis "thousand / s" 0 --> 2055.1
    bar [372.39, 847.057, 1819.47, 849.301, 617.319, 1868.27, 160.647, 56.4089, 158.99]
```

```mermaid
xychart-beta
    title "MCAP bytes per record (zstd)"
    x-axis ["Go logs", "Go spans", "Go metrics", "Rust logs", "Rust spans", "Rust metrics", "Python logs", "Python spans", "Python metrics"]
    y-axis "bytes" 0 --> 64.6876
    bar [28.5784, 58.8022, 19.5989, 28.659, 58.3725, 18.9879, 28.913, 58.8069, 23.9084]
```

### Go: cost of one call

```mermaid
xychart-beta
    title "Go SDK nanoseconds per call"
    x-axis ["LogInfo/export=false", "LogInfo/export=false-8", "LogInfo/export=true", "LogInfo/export=true-8", "Span/export=false", "Span/export=false-8", "Span/export=true", "Span/export=true-8", "Counter", "Counter-8"]
    y-axis "ns / op" 0 --> 1546.6
    bar [99.66, 75.45, 1406, 662.4, 100.3, 30.09, 539.2, 209.8, 116.9, 75.74]
```

## OTLP export

| SDK | Mode | Signal | Delivered | Lost | Emit rate | Per call | Flush on close |
| --- | ---- | ------ | --------: | ---: | --------: | -------: | -------------: |
| Go | default (drop when full), 1 goroutine | logs | 513,091 | 48.7% | 620,935/s | 1,610 ns | 0.01 s |
| Go | default (drop when full), 1 goroutine | spans | 522,752 | 47.7% | 1,693,677/s | 590 ns | 0.00 s |
| Go | default (drop when full), 1 goroutine | metrics | 1,000,000 | 0.0% | 8,110,607/s | 123 ns | 0.00 s |
| Go | Lossless, 1 goroutine | logs | 1,000,000 | 0.0% | 332,789/s | 3,005 ns | 0.01 s |
| Go | Lossless, 1 goroutine | spans | 1,000,000 | 0.0% | 1,050,896/s | 952 ns | 0.00 s |
| Go | Lossless, 1 goroutine | metrics | 1,000,000 | 0.0% | 8,162,349/s | 123 ns | 0.00 s |
| Go | Lossless, 14 goroutines, batch 8192 | logs | 1,000,000 | 0.0% | 394,537/s | 2,535 ns | 0.15 s |
| Go | Lossless, 14 goroutines, batch 8192 | spans | 1,000,000 | 0.0% | 1,496,277/s | 668 ns | 0.04 s |
| Go | Lossless, 14 goroutines, batch 8192 | metrics | 1,000,000 | 0.0% | 12,626,183/s | 79 ns | 0.00 s |
| Go | Lossless, 14 goroutines, batch 8192, 4 exporters | logs | 1,000,000 | 0.0% | 1,206,449/s | 829 ns | 0.02 s |
| Go | Lossless, 14 goroutines, batch 8192, 4 exporters | spans | 1,000,000 | 0.0% | 3,937,499/s | 254 ns | 0.04 s |
| Go | Lossless, 14 goroutines, batch 8192, 4 exporters | metrics | 1,000,000 | 0.0% | 12,744,157/s | 78 ns | 0.00 s |
| Rust | default (drop when full) | logs | 170,540 | 82.9% | 4,088,980/s | 245 ns | 0.00 s |
| Rust | default (drop when full) | spans | 293,916 | 70.6% | 4,143,154/s | 241 ns | 0.00 s |
| Rust | default (drop when full) | metrics | 1,000,000 | 0.0% | 35,822,767/s | 28 ns | 0.00 s |
| Rust | Lossless | logs | 1,000,000 | 0.0% | 909,124/s | 1,100 ns | 0.07 s |
| Rust | Lossless | spans | 1,000,000 | 0.0% | 1,824,392/s | 548 ns | 0.03 s |
| Rust | Lossless | metrics | 1,000,000 | 0.0% | 40,276,836/s | 25 ns | 0.00 s |
| Python | default (drop when full) | logs | 355,392 | 64.5% | 541,650/s | 1,846 ns | 0.01 s |
| Python | default (drop when full) | spans | 1,000,000 | 0.0% | 99,898/s | 10,010 ns | 0.00 s |
| Python | default (drop when full) | metrics | 1,000,000 | 0.0% | 413,653/s | 2,417 ns | 0.00 s |
| Python | Lossless | logs | 1,000,000 | 0.0% | 252,319/s | 3,963 ns | 0.29 s |
| Python | Lossless | spans | 1,000,000 | 0.0% | 94,400/s | 10,593 ns | 0.03 s |
| Python | Lossless | metrics | 1,000,000 | 0.0% | 415,276/s | 2,408 ns | 0.00 s |
| C++ | default (drop when full) | logs | 313,314 | 68.7% | 5,028,439/s | 199 ns | 0.00 s |
| C++ | default (drop when full) | spans | 908,298 | 9.2% | 2,014,538/s | 496 ns | 0.00 s |
| C++ | default (drop when full) | metrics | 1,000,000 | 0.0% | 25,991,044/s | 38 ns | 0.00 s |
| Go | official OpenTelemetry SDK alone (default batch processor) | logs | 270,429 | 73.0% | 1,712,027/s | 584 ns | 0.00 s |
| Go | official OpenTelemetry SDK alone (default batch processor) | spans | 425,909 | 57.4% | 1,922,065/s | 520 ns | 0.00 s |
| Go | official OpenTelemetry SDK alone (default batch processor) | metrics | 1,000,000 | 0.0% | 19,191,678/s | 52 ns | 0.00 s |
| Rust | official OpenTelemetry SDK alone (default batch processor) | logs | 113,724 | 88.6% | 7,662,040/s | 131 ns | 0.00 s |
| Rust | official OpenTelemetry SDK alone (default batch processor) | spans | 143,917 | 85.6% | 7,959,605/s | 126 ns | 0.00 s |
| Rust | official OpenTelemetry SDK alone (default batch processor) | metrics | 1,000,000 | 0.0% | 143,322,100/s | 7 ns | 0.00 s |
| Python | official OpenTelemetry SDK alone (default batch processor) | logs | 144,896 | 85.5% | 121,144/s | 8,255 ns | 0.13 s |
| Python | official OpenTelemetry SDK alone (default batch processor) | spans | 285,696 | 71.4% | 76,605/s | 13,054 ns | 0.10 s |
| Python | official OpenTelemetry SDK alone (default batch processor) | metrics | 1,000,000 | 0.0% | 556,169/s | 1,798 ns | 0.01 s |
| C++ | official OpenTelemetry SDK alone (default batch processor) | logs | 207,592 | 79.2% | 4,665,579/s | 214 ns | 0.00 s |
| C++ | official OpenTelemetry SDK alone (default batch processor) | spans | 840,730 | 15.9% | 2,033,027/s | 492 ns | 0.00 s |
| C++ | official OpenTelemetry SDK alone (default batch processor) | metrics | 1,000,000 | 0.0% | 75,778,145/s | 13 ns | 0.00 s |

**Lost** is what never reached the collector. Metrics are aggregated in-process (a counter
exports its running total), so they cannot lose increments; logs and spans are exported one by
one through a bounded queue, and a queue that fills either drops (default) or makes the
program wait (lossless). Every SDK counts what it dropped and reports it on close.

## MCAP recording

The same 1,000,000 records per signal, recorded to an MCAP file for Foxglove with OTLP off,
then read back with the `mcap` library: **Recorded** is what the file holds.

| SDK | Signal | Recorded | Write rate | Per call | File size | Bytes / record |
| --- | ------ | -------: | ---------: | -------: | --------: | -------------: |
| Go | logs | 1,000,000 | 372,390/s | 2,685 ns | 27.3 MiB | 29 |
| Go | spans | 1,000,000 | 847,057/s | 1,181 ns | 56.1 MiB | 59 |
| Go | metrics | 1,000,000 | 1,819,471/s | 550 ns | 18.7 MiB | 20 |
| Rust | logs | 1,000,000 | 849,301/s | 1,177 ns | 27.3 MiB | 29 |
| Rust | spans | 1,000,000 | 617,319/s | 1,620 ns | 55.7 MiB | 58 |
| Rust | metrics | 1,000,000 | 1,868,271/s | 535 ns | 18.1 MiB | 19 |
| Python | logs | 1,000,000 | 160,647/s | 6,225 ns | 27.6 MiB | 29 |
| Python | spans | 1,000,000 | 56,409/s | 17,728 ns | 56.1 MiB | 59 |
| Python | metrics | 1,000,000 | 158,990/s | 6,290 ns | 22.8 MiB | 24 |

## Go: cost per call

`go test -bench . ./telemetry-go` — `export=false` is the SDK's own overhead with nothing
exported; `export=true` exports to an in-process collector. `-8` runs 8 goroutines in parallel.

| Benchmark | ns/op | B/op | allocs/op |
| --------- | ----: | ---: | --------: |
| `BenchmarkLogInfo/export=false` | 99.66 | 336 | 2 |
| `BenchmarkLogInfo/export=false-8` | 75.45 | 336 | 2 |
| `BenchmarkLogInfo/export=true` | 1406 | 1386 | 5 |
| `BenchmarkLogInfo/export=true-8` | 662.4 | 1813 | 14 |
| `BenchmarkSpan/export=false` | 100.3 | 240 | 5 |
| `BenchmarkSpan/export=false-8` | 30.09 | 240 | 5 |
| `BenchmarkSpan/export=true` | 539.2 | 1139 | 7 |
| `BenchmarkSpan/export=true-8` | 209.8 | 1209 | 7 |
| `BenchmarkCounter` | 116.9 | 40 | 2 |
| `BenchmarkCounter-8` | 75.74 | 40 | 2 |
