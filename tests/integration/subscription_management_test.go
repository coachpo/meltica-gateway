package integration

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/config"
	"github.com/coachpo/meltica/internal/adapters/binance"
	"github.com/coachpo/meltica/internal/bus/controlbus"
	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/conductor"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/observability"
	"github.com/coachpo/meltica/internal/schema"
)

func TestSubscriptionManagementLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ws := NewFakeWebSocket(8)
	rest := NewFakeREST(0)
	parser := binance.NewParser()

	wsClient := binance.NewWSClient("binance", &fakeFrameProvider{ws: ws}, parser, time.Now, nil)
	restClient := binance.NewRESTClient(&fakeSnapshotFetcher{rest: rest}, parser, time.Now)
	provider := binance.NewProvider("binance", wsClient, restClient, binance.ProviderOptions{})
	require.NoError(t, provider.Start(ctx))

	bus := databus.NewMemoryBus(databus.MemoryConfig{BufferSize: 16})
	defer bus.Close()

	eventOrchestrator := conductor.NewEventOrchestrator()
	eventOrchestrator.AddProvider("binance", provider.Events(), provider.Errors())
	go func() {
		_ = eventOrchestrator.Start(ctx)
	}()

	runtimeCfg := config.DispatcherRuntimeConfig{
		StreamOrdering: config.StreamOrderingConfig{
			LatenessTolerance: 150 * time.Millisecond,
			FlushInterval:     50 * time.Millisecond,
			MaxBufferSize:     64,
		},
		Backpressure: config.BackpressureConfig{
			TokenRatePerStream: 1000,
			TokenBurst:         100,
		},
	}
	dispatch := dispatcher.NewRuntime(bus, nil, runtimeCfg, observability.NewRuntimeMetrics())
	dispatchErrs := dispatch.Start(ctx, eventOrchestrator.Events())
	go drainErrors(t, dispatchErrs)

	controlBus := controlbus.NewMemoryBus(controlbus.MemoryConfig{BufferSize: 8})
	defer controlBus.Close()

	table := dispatcher.NewTable()
	route := dispatcher.Route{
		Type:     schema.CanonicalType("TRADE"),
		WSTopics: []string{"btcusdt@aggTrade"},
	}
	require.NoError(t, table.Upsert(route))

	subManager := binance.NewSubscriptionManager(provider)
	controller := dispatcher.NewController(table, controlBus, subManager)
	go func() {
		_ = controller.Start(ctx)
	}()

	handler := dispatcher.NewControlHTTPHandler(controlBus)
	server := httptest.NewServer(handler)
	defer server.Close()

	_, trades, err := bus.Subscribe(ctx, schema.EventTypeTrade)
	require.NoError(t, err)

	subAck := postCommand(t, server.URL+"/control/subscribe", schema.Subscribe{Type: route.Type})
	require.True(t, subAck.Success)

	ws.Publish(encodeJSON(aggTradeFrame(101)))
	select {
	case evt := <-trades:
		require.NotNil(t, evt)
		require.Equal(t, uint64(101), evt.SeqProvider)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected trade event after subscribe")
	}

	unsubAck := postCommand(t, server.URL+"/control/unsubscribe", schema.Unsubscribe{Type: route.Type})
	require.True(t, unsubAck.Success)

	// Allow unsubscribe to propagate before publishing another frame.
	time.Sleep(100 * time.Millisecond)
	ws.Publish(encodeJSON(aggTradeFrame(202)))

	select {
	case evt := <-trades:
		t.Fatalf("unexpected event after unsubscribe: %#v", evt)
	case <-time.After(250 * time.Millisecond):
		// success – no events received
	}
}

func drainErrors(t *testing.T, errs <-chan error) {
	for err := range errs {
		if err != nil {
			t.Logf("dispatcher error: %v", err)
		}
	}
}
