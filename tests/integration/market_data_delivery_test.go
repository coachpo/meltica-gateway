package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/config"
	"github.com/coachpo/meltica/internal/adapters/binance"
	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/conductor"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/observability"
	"github.com/coachpo/meltica/internal/schema"
)

func TestMarketDataEndToEndDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ws := NewFakeWebSocket(8)
	rest := NewFakeREST(0)

	rest.QueueResponse(map[string]any{
		"lastUpdateId": 100,
		"bids":         [][]string{{"50000.00", "1.0"}},
		"asks":         [][]string{{"50005.00", "0.8"}},
		"s":            "BTCUSDT",
	})

	parser := binance.NewParser()
	clock := time.Now
	wsClient := binance.NewWSClient("binance", &fakeFrameProvider{ws: ws}, parser, clock, nil)
	restClient := binance.NewRESTClient(&fakeSnapshotFetcher{rest: rest}, parser, clock)

	provider := binance.NewProvider("binance", wsClient, restClient, binance.ProviderOptions{
		Topics: []string{"btcusdt@depth@100ms", "btcusdt@aggTrade"},
		Snapshots: []binance.RESTPoller{{
			Name:     "orderbook",
			Endpoint: "/api/v3/depth",
			Interval: 10 * time.Millisecond,
			Parser:   "orderbook",
		}},
	})

	go func() {
		_ = provider.Start(ctx)
	}()

	bus := databus.NewMemoryBus(databus.MemoryConfig{BufferSize: 16})
	metrics := observability.NewRuntimeMetrics()
	dispatcherCfg := config.DispatcherRuntimeConfig{
		StreamOrdering: config.StreamOrderingConfig{
			LatenessTolerance: 150 * time.Millisecond,
			FlushInterval:     50 * time.Millisecond,
			MaxBufferSize:     16,
		},
	}
	dispatch := dispatcher.NewRuntime(bus, nil, dispatcherCfg, metrics)

	orch := conductor.NewEventOrchestrator()
	orch.AddProvider("binance", provider.Events(), provider.Errors())
	go orch.Start(ctx)

	dispatchErrs := dispatch.Start(ctx, orch.Events())

	_, snapshotCh, err := bus.Subscribe(ctx, schema.EventTypeBookSnapshot)
	require.NoError(t, err)
	_, updateCh, err := bus.Subscribe(ctx, schema.EventTypeBookUpdate)
	require.NoError(t, err)
	_, tradeCh, err := bus.Subscribe(ctx, schema.EventTypeTrade)
	require.NoError(t, err)

	ws.Publish(encodeJSON(depthUpdateFrame(200)))
	ws.Publish(encodeJSON(aggTradeFrame(300)))

	require.NotNil(t, awaitEvent(t, snapshotCh, dispatchErrs))
	require.NotNil(t, awaitEvent(t, updateCh, dispatchErrs))
	require.NotNil(t, awaitEvent(t, tradeCh, dispatchErrs))
}

func awaitEvent(t *testing.T, ch <-chan *schema.Event, errs <-chan error) *schema.Event {
	t.Helper()
	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatalf("channel closed before event")
		}
		return evt
	case err := <-errs:
		t.Fatalf("dispatcher error: %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for event")
	}
	return nil
}

type fakeFrameProvider struct {
	ws *FakeWebSocket
}

func (f *fakeFrameProvider) Subscribe(ctx context.Context, topics []string) (<-chan []byte, <-chan error, error) {
	frames, errs := f.ws.Stream(ctx)
	return frames, errs, nil
}

type fakeSnapshotFetcher struct {
	rest *FakeREST
}

func (f *fakeSnapshotFetcher) Fetch(ctx context.Context, endpoint string) ([]byte, error) {
	select {
	case payload, ok := <-f.rest.Poll(ctx):
		if !ok {
			return nil, context.Canceled
		}
		return encodeJSON(payload), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func depthUpdateFrame(seq uint64) map[string]any {
	return map[string]any{
		"stream": "btcusdt@depth@100ms",
		"data": map[string]any{
			"e": "depthUpdate",
			"E": time.Now().UnixMilli(),
			"s": "BTCUSDT",
			"u": seq,
			"b": [][]string{{"50001.00", "1.2"}},
			"a": [][]string{{"50005.00", "0.5"}},
		},
	}
}

func aggTradeFrame(tradeID uint64) map[string]any {
	return map[string]any{
		"stream": "btcusdt@aggTrade",
		"data": map[string]any{
			"e": "aggTrade",
			"E": time.Now().UnixMilli(),
			"s": "BTCUSDT",
			"t": tradeID,
			"p": "50002.00",
			"q": "0.25",
			"m": false,
		},
	}
}

func encodeJSON(payload any) []byte {
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return data
}
