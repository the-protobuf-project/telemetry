package metrics

import (
	"math"
	"runtime"
	"sync/atomic"
	_ "unsafe" // for go:linkname

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Counters and gauges are accumulated here rather than in the OTel SDK's
// synchronous instruments, whose Add takes a lock-free but shared path
// (attribute lookup, one shared atomic per series) on every call. A counter
// series is a set of per-P shards, so concurrent Adds land on different cache
// lines; an observable instrument reports the shards' total at collection
// time, including the final collection on Close.

//go:linkname procPin runtime.procPin
func procPin() int

//go:linkname procUnpin runtime.procUnpin
func procUnpin()

// shardHint is the current P's id. Pinning only reads it: the shard is
// updated atomically, so a goroutine that moves to another P afterwards
// costs some contention, never a lost increment.
func shardHint() int {
	id := procPin()
	procUnpin()
	return id
}

// maxShards bounds the memory of a series (128 bytes a shard) on machines
// with many Ps; beyond it Ps share shards, which is still correct.
const maxShards = 64

// shard is one P's part of a counter, alone on its cache line (128 bytes
// covers Apple silicon's lines and Intel's adjacent-line prefetch).
type shard struct {
	n atomic.Int64  // integral deltas, the common case: exact and one atomic add
	f atomic.Uint64 // float64 bits of the sum of the other deltas
	_ [112]byte
}

// counterSeries is one counter name and attribute set.
type counterSeries struct {
	shards []shard
	mask   int
	attrs  metric.ObserveOption // the series' attributes, built once for every collection
}

func newCounterSeries(set attribute.Set) *counterSeries {
	n := 1
	for n < min(runtime.GOMAXPROCS(0), maxShards) {
		n *= 2
	}
	return &counterSeries{shards: make([]shard, n), mask: n - 1, attrs: metric.WithAttributeSet(set)}
}

// add adds v, dropping a negative or NaN v as the OTel counter contract asks
// (a monotonic sum never decreases).
func (c *counterSeries) add(v float64) {
	if !(v >= 0) {
		return
	}
	s := &c.shards[shardHint()&c.mask]
	if v < 1<<53 {
		if i := int64(v); float64(i) == v {
			s.n.Add(i)
			return
		}
	}
	for {
		old := s.f.Load()
		if s.f.CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+v)) {
			return
		}
	}
}

func (c *counterSeries) sum() float64 {
	var n int64
	var f float64
	for i := range c.shards {
		n += c.shards[i].n.Load()
		f += math.Float64frombits(c.shards[i].f.Load())
	}
	return float64(n) + f
}

// gaugeSeries is one gauge name and attribute set: the value last set.
type gaugeSeries struct {
	bits  atomic.Uint64
	set   atomic.Bool // a gauge never set reports nothing rather than 0
	attrs metric.ObserveOption
}

func (g *gaugeSeries) store(v float64) {
	g.bits.Store(math.Float64bits(v))
	if !g.set.Load() {
		g.set.Store(true)
	}
}

// seriesList is the series of one observable instrument, copied on write so
// a collection reads it without a lock.
type seriesList[S any] struct {
	series atomic.Pointer[[]S]
}

func (l *seriesList[S]) add(s S) {
	var next []S
	if cur := l.series.Load(); cur != nil {
		next = append(next, *cur...)
	}
	next = append(next, s)
	l.series.Store(&next)
}

func (l *seriesList[S]) each(fn func(S)) {
	if cur := l.series.Load(); cur != nil {
		for _, s := range *cur {
			fn(s)
		}
	}
}
