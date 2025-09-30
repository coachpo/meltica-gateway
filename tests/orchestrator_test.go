package tests

import (
	"context"
	"sync"
	"testing"

	"github.com/coachpo/meltica/core/events"
	"github.com/coachpo/meltica/core/orchestrator"
)

type stubRecycler struct {
	recycled [][]*events.Event
}

func (s *stubRecycler) RecycleEvent(*events.Event)             {}
func (s *stubRecycler) RecycleMergedEvent(*events.MergedEvent) {}
func (s *stubRecycler) RecycleMany(evts []*events.Event) {
	copySlice := make([]*events.Event, len(evts))
	copy(copySlice, evts)
	s.recycled = append(s.recycled, copySlice)
}
func (s *stubRecycler) EnableDebugMode()                        {}
func (s *stubRecycler) DisableDebugMode()                       {}
func (s *stubRecycler) CheckoutEvent(*events.Event)             {}
func (s *stubRecycler) CheckoutMergedEvent(*events.MergedEvent) {}
func (s *stubRecycler) RecycleExecReport(*events.ExecReport)    {}
func (s *stubRecycler) CheckoutExecReport(*events.ExecReport)   {}

func TestMergerRecyclesPartialsAfterMerge(t *testing.T) {
	recycler := &stubRecycler{}
	pool := &sync.Pool{New: func() any { return &events.MergedEvent{} }}
	merger := orchestrator.NewMerger(pool, recycler)

	partials := []*events.Event{
		{ProviderID: "binance", TraceID: "trace-1", Kind: events.KindExecReport, RoutingVersion: 2},
		{ProviderID: "coinbase", TraceID: "trace-2", Kind: events.KindExecReport, RoutingVersion: 2},
	}

	merged, err := merger.MergeEvents(context.Background(), partials)
	if err != nil {
		t.Fatalf("merge events returned error: %v", err)
	}
	if merged == nil {
		t.Fatalf("expected merged event")
	}
	if len(recycler.recycled) != 1 {
		t.Fatalf("expected recycler to record one batch, got %d", len(recycler.recycled))
	}
	if got := len(recycler.recycled[0]); got != 2 {
		t.Fatalf("expected 2 recycled partials, got %d", got)
	}
	if len(merged.SourceProviders) != 2 {
		t.Fatalf("expected merged providers to include both sources")
	}
}

func TestStamperUsesAtomicVersion(t *testing.T) {
	stamper := orchestrator.NewStamper(10)
	if version := stamper.CurrentVersion(); version != 10 {
		t.Fatalf("expected initial version 10, got %d", version)
	}

	stamper.UpdateVersion(42)
	ev := &events.Event{}
	stamper.StampRoutingVersion(ev)
	if ev.RoutingVersion != 42 {
		t.Fatalf("expected routing version 42, got %d", ev.RoutingVersion)
	}

	stamper.UpdateVersion(100)
	if version := stamper.CurrentVersion(); version != 100 {
		t.Fatalf("expected updated version 100, got %d", version)
	}
}
