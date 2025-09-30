package unit

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/lib/async"
)

func TestAsyncPoolSubmitAndShutdown(t *testing.T) {
	pool, err := async.NewPool(2, 4)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var count atomic.Int32
	for i := 0; i < 4; i++ {
		require.NoError(t, pool.Submit(ctx, func(context.Context) error {
			count.Add(1)
			return nil
		}))
	}

	require.Eventually(t, func() bool { return count.Load() == 4 }, time.Second, 10*time.Millisecond)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	require.NoError(t, pool.Shutdown(shutdownCtx))
	require.Equal(t, int32(4), count.Load())
}

func TestAsyncPoolContextCancellation(t *testing.T) {
	pool, err := async.NewPool(1, 0)
	require.NoError(t, err)
	defer pool.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = pool.Submit(ctx, func(context.Context) error { return nil })
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
}
