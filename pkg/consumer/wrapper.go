package consumer

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/pkg/events"
	"github.com/coachpo/meltica/pkg/recycler"
)

// ConsumerFunc represents the lambda executed for each delivered event.
type ConsumerFunc func(context.Context, *events.Event) error //nolint:revive

// ConsumerWrapper defines lifecycle hooks for invoking consumer lambdas with automatic recycling.
type ConsumerWrapper interface { //nolint:revive
	Invoke(ctx context.Context, ev *events.Event, lambda ConsumerFunc) (err error)
	UpdateMinVersion(version uint64)
	ShouldProcess(ev *events.Event) bool
	Metrics() *ConsumerMetrics
}

// Wrapper implements ConsumerWrapper with defer-based recycling, panic recovery, and routing version filtering.
type Wrapper struct {
	consumerID       string
	recycler         recycler.Recycler
	minAcceptVersion atomic.Uint64
	metrics          *ConsumerMetrics
}

// NewWrapper constructs a Consumer wrapper for the provided consumer identifier.
func NewWrapper(consumerID string, rec recycler.Recycler, metrics *ConsumerMetrics) *Wrapper {
	return &Wrapper{ //nolint:exhaustruct
		consumerID: consumerID,
		recycler:   rec,
		metrics:    metrics,
	}
}

// Invoke executes the consumer lambda with automatic recycling and panic recovery.
func (w *Wrapper) Invoke(ctx context.Context, ev *events.Event, lambda ConsumerFunc) (err error) {
	if ev == nil {
		return nil
	}
	if w.metrics != nil {
		w.metrics.ObserveInvocation(w.consumerID)
	}
	defer func() {
		if r := recover(); r != nil {
			if w.metrics != nil {
				w.metrics.ObservePanic(w.consumerID)
			}
			err = fmt.Errorf("consumer panic: %v\n%s", r, debug.Stack())
		}
		if w.recycler != nil {
			w.recycler.RecycleEvent(ev)
		}
	}()
	if lambda == nil {
		return nil
	}
	if !w.ShouldProcess(ev) {
		if w.metrics != nil {
			w.metrics.ObserveFiltered(w.consumerID)
		}
		return nil
	}
	start := time.Now()
	err = lambda(ctx, ev)
	if w.metrics != nil {
		w.metrics.ObserveDuration(w.consumerID, time.Since(start))
	}
	return err
}

// UpdateMinVersion stores the minimum acceptable routing version for processing events.
func (w *Wrapper) UpdateMinVersion(version uint64) {
	w.minAcceptVersion.Store(version)
}

// ShouldProcess determines whether the event should be processed based on routing version and criticality.
func (w *Wrapper) ShouldProcess(ev *events.Event) bool {
	if ev == nil {
		return false
	}
	if ev.Kind.IsCritical() {
		return true
	}
	minVersion := w.minAcceptVersion.Load()
	return ev.RoutingVersion >= minVersion
}

// Metrics exposes the metrics instrument associated with the wrapper.
func (w *Wrapper) Metrics() *ConsumerMetrics {
	return w.metrics
}
