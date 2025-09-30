package tests

import (
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/coachpo/meltica/core/events"
	"github.com/coachpo/meltica/core/recycler"
)

const poisonSentinel uint64 = 0xDEADBEEFDEADBEEF

func newTestRecycler(t *testing.T) *recycler.RecyclerImpl {
	t.Helper()
	eventPool := &sync.Pool{New: func() any { return &events.Event{} }}
	mergedPool := &sync.Pool{New: func() any { return &events.MergedEvent{} }}
	execPool := &sync.Pool{New: func() any { return &events.ExecReport{} }}
	metrics := recycler.NewRecyclerMetrics(prometheus.NewRegistry())
	return recycler.NewRecycler(eventPool, mergedPool, execPool, metrics)
}

func TestRecycleEventResetsFields(t *testing.T) {
	r := newTestRecycler(t)
	ev := &events.Event{
		TraceID:        "trace",
		RoutingVersion: 7,
		Kind:           events.KindExecReport,
		Payload:        struct{}{},
		IngestTS:       time.Now(),
		SeqProvider:    42,
		ProviderID:     "binance",
	}

	r.RecycleEvent(ev)

	if ev.TraceID != "" || ev.RoutingVersion != 0 || ev.Kind != 0 || ev.Payload != nil || !ev.IngestTS.IsZero() || ev.SeqProvider != 0 || ev.ProviderID != "" {
		t.Fatalf("event fields not reset: %+v", ev)
	}
}

func TestDebugModePoisoning(t *testing.T) {
	r := newTestRecycler(t)
	r.EnableDebugMode()
	ev := &events.Event{}

	r.RecycleEvent(ev)

	word := *(*uint64)(unsafe.Pointer(ev))
	if word != poisonSentinel {
		t.Fatalf("expected poison pattern %x, got %x", poisonSentinel, word)
	}
}

func TestDoublePutGuardPanics(t *testing.T) {
	r := newTestRecycler(t)
	r.EnableDebugMode()
	ev := &events.Event{}

	r.RecycleEvent(ev)

	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic on double recycle")
		}
	}()
	r.RecycleEvent(ev)
}

func TestRecycleMany(t *testing.T) {
	r := newTestRecycler(t)
	first := &events.Event{TraceID: "a", Kind: events.KindMarketData, ProviderID: "one"}
	second := &events.Event{TraceID: "b", Kind: events.KindExecReport, ProviderID: "two"}

	r.RecycleMany([]*events.Event{first, second})

	if first.TraceID != "" || first.ProviderID != "" || first.Kind != 0 {
		t.Fatalf("first event not reset: %+v", first)
	}
	if second.TraceID != "" || second.ProviderID != "" || second.Kind != 0 {
		t.Fatalf("second event not reset: %+v", second)
	}
}
