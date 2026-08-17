// Command telemetry is a runnable, checked-in proof that the
// protoc-gen-telemetry pipeline works end to end against the real SDK: an
// annotated proto (../../../examples/proto/jobs/v1/job.proto) generates a
// typed JobMetrics type (gen/jobs/v1/jobsv1/job_telemetry.pb.go) bound to
// runtime-go/telemetry.Meter, and this module wires that generated code to a
// real *telemetrysdk.Telemetry instance via its Meter() adapter
// (../../meter.go) — the actual Phase C2 bridge, not a stand-in.
//
// This module is isolated from the main SDK module (see go.mod) so its
// dependency graph never touches the plugin's or runtime-go/telemetry's own
// go.mod — the same reason orm/examples is its own module.
//
// Regenerate gen/ after editing job.proto (from the repo root):
//
//	go build -o telemetry-go/bin/protoc-gen-telemetry ./telemetry-go/plugin/cmd/protoc-gen-telemetry
//	buf generate --template buf.gen.example.yaml
//
// Run this example:
//
//	go run .
package main

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	telemetrysdk "github.com/the-protobuf-project/telemetry/telemetry-go"
	"github.com/the-protobuf-project/runtime-go/telemetry"

	"github.com/the-protobuf-project/telemetry/telemetry-go/examples/telemetry/gen/jobs/v1/jobsv1"
)

// Job is the mock service's own record — a plain struct, not the proto
// message; JobMetrics only ever sees the individual field values a call site
// passes it, the same as it would for any other caller.
type Job struct {
	Name    string
	Queue   string
	State   jobsv1.Job_State
	Attempt int32
}

// JobService is the "mock service object" wiring everything together: a
// generated JobMetrics for the fields telemetry.v1 knows about (state,
// queue, attempt, duration, retries), plus one instrument built directly
// from the same Meter for something the proto schema doesn't model — the
// count of jobs currently in flight. Both styles read through the exact same
// runtime-go/telemetry.Meter, which is the point: generated and hand-written
// instrumentation compose without needing two different APIs.
type JobService struct {
	metrics *jobsv1.JobMetrics

	// activeJobs demonstrates telemetry.UpDownCounter directly (built from
	// the same Meter, not through generated code): Add(ctx, +1, ...) when a
	// job starts, Add(ctx, -1, ...) when it finishes — the "add or decrease
	// the counter" pattern an UpDownCounter exists for, which a monotonic
	// Counter (like JobMetrics' own created/retries) can't do.
	activeJobs telemetry.UpDownCounter
}

// NewJobService builds a JobService against m — pass a real backend's Meter
// (as returned by (*telemetrysdk.Telemetry).Meter(), see main below)
// in production, or telemetry.NoopMeter / a small fake (see
// runtime-go/telemetry's own tests for the pattern) in a unit test. Neither
// JobService nor the generated JobMetrics code changes either way.
func NewJobService(m telemetry.Meter) *JobService {
	return &JobService{
		metrics:    jobsv1.NewJobMetrics(m),
		activeJobs: m.UpDownCounter("jobs_active", telemetry.WithUnit("1")),
	}
}

// Create starts a new job: increments the lifecycle counter, records the
// starting attempt count, and adds one to jobs_active.
func (s *JobService) Create(ctx context.Context, queue string) *Job {
	job := &Job{
		Name:    fmt.Sprintf("job-%d", rand.Intn(100000)),
		Queue:   queue,
		State:   jobsv1.Job_STATE_QUEUED,
		Attempt: 1,
	}
	s.metrics.IncCreated(ctx, job.State, job.Queue)
	s.metrics.SetAttempt(ctx, job.Attempt, job.State, job.Queue)
	s.activeJobs.Add(ctx, 1, telemetry.Labels{"queue": job.Queue})
	return job
}

// Retry bumps a job's attempt count: records the new gauge value and adds one
// to the (monotonic) retry counter.
func (s *JobService) Retry(ctx context.Context, job *Job) {
	job.Attempt++
	job.State = jobsv1.Job_STATE_RUNNING
	s.metrics.SetAttempt(ctx, job.Attempt, job.State, job.Queue)
	s.metrics.AddRetries(ctx, 1, job.State, job.Queue)
}

// Complete finishes a job (success or failure): records latency and
// duration, and subtracts one from jobs_active — the "decrease" half of the
// UpDownCounter pair Create's Add(ctx, 1, ...) started.
func (s *JobService) Complete(ctx context.Context, job *Job, elapsed time.Duration, failed bool) {
	job.State = jobsv1.Job_STATE_SUCCEEDED
	if failed {
		job.State = jobsv1.Job_STATE_FAILED
	}
	ms := float64(elapsed.Microseconds()) / 1000
	s.metrics.RecordLatency(ctx, ms, job.State, job.Queue)
	s.metrics.RecordDuration(ctx, ms, job.State, job.Queue)
	s.activeJobs.Add(ctx, -1, telemetry.Labels{"queue": job.Queue})
}

// main runs the job service telemetry example and records its metrics to an MCAP file.
func main() {
	ctx := context.Background()

	// Initialize the real SDK — the same telemetrysdk.New()...Build() any
	// other service using this SDK calls. WithMCAP records everything to a
	// local .mcap file (open it in Foxglove Studio to see the metrics this
	// run produces); WithOTLP additionally ships them to a live collector —
	// point it at your own (e.g. an otel-collector container) or drop the
	// call entirely to only record to MCAP.
	p, err := telemetrysdk.New().
		WithService("job-worker", "1.0.0").
		WithLabel("example", "protoc-gen-telemetry").
		WithMCAP("./job-service.mcap").
		WithOTLP("localhost", 6009).
		Build()
	if err != nil {
		panic(err)
	}
	defer func() {
		// If WithOTLP above is removed, Build()'s own autoConfigureOTLP
		// quirk still auto-enables the OTLP exporter whenever the legacy
		// Host field is non-empty, which defaults to "localhost" even with
		// zero config (see options/default.go's DefaultTelemetry — a
		// pre-existing SDK behavior, not this example's) — so Close()'s
		// metric flush would still try 127.0.0.1:4317 and fail with nothing
		// listening there. Harmless either way: the metrics were already
		// recorded into job-service.mcap regardless of whether the flush
		// below succeeds.
		if err := p.Close(); err != nil {
			fmt.Printf("(expected if no OTLP collector is reachable) %v\n", err)
		}
	}()

	// This is the Phase C2 bridge: p.Meter() adapts the SDK instance Build()
	// just configured into runtime-go/telemetry.Meter, so every measurement
	// below flows through the SDK's real instrument pipeline (and into
	// job-service.mcap) — not a stand-in.
	svc := NewJobService(p.Meter())

	p.Logger.Info("job service started", map[string]interface{}{"queue": "default"})

	job := svc.Create(ctx, "default")
	p.Logger.Info("job created", map[string]interface{}{"job": job.Name, "attempt": job.Attempt})

	start := time.Now()
	time.Sleep(15 * time.Millisecond) // stand-in for real work

	svc.Retry(ctx, job)
	p.Logger.Info("job retried", map[string]interface{}{"job": job.Name, "attempt": job.Attempt})

	failed := rand.Intn(4) == 0 // occasionally simulate a failure
	svc.Complete(ctx, job, time.Since(start), failed)
	p.Logger.Info("job finished", map[string]interface{}{"job": job.Name, "state": job.State.String()})

	fmt.Println("done — see ./job-service.mcap (open in Foxglove Studio) for the recorded metrics")
}
