package integration

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

var poolingEventSink *schema.Event

func TestPoolingEndToEndBalancedGetPut(t *testing.T) {
	manager := pool.NewPoolManager()
	require.NoError(t, manager.RegisterPool("CanonicalEvent", 256, func() interface{} {
		return &schema.Event{}
	}))

	const total = 256
	acquired := make(chan *schema.Event, 64)
	clones := make(chan *schema.Event, total)

	go func() {
		defer close(acquired)
		for i := 0; i < total; i++ {
			obj, err := manager.Get(context.Background(), "CanonicalEvent")
			if err != nil {
				panic(fmt.Errorf("get canonical event: %w", err))
			}
			evt := obj.(*schema.Event)
			evt.EventID = fmt.Sprintf("evt-%d", i)
			evt.Provider = "binance"
			evt.Symbol = "BTC-USDT"
			evt.Type = schema.EventTypeTrade
			evt.SeqProvider = uint64(i + 1)
			evt.IngestTS = time.UnixMilli(int64(i))
			evt.EmitTS = evt.IngestTS
			acquired <- evt
		}
	}()

	go func() {
		defer close(clones)
		for evt := range acquired {
			clone := schema.CloneEvent(evt)
			clones <- clone
			manager.Put("CanonicalEvent", evt)
		}
	}()

	received := 0
	for clone := range clones {
		require.NotNil(t, clone)
		require.NotEmpty(t, clone.EventID)
		received++
	}
	require.Equal(t, total, received)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	require.NoError(t, manager.Shutdown(shutdownCtx))
}

func TestPoolingNoDoublePutUnderLoad(t *testing.T) {
	manager := pool.NewPoolManager()
	require.NoError(t, manager.RegisterPool("CanonicalEvent", 128, func() interface{} {
		return &schema.Event{}
	}))

	const total = 512
	ingest := make(chan *schema.Event, 64)
	fanOut := make(chan *schema.Event, 64)

	go func() {
		defer close(ingest)
		for i := 0; i < total; i++ {
			obj, err := manager.Get(context.Background(), "CanonicalEvent")
			if err != nil {
				panic(fmt.Errorf("get canonical event: %w", err))
			}
			evt := obj.(*schema.Event)
			evt.EventID = fmt.Sprintf("evt-%d", i%8)
			evt.Provider = "binance"
			evt.Symbol = "BTC-USDT"
			evt.Type = schema.EventTypeTrade
			evt.SeqProvider = uint64(i + 1)
			ingest <- evt
		}
	}()

	go func() {
		seen := make(map[string]struct{})
		for evt := range ingest {
			if _, exists := seen[evt.EventID]; exists {
				manager.Put("CanonicalEvent", evt)
				continue
			}
			seen[evt.EventID] = struct{}{}
			fanOut <- evt
		}
		close(fanOut)
	}()

	require.NotPanics(t, func() {
		for evt := range fanOut {
			clone := schema.CloneEvent(evt)
			require.NotNil(t, clone)
			manager.Put("CanonicalEvent", evt)
		}
	})

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	require.NoError(t, manager.Shutdown(shutdownCtx))
}

func TestPoolingReducesAllocationsByFortyPercent(t *testing.T) {
	manager := pool.NewPoolManager()
	require.NoError(t, manager.RegisterPool("CanonicalEvent", 1, func() interface{} {
		return &schema.Event{}
	}))

	baseline := testing.AllocsPerRun(1024, func() {
		poolingEventSink = &schema.Event{
			EventID:  "baseline",
			Provider: "binance",
			Symbol:   "BTC-USDT",
			Type:     schema.EventTypeTrade,
		}
		runtime.KeepAlive(poolingEventSink)
		poolingEventSink = nil
	})

	pooled := testing.AllocsPerRun(1024, func() {
		obj, err := manager.Get(context.Background(), "CanonicalEvent")
		if err != nil {
			panic(fmt.Errorf("get pooled event: %w", err))
		}
		evt := obj.(*schema.Event)
		evt.EventID = "pooled"
		evt.Provider = "binance"
		evt.Symbol = "BTC-USDT"
		evt.Type = schema.EventTypeTrade
		poolingEventSink = evt
		manager.Put("CanonicalEvent", evt)
		runtime.KeepAlive(poolingEventSink)
		poolingEventSink = nil
	})

	require.Greater(t, baseline, float64(0))
	reduction := 1 - (pooled / baseline)
	require.GreaterOrEqual(t, reduction, 0.40, "expected at least 40%% allocation reduction, baseline=%.2f pooled=%.2f", baseline, pooled)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	require.NoError(t, manager.Shutdown(shutdownCtx))
}
