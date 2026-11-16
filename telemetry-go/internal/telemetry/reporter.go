package telemetry

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// reportEvery bounds how often one kind of problem is reported: once when it
// first happens, then at most once per interval with how often it recurred.
const reportEvery = 30 * time.Second

// reporter tells the program what its telemetry could not do — exports that
// failed, records that were dropped — without letting a collector that is
// down flood the program's output. It is the OpenTelemetry error handler.
type reporter struct {
	errors  atomic.Int64
	lastErr atomic.Value // string

	mu         sync.Mutex
	out        slog.Handler
	last       map[string]time.Time
	suppressed map[string]int
}

func newReporter() *reporter {
	return &reporter{
		out:        slog.NewTextHandler(os.Stderr, nil),
		last:       map[string]time.Time{},
		suppressed: map[string]int{},
	}
}

// SetOutput sends reports to h: the program's own console log, once it has one.
func (r *reporter) SetOutput(h slog.Handler) {
	if h == nil {
		return
	}
	r.mu.Lock()
	r.out = h
	r.mu.Unlock()
}

// Handle implements otel.ErrorHandler.
func (r *reporter) Handle(err error) {
	if err == nil {
		return
	}
	r.errors.Add(1)
	r.lastErr.Store(err.Error())
	r.warn("error", "telemetry: export failed", "error", err.Error())
}

// warn reports msg unless a report of the same kind went out within
// reportEvery, in which case it is counted and summarised with the next one.
func (r *reporter) warn(kind, msg string, args ...any) {
	r.mu.Lock()
	now := time.Now()
	if t, ok := r.last[kind]; ok && now.Sub(t) < reportEvery {
		r.suppressed[kind]++
		r.mu.Unlock()
		return
	}
	if n := r.suppressed[kind]; n > 0 {
		args = append(args, "repeated", n)
	}
	r.last[kind] = now
	r.suppressed[kind] = 0
	out := r.out
	r.mu.Unlock()

	rec := slog.NewRecord(now, slog.LevelWarn, msg, 0)
	rec.Add(args...)
	_ = out.Handle(context.Background(), rec)
}

// Errors is how many errors were reported, and the latest.
func (r *reporter) Errors() (int64, string) {
	last, _ := r.lastErr.Load().(string)
	return r.errors.Load(), last
}
