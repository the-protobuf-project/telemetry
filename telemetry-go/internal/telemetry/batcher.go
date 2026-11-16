package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// SignalStats counts what happened to one signal's records.
type SignalStats struct {
	Emitted  int64 // handed to the pipeline by the program
	Exported int64 // accepted by the collector
	Dropped  int64 // discarded because the export queue was full, or arrived after Close
	Failed   int64 // in an export request that failed (collector down, rejected, timed out)
}

// Pending is what was emitted but neither exported, dropped nor failed yet:
// waiting in the queue or in an export in flight.
func (s SignalStats) Pending() int64 { return s.Emitted - s.Exported - s.Dropped - s.Failed }

type counters struct {
	emitted, exported, dropped, failed atomic.Int64
}

func (c *counters) stats() SignalStats {
	return SignalStats{
		Emitted: c.emitted.Load(), Exported: c.exported.Load(),
		Dropped: c.dropped.Load(), Failed: c.failed.Load(),
	}
}

// batcher queues one signal's records and exports them in batches from a
// single goroutine, since an OpenTelemetry exporter's Export must not be
// called concurrently.
//
// It replaces the SDK's own batch processors so every record is accounted
// for: theirs drop silently once their queue is full (at log verbosity no
// default logger prints), which is what made a burst of a million spans lose
// 97% of them with no sign of it.
//
// Writers append to the batch being filled under a mutex, and the exporter
// goroutine is woken once per full batch, not once per record: handing each
// record over a channel spent a third of the CPU waking goroutines.
type batcher[T any] struct {
	name   string // "spans", "log records": for messages
	cfg    options.Batch
	export func(context.Context, []T) error
	report *reporter
	count  *counters

	mu      sync.Mutex
	room    sync.Cond // broadcast when records leave the queue; lossless writers wait on it
	waiting int       // writers waiting on room
	batch   []T       // being filled
	ready   [][]T     // full batches, oldest first, waiting for the exporter
	free    [][]T     // exported batches, emptied, for reuse
	queued  int       // records in batch and ready
	stopped bool

	wake     chan struct{} // a batch is ready; buffered, so a writer never blocks on it
	flushReq chan chan struct{}
	done     chan struct{}   // closed by shutdown
	exited   chan struct{}   // closed when the exporter has sent everything and returned
	stopCtx  context.Context // bounds the final drain; set before done closes

	warnedDropped int64 // exporter-owned: drops already warned about
}

func newBatcher[T any](name string, cfg options.Batch, export func(context.Context, []T) error, r *reporter, c *counters) *batcher[T] {
	b := &batcher[T]{
		name: name, cfg: cfg, export: export, report: r, count: c,
		batch:    make([]T, 0, cfg.BatchSize),
		wake:     make(chan struct{}, 1),
		flushReq: make(chan chan struct{}),
		done:     make(chan struct{}),
		exited:   make(chan struct{}),
		stopCtx:  context.Background(),
	}
	b.room.L = &b.mu
	go b.run()
	return b
}

// enqueue adds one record to the batch being filled: waiting for room when
// lossless (until ctx ends or the batcher shuts down), dropping it otherwise.
func (b *batcher[T]) enqueue(ctx context.Context, item T) {
	b.count.emitted.Add(1)
	b.mu.Lock()
	for b.queued >= b.cfg.QueueSize && b.cfg.Lossless && !b.stopped && ctx.Err() == nil {
		b.waitForRoom(ctx)
	}
	if b.stopped || b.queued >= b.cfg.QueueSize {
		b.mu.Unlock()
		b.count.dropped.Add(1)
		return
	}
	b.batch = append(b.batch, item)
	b.queued++
	if len(b.batch) >= b.cfg.BatchSize {
		b.cut()
	}
	b.mu.Unlock()
}

// waitForRoom waits, with mu held, until a batch is exported, the batcher
// stops or ctx ends.
func (b *batcher[T]) waitForRoom(ctx context.Context) {
	b.waiting++
	if ctx.Done() != nil {
		stop := context.AfterFunc(ctx, func() {
			b.mu.Lock()
			b.room.Broadcast()
			b.mu.Unlock()
		})
		b.room.Wait()
		stop()
	} else {
		b.room.Wait()
	}
	b.waiting--
}

