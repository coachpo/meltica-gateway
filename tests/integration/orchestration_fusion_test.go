package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/conductor"
	"github.com/coachpo/meltica/internal/schema"
	"github.com/coachpo/meltica/internal/snapshot"
)

func TestOrchestratorFusesSnapshotsAndDeltas(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := snapshot.NewMemoryStore()
	throttle := conductor.NewThrottle(0)
	orchestrator := conductor.NewOrchestrator(store, throttle)

	events := make(chan schema.MelticaEvent, 2)
	fusedCh, errs := orchestrator.Run(ctx, events)

	base := schema.MelticaEvent{
		Type:       schema.CanonicalType("ORDERBOOK.SNAPSHOT"),
		Instrument: "BTC-USDT",
		Market:     "BINANCE-SPOT",
		Source:     "binance.rest.orderbook",
		Ts:         time.Now(),
	}

	snapshotEvt := base
	snapshotEvt.Payload = map[string]any{
		"lastUpdateId": float64(100),
		"topBid":       68000.0,
		"topAsk":       68010.0,
	}

	deltaEvt := base
	deltaEvt.Type = schema.CanonicalType("ORDERBOOK.DELTA")
	deltaEvt.Source = "binance.ws.depth"
	deltaEvt.Payload = map[string]any{
		"side":  "bid",
		"price": 68005.0,
		"qty":   0.8,
	}

	events <- snapshotEvt
	events <- deltaEvt
	close(events)

	var fused []schema.MelticaEvent
	collect := time.After(100 * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("context done: %v", ctx.Err())
		case err, ok := <-errs:
			if ok && err != nil {
				t.Fatalf("orchestrator error: %v", err)
			}
		case evt, ok := <-fusedCh:
			if !ok {
				goto assertions
			}
			fused = append(fused, evt)
		case <-collect:
			goto assertions
		}
	}

assertions:
	require.GreaterOrEqual(t, len(fused), 2)
	fusedEvent := fused[len(fused)-1]
	require.Equal(t, schema.CanonicalType("ORDERBOOK.SNAPSHOT"), fusedEvent.Type)
	require.Equal(t, float64(68005.0), fusedEvent.Payload.(map[string]any)["topBid"])
	require.Equal(t, uint64(2), fusedEvent.Seq)
}
