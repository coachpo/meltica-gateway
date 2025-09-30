package integration

import (
	"context"
	"math"
	"sort"
	"testing"
	"time"

	json "github.com/goccy/go-json"
	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/config"
	"github.com/coachpo/meltica/internal/adapters/binance"
	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/conductor"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/observability"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

func BenchmarkEndToEndLatency(b *testing.B) {
	if testing.Short() {
		b.Skip("latency benchmark skipped in short mode")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pools := pool.NewPoolManager()
	registerPool := func(name string, capacity int, ctor func() interface{}) {
		require.NoError(b, pools.RegisterPool(name, capacity, ctor))
	}
	registerPool("WsFrame", 512, func() interface{} { return &schema.WsFrame{} })
	registerPool("ProviderRaw", 512, func() interface{} { return &schema.ProviderRaw{} })
	registerPool("CanonicalEvent", 1024, func() interface{} { return &schema.Event{} })
	registerPool("MergedEvent", 128, func() interface{} { return &schema.MergedEvent{} })

	ws := NewFakeWebSocket(b.N + 1024)
	parser := binance.NewParserWithPool("binance", pools)
	wsClient := binance.NewWSClient("binance", &fakeFrameProvider{ws: ws}, parser, time.Now, pools)
	provider := binance.NewProvider("binance", wsClient, nil, binance.ProviderOptions{
		Topics: []string{"btcusdt@aggTrade"},
	})
	require.NoError(b, provider.Start(ctx))

	orch := conductor.NewEventOrchestratorWithPool(pools)
	orch.AddProvider("binance", provider.Events(), provider.Errors())
	orchErrs := make(chan error, 8)
	go func() {
		err := orch.Start(ctx)
		if err != nil && err != context.Canceled {
			orchErrs <- err
		}
		close(orchErrs)
	}()

	bus := databus.NewMemoryBus(databus.MemoryConfig{BufferSize: 2048})
	dispatchCfg := config.DispatcherRuntimeConfig{
		StreamOrdering: config.StreamOrderingConfig{
			LatenessTolerance: 150 * time.Millisecond,
			FlushInterval:     10 * time.Millisecond,
			MaxBufferSize:     2048,
		},
	}
	dispatch := dispatcher.NewRuntime(bus, pools, dispatchCfg, observability.NewRuntimeMetrics())
	dispatchErrs := dispatch.Start(ctx, orch.Events())

	_, tradeCh, err := bus.Subscribe(ctx, schema.EventTypeTrade)
	require.NoError(b, err)

	// Warm-up iterations to stabilise goroutines and pool caches.
	const warmup = 16
	for i := 0; i < warmup; i++ {
		ws.Publish(encodeAggTradeFrame(uint64(i + 1)))
		awaitTradeEvent(b, tradeCh, dispatchErrs, orchErrs)
	}

	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		ws.Publish(encodeAggTradeFrame(uint64(i + 1 + warmup)))
		awaitTradeEvent(b, tradeCh, dispatchErrs, orchErrs)
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()

	if len(durations) == 0 {
		b.Fatalf("no durations recorded")
	}

	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(math.Ceil(float64(len(sorted))*0.99)) - 1
	if idx < 0 {
		idx = 0
	} else if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	p99 := sorted[idx]

	limit := 150 * time.Millisecond
	if p99 > limit {
		b.Fatalf("p99 latency %s exceeds target %s", p99, limit)
	}

	b.ReportMetric(float64(p99)/float64(time.Millisecond), "p99_ms")
}

func awaitTradeEvent(b *testing.B, tradeCh <-chan *schema.Event, dispatchErrs <-chan error, orchErrs <-chan error) *schema.Event {
	b.Helper()
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()

	for {
		select {
		case evt, ok := <-tradeCh:
			if !ok {
				b.Fatalf("trade channel closed")
			}
			if evt != nil {
				return evt
			}
		case err, ok := <-dispatchErrs:
			if ok && err != nil {
				b.Fatalf("dispatcher error: %v", err)
			}
		case err, ok := <-orchErrs:
			if ok && err != nil {
				b.Fatalf("orchestrator error: %v", err)
			}
		case <-timer.C:
			b.Fatalf("timeout waiting for trade event")
		}
	}
}

func encodeAggTradeFrame(seq uint64) []byte {
	payload := map[string]any{
		"stream": "btcusdt@aggTrade",
		"data": map[string]any{
			"e": "aggTrade",
			"E": time.Now().UnixMilli(),
			"s": "BTCUSDT",
			"t": seq,
			"p": "68000.50",
			"q": "0.10",
			"m": false,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return data
}