// cut moves the batch being filled, if any, to ready and wakes the exporter.
// mu must be held.
func (b *batcher[T]) cut() {
	if len(b.batch) == 0 {
		return
	}
	b.ready = append(b.ready, b.batch)
	if n := len(b.free); n > 0 {
		b.batch = b.free[n-1]
		b.free = b.free[:n-1]
	} else {
		b.batch = make([]T, 0, b.cfg.BatchSize)
	}
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// run exports ready batches as they come, a partly filled one every
// Interval, and everything on flush and shutdown.
func (b *batcher[T]) run() {
	defer close(b.exited)
	tick := time.NewTicker(b.cfg.Interval)
	defer tick.Stop()
	ctx := context.Background()
	for {
		select {
		case <-b.wake:
			b.exportReady(ctx, false)
		case <-tick.C:
			b.exportReady(ctx, true)
			b.warnDrops()
		case ack := <-b.flushReq:
			b.exportReady(ctx, true)
			close(ack)
		case <-b.done:
			b.exportReady(b.stopCtx, true)
			b.warnDrops()
			return
		}
	}
}

// exportReady exports ready batches, oldest first, until none is left;
// with partial set, the one being filled goes too.
func (b *batcher[T]) exportReady(ctx context.Context, partial bool) {
	b.mu.Lock()
	if partial {
		b.cut()
	}
	for len(b.ready) > 0 {
		batch := b.ready[0]
		b.ready[0] = nil
		b.ready = b.ready[1:]
		b.mu.Unlock()

		b.exportOne(ctx, batch)

		b.mu.Lock()
		b.queued -= len(batch)
		clear(batch) // drop references to what was exported
		b.free = append(b.free, batch[:0])
		if b.waiting > 0 {
			b.room.Broadcast()
		}
	}
	b.mu.Unlock()
}

func (b *batcher[T]) exportOne(ctx context.Context, batch []T) {
	ctx, cancel := context.WithTimeout(ctx, b.cfg.Timeout)
	err := b.export(ctx, batch)
	cancel()
	n := int64(len(batch))
	if err != nil {
		b.count.failed.Add(n)
		b.report.Handle(fmt.Errorf("exporting %d %s: %w", n, b.name, err))
	} else {
		b.count.exported.Add(n)
	}
}

// warnDrops says how many records were dropped since it last said so.
func (b *batcher[T]) warnDrops() {
	dropped := b.count.dropped.Load()
	if dropped == b.warnedDropped {
		return
	}
	b.report.warn("dropped:"+b.name, fmt.Sprintf("telemetry: dropped %s: export queue full", b.name),
		"dropped", dropped-b.warnedDropped, "total_dropped", dropped, "queue_size", b.cfg.QueueSize,
		"fix", "ExportOptions.Lossless, a larger QueueSize, or a faster collector")
	b.warnedDropped = dropped
}

// flush exports everything queued so far.
func (b *batcher[T]) flush(ctx context.Context) error {
	ack := make(chan struct{})
	select {
	case b.flushReq <- ack:
	case <-b.exited:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// shutdown exports what is queued, within ctx, and stops the worker.
func (b *batcher[T]) shutdown(ctx context.Context) error {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return nil
	}
	b.stopped = true
	b.stopCtx = ctx
	b.room.Broadcast() // lossless writers waiting for room drop instead
	b.mu.Unlock()
	close(b.done)
	select {
	case <-b.exited:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("flushing %s: %w", b.name, ctx.Err())
	}
}

// logProcessor is the sdklog.Processor in front of a log exporter.
type logProcessor struct {
	*batcher[sdklog.Record]
	exporter sdklog.Exporter
	once     sync.Once
}

func newLogProcessor(exp sdklog.Exporter, cfg options.Batch, r *reporter, c *counters) *logProcessor {
	return &logProcessor{
		batcher:  newBatcher("log records", cfg, exp.Export, r, c),
		exporter: exp,
	}
}

// Enabled reports every record wanted: level filtering happens before a
// record reaches OpenTelemetry.
func (p *logProcessor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }

// OnEmit queues a copy of record: the SDK reuses the one it passes.
func (p *logProcessor) OnEmit(ctx context.Context, record *sdklog.Record) error {
	p.enqueue(ctx, record.Clone())
	return nil
}

func (p *logProcessor) ForceFlush(ctx context.Context) error {
	if err := p.flush(ctx); err != nil {
		return err
	}
	return p.exporter.ForceFlush(ctx)
}

func (p *logProcessor) Shutdown(ctx context.Context) error {
	err := p.shutdown(ctx)
	p.once.Do(func() {
		if e := p.exporter.Shutdown(ctx); err == nil {
			err = e
		}
	})
	return err
}

// spanProcessor is the sdktrace.SpanProcessor in front of a span exporter.
type spanProcessor struct {
	*batcher[sdktrace.ReadOnlySpan]
	exporter sdktrace.SpanExporter
	once     sync.Once
}

func newSpanProcessor(exp sdktrace.SpanExporter, cfg options.Batch, r *reporter, c *counters) *spanProcessor {
	return &spanProcessor{
		batcher:  newBatcher("spans", cfg, exp.ExportSpans, r, c),
		exporter: exp,
	}
}

func (p *spanProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}

// OnEnd queues an ended span; one its sampler did not keep is not exported.
func (p *spanProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	if s.SpanContext().IsSampled() {
		p.enqueue(context.Background(), s)
	}
}

func (p *spanProcessor) ForceFlush(ctx context.Context) error { return p.flush(ctx) }

func (p *spanProcessor) Shutdown(ctx context.Context) error {
	err := p.shutdown(ctx)
	p.once.Do(func() {
		if e := p.exporter.Shutdown(ctx); err == nil {
			err = e
		}
	})
	return err
}

// shardLogs spreads log records over several processors, each with its own
// exporter, round robin: one exporter's Export must not run concurrently,
// so a single one caps how fast records leave the process (about 400k log
// records/s over gRPC); n of them export n batches at once. Records from
// different shards may reach the collector out of order; each keeps its
// timestamp.
func shardLogs(ps []*logProcessor) sdklog.Processor {
	if len(ps) == 1 {
		return ps[0]
	}
	return &logShards{ps: ps}
}

type logShards struct {
	ps   []*logProcessor
	next atomic.Uint64
}

func (s *logShards) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }

func (s *logShards) OnEmit(ctx context.Context, r *sdklog.Record) error {
	return s.ps[s.next.Add(1)%uint64(len(s.ps))].OnEmit(ctx, r)
}

func (s *logShards) ForceFlush(ctx context.Context) error {
	return eachShard(s.ps, func(p *logProcessor) error { return p.ForceFlush(ctx) })
}

func (s *logShards) Shutdown(ctx context.Context) error {
	return eachShard(s.ps, func(p *logProcessor) error { return p.Shutdown(ctx) })
}

// shardSpans spreads spans over several processors; see shardLogs.
func shardSpans(ps []*spanProcessor) sdktrace.SpanProcessor {
	if len(ps) == 1 {
		return ps[0]
	}
	return &spanShards{ps: ps}
}

type spanShards struct {
	ps   []*spanProcessor
	next atomic.Uint64
}

func (s *spanShards) OnStart(context.Context, sdktrace.ReadWriteSpan) {}

func (s *spanShards) OnEnd(sp sdktrace.ReadOnlySpan) {
	s.ps[s.next.Add(1)%uint64(len(s.ps))].OnEnd(sp)
}

func (s *spanShards) ForceFlush(ctx context.Context) error {
	return eachShard(s.ps, func(p *spanProcessor) error { return p.ForceFlush(ctx) })
}

func (s *spanShards) Shutdown(ctx context.Context) error {
	return eachShard(s.ps, func(p *spanProcessor) error { return p.Shutdown(ctx) })
}

// eachShard runs fn on every shard concurrently and joins their errors.
func eachShard[P any](ps []P, fn func(P) error) error {
	errs := make([]error, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Go(func() { errs[i] = fn(p) })
	}
	wg.Wait()
	return errors.Join(errs...)
}
