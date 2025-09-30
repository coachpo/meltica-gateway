//go:build longrun

package integration

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

func TestPoolManagerNoLeakUnderSustainedLoad(t *testing.T) {
	duration := time.Minute
	if env := os.Getenv("LEAK_TEST_DURATION"); env != "" {
		if parsed, err := time.ParseDuration(env); err == nil {
			duration = parsed
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	manager := pool.NewPoolManager()
	require.NoError(t, manager.RegisterPool("CanonicalEvent", 512, func() interface{} {
		return &schema.Event{}
	}))
	require.NoError(t, manager.RegisterPool("WsFrame", 512, func() interface{} {
		return &schema.WsFrame{}
	}))
	require.NoError(t, manager.RegisterPool("ProviderRaw", 256, func() interface{} {
		return &schema.ProviderRaw{}
	}))

	errCh := make(chan error, 1)
	var workers sync.WaitGroup
	runWorker := func(poolName string, acquire func(pool.PooledObject) pool.PooledObject) {
		defer workers.Done()
		workerCtx := ctx
		for {
			select {
			case <-workerCtx.Done():
				return
			default:
			}
			obj, err := manager.Get(workerCtx, poolName)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					continue
				}
				select {
				case errCh <- err:
				default:
				}
				return
			}
			po := acquire(obj)
			time.Sleep(2 * time.Millisecond)
			manager.Put(poolName, po)
		}
	}

	workers.Add(3)
	go runWorker("CanonicalEvent", func(obj pool.PooledObject) pool.PooledObject {
		if evt, ok := obj.(*schema.Event); ok {
			evt.EventID = "longrun"
			evt.Provider = "binance"
		}
		return obj
	})
	go runWorker("WsFrame", func(obj pool.PooledObject) pool.PooledObject {
		if frame, ok := obj.(*schema.WsFrame); ok {
			frame.Provider = "binance"
			frame.MessageType = 1
		}
		return obj
	})
	go runWorker("ProviderRaw", func(obj pool.PooledObject) pool.PooledObject {
		if raw, ok := obj.(*schema.ProviderRaw); ok {
			raw.Provider = "binance"
			raw.StreamName = "btcusdt@aggTrade"
		}
		return obj
	})

	select {
	case <-ctx.Done():
	case err := <-errCh:
		cancel()
		t.Fatalf("pool worker error: %v", err)
	}

	workers.Wait()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	require.NoError(t, manager.Shutdown(shutdownCtx))
}
