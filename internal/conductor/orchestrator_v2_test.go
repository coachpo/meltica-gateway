package conductor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

func TestAcquireMergedEventWithoutPool(t *testing.T) {
	orchestrator := NewEventOrchestrator()
	merged, release, err := orchestrator.acquireMergedEvent(context.Background())
	require.NoError(t, err)
	require.NotNil(t, merged)
	require.NotNil(t, release)
}

func TestAcquireMergedEventWithPool(t *testing.T) {
	pm := pool.NewPoolManager()
	require.NoError(t, pm.RegisterPool("MergedEvent", 1, func() interface{} { return new(schema.MergedEvent) }))
	orchestrator := NewEventOrchestratorWithPool(pm)

	merged, release, err := orchestrator.acquireMergedEvent(context.Background())
	require.NoError(t, err)
	require.NotNil(t, release)
	require.False(t, merged.IsReturned())

	release()
	require.True(t, merged.IsReturned())
}

func TestPopulateMergedEventCopiesFields(t *testing.T) {
	orchestrator := NewEventOrchestrator()
	merged := new(schema.MergedEvent)
	mergeID := "merge-1"
	evt := &schema.Event{MergeID: &mergeID, Symbol: "BTC-USDT", Type: schema.EventTypeTrade, IngestTS: time.Unix(0, 10), EmitTS: time.Unix(0, 20), TraceID: "trace"}

	orchestrator.populateMergedEvent(merged, evt)
	require.Equal(t, mergeID, merged.MergeID)
	require.Equal(t, evt.Symbol, merged.Symbol)
	require.Equal(t, evt.Type, merged.EventType)
	require.Equal(t, evt.IngestTS.UnixNano(), merged.WindowOpen)
	require.Equal(t, evt.EmitTS.UnixNano(), merged.WindowClose)
	require.True(t, merged.IsComplete)
	require.Len(t, merged.Fragments, 1)
}

func TestDispatchEventReleasesOnContextCancel(t *testing.T) {
	orchestrator := NewEventOrchestrator()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	released := false
	orchestrator.dispatchEvent(ctx, &schema.Event{}, func() { released = true })
	require.True(t, released)
}

func TestEmitErrorSendsOnce(t *testing.T) {
	orchestrator := NewEventOrchestrator()
	err := errors.New("boom")
	orchestrator.emitError(err)

	select {
	case got := <-orchestrator.Errors():
		require.EqualError(t, got, "boom")
	default:
		t.Fatal("expected error to be emitted")
	}
}

func TestHandleEventEmitsErrorWhenAcquireFails(t *testing.T) {
	pm := pool.NewPoolManager()
	require.NoError(t, pm.RegisterPool("MergedEvent", 1, func() interface{} { return new(schema.MergedEvent) }))
	require.NoError(t, pm.Shutdown(context.Background()))
	orchestrator := NewEventOrchestratorWithPool(pm)

	mergeID := "merge"
	orchestrator.handleEvent(context.Background(), &schema.Event{MergeID: &mergeID})

	select {
	case <-orchestrator.Events():
	default:
	}

	select {
	case err := <-orchestrator.Errors():
		require.Error(t, err)
	default:
		t.Fatal("expected error from handleEvent")
	}
}
