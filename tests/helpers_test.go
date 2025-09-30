package tests

import (
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/coachpo/meltica/core/events"
	"github.com/coachpo/meltica/core/recycler"
	"github.com/coachpo/meltica/internal/observability"
)

type trackingRecycler struct {
	impl         *recycler.RecyclerImpl
	recycled     atomic.Int64
	recycledPtrs sync.Map // map[uintptr]struct{}
	eventPool    *sync.Pool
}

func (t *trackingRecycler) pointer(ev *events.Event) uintptr {
	if ev == nil {
		return 0
	}
	return uintptr(unsafe.Pointer(ev))
}

func newTrackingRecycler(tb testing.TB) (*trackingRecycler, *sync.Pool) {
	tb.Helper()
	eventPool := &sync.Pool{New: func() any { return &events.Event{} }}
	mergedPool := &sync.Pool{New: func() any { return &events.MergedEvent{} }}
	execPool := &sync.Pool{New: func() any { return &events.ExecReport{} }}
	metrics := recycler.NewRecyclerMetrics(prometheus.NewRegistry())
	impl := recycler.NewRecycler(eventPool, mergedPool, execPool, metrics)
	return &trackingRecycler{impl: impl, eventPool: eventPool}, eventPool
}

func (t *trackingRecycler) storePointer(ev *events.Event) {
	if ev == nil {
		return
	}
	ptr := t.pointer(ev)
	t.recycledPtrs.Store(ptr, struct{}{})
}

func (t *trackingRecycler) WasRecycled(ev *events.Event) bool {
	if ev == nil {
		return false
	}
	ptr := t.pointer(ev)
	_, ok := t.recycledPtrs.Load(ptr)
	return ok
}

func (t *trackingRecycler) RecycleEvent(ev *events.Event) {
	t.recycled.Add(1)
	t.storePointer(ev)
	t.impl.RecycleEvent(ev)
}

func (t *trackingRecycler) RecycleMergedEvent(ev *events.MergedEvent) {
	t.impl.RecycleMergedEvent(ev)
}

func (t *trackingRecycler) RecycleExecReport(er *events.ExecReport) {
	t.impl.RecycleExecReport(er)
}

func (t *trackingRecycler) RecycleMany(events []*events.Event) {
	t.impl.RecycleMany(events)
}

func (t *trackingRecycler) EnableDebugMode() {
	t.impl.EnableDebugMode()
}

func (t *trackingRecycler) DisableDebugMode() {
	t.impl.DisableDebugMode()
}

func (t *trackingRecycler) CheckoutEvent(ev *events.Event) {
	t.impl.CheckoutEvent(ev)
}

func (t *trackingRecycler) CheckoutMergedEvent(ev *events.MergedEvent) {
	t.impl.CheckoutMergedEvent(ev)
}

func (t *trackingRecycler) CheckoutExecReport(er *events.ExecReport) {
	t.impl.CheckoutExecReport(er)
}

type captureLogger struct {
	mu      sync.Mutex
	message string
	fields  []observability.Field
}

func newCaptureLogger() *captureLogger {
	return &captureLogger{}
}

func (c *captureLogger) Debug(string, ...observability.Field) {}

func (c *captureLogger) Info(string, ...observability.Field) {}

func (c *captureLogger) Error(msg string, fields ...observability.Field) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.message = msg
	c.fields = append([]observability.Field(nil), fields...)
}

func (c *captureLogger) Snapshot() (string, []observability.Field) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.message, append([]observability.Field(nil), c.fields...)
}
