package options

import (
	"os"
	"strconv"
	"time"
)

// ExportOptions tunes the queue each signal's records wait in before they are
// exported over OTLP, and what happens when a program produces them faster
// than the collector takes them.
//
// By default a full queue drops what does not fit, so telemetry can never
// stall the program; every drop is counted (see Telemetry.Stats), warned about
// while it happens, and reported by Close. Lossless instead makes the
// producer wait for room: nothing is dropped, and a slow or absent collector
// slows the program down to its pace.
//
// Zero fields keep the defaults, which follow the OpenTelemetry environment
// variables OTEL_BSP_* (spans) and OTEL_BLRP_* (log records) when set.
type ExportOptions struct {
	Lossless  bool          `json:"lossless"`   // block the producer when a queue is full instead of dropping
	QueueSize int           `json:"queue_size"` // records each signal may hold awaiting export (default 2048)
	BatchSize int           `json:"batch_size"` // records sent per export request (default 512)
	Interval  time.Duration `json:"interval"`   // longest a record waits for a batch to fill (default 1s logs, 5s spans)
	Timeout   time.Duration `json:"timeout"`    // longest one export request may take (default 30s)
	// Exporters is how many exporters (each with its own queue and
	// connection) share a signal's records, round robin. One exports about
	// 400k log records or 1.5M spans a second over gRPC; more raise the
	// ceiling, at the cost of records arriving slightly out of order.
	// Default 1.
	Exporters int `json:"exporters"`
}

// Batch is ExportOptions resolved for one signal: every field set.
type Batch struct {
	Lossless  bool
	QueueSize int
	BatchSize int
	Interval  time.Duration
	Timeout   time.Duration
	Exporters int
}

// Signal environment-variable prefixes, as the OpenTelemetry specification
// names them.
const (
	SpanEnvPrefix = "OTEL_BSP_"
	LogEnvPrefix  = "OTEL_BLRP_"
)

// Resolve fills in what o leaves unset for the signal whose environment
// variables start with envPrefix (SpanEnvPrefix or LogEnvPrefix), whose
// default interval is interval.
func (o ExportOptions) Resolve(envPrefix string, interval time.Duration) Batch {
	b := Batch{
		Lossless:  o.Lossless,
		QueueSize: firstPositive(o.QueueSize, envInt(envPrefix+"MAX_QUEUE_SIZE"), 2048),
		BatchSize: firstPositive(o.BatchSize, envInt(envPrefix+"MAX_EXPORT_BATCH_SIZE"), 512),
		Interval:  time.Duration(firstPositive(int64(o.Interval), envMillis(envPrefix+"SCHEDULE_DELAY"), int64(interval))),
		Timeout:   time.Duration(firstPositive(int64(o.Timeout), envMillis(envPrefix+"EXPORT_TIMEOUT"), int64(30*time.Second))),
		Exporters: firstPositive(o.Exporters, 1),
	}
	b.BatchSize = min(b.BatchSize, b.QueueSize)
	return b
}

func firstPositive[T int | int64](vs ...T) T {
	for _, v := range vs {
		if v > 0 {
			return v
		}
	}
	return 0
}

func envInt(key string) int {
	n, _ := strconv.Atoi(os.Getenv(key))
	return n
}

// envMillis reads key as milliseconds, the unit the specification gives it.
func envMillis(key string) int64 {
	return int64(envInt(key)) * int64(time.Millisecond)
}
