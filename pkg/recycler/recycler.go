package recycler

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/coachpo/meltica/pkg/events"
)

// RecyclerImpl provides the concrete implementation backed by sync.Pool instances.
type RecyclerImpl struct { //nolint:revive
	eventPool      *sync.Pool
	execReportPool *sync.Pool
	metrics        *RecyclerMetrics
	debugEnabled   atomic.Bool
	putTracker     sync.Map
}

// NewRecycler constructs a RecyclerImpl with the provided pools and metrics instruments.
func NewRecycler(eventPool, execReportPool *sync.Pool, metrics *RecyclerMetrics) *RecyclerImpl {
	if metrics == nil {
		metrics = NewRecyclerMetrics(nil)
	}
	r := &RecyclerImpl{ //nolint:exhaustruct
		eventPool:      eventPool,
		execReportPool: execReportPool,
		metrics:        metrics,
	}
	return r
}

// RecycleEvent resets an event, applies optional debug instrumentation, and returns it to the pool.
func (r *RecyclerImpl) RecycleEvent(ev *events.Event) {
	if ev == nil || r.eventPool == nil {
		return
	}
	debugMode := r.debugEnabled.Load()
	var ptr unsafe.Pointer
	if debugMode {
		ptr = unsafe.Pointer(ev) //nolint:gosec
		r.guardDoublePut(ptr)
	}
	kind := ev.Kind
	started := time.Now()
	ev.Reset()
	if debugMode {
		poisonEventMemory(ptr)
	}
	r.eventPool.Put(ev)
	r.metrics.observeRecycle(kind, started)
}

// RecycleExecReport resets an execution report and returns it to the exec report pool.
func (r *RecyclerImpl) RecycleExecReport(er *events.ExecReport) {
	if er == nil || r.execReportPool == nil {
		return
	}
	debugMode := r.debugEnabled.Load()
	var ptr unsafe.Pointer
	if debugMode {
		ptr = unsafe.Pointer(er) //nolint:gosec
		r.guardDoublePut(ptr)
	}
	started := time.Now()
	er.Reset()
	if debugMode {
		poisonEventMemory(ptr)
	}
	r.execReportPool.Put(er)
	r.metrics.observeRecycle(events.KindExecReport, started)
}

// RecycleMany performs bulk recycling of events, avoiding repeated allocations during partial cleanup.
func (r *RecyclerImpl) RecycleMany(eventsToRecycle []*events.Event) {
	for _, ev := range eventsToRecycle {
		r.RecycleEvent(ev)
	}
}

// EnableDebugMode activates poisoning and double-put tracking.
func (r *RecyclerImpl) EnableDebugMode() {
	r.debugEnabled.Store(true)
}

// DisableDebugMode deactivates poisoning and clears the tracking map.
func (r *RecyclerImpl) DisableDebugMode() {
	r.debugEnabled.Store(false)
	r.putTracker = sync.Map{}
}

// CheckoutEvent marks an event as out-of-pool, clearing debug trackers for subsequent reuse.
func (r *RecyclerImpl) CheckoutEvent(ev *events.Event) {
	if ev == nil {
		return
	}
	if r.debugEnabled.Load() {
		ptr := unsafe.Pointer(ev) //nolint:gosec
		r.releasePointer(ptr)
	}
}

// CheckoutExecReport marks an exec report as checked out from the pool.
func (r *RecyclerImpl) CheckoutExecReport(er *events.ExecReport) {
	if er == nil {
		return
	}
	if r.debugEnabled.Load() {
		ptr := unsafe.Pointer(er) //nolint:gosec
		r.releasePointer(ptr)
	}
}
