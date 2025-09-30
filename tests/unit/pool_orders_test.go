package unit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

func TestAcquireOrderRequestFromPool(t *testing.T) {
	manager := pool.NewPoolManager()
	require.NoError(t, manager.RegisterPool("OrderRequest", 1, func() interface{} {
		return &schema.OrderRequest{}
	}))

	req, release, err := pool.AcquireOrderRequest(context.Background(), manager)
	require.NoError(t, err)
	require.NotNil(t, req)
	require.False(t, req.IsReturned())

	req.ClientOrderID = "abc"
	release()

	// After release the pool should accept another acquisition without leak.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	obj, releaseAgain, err := pool.AcquireOrderRequest(ctx, manager)
	require.NoError(t, err)
	require.Equal(t, "", obj.ClientOrderID)
	releaseAgain()
}

func TestAcquireOrderRequestTimeout(t *testing.T) {
	manager := pool.NewPoolManager()
	require.NoError(t, manager.RegisterPool("OrderRequest", 1, func() interface{} {
		return &schema.OrderRequest{}
	}))

	req, release, err := pool.AcquireOrderRequest(context.Background(), manager)
	require.NoError(t, err)
	defer release()
	require.NotNil(t, req)

	ctx, cancel := context.WithTimeout(context.Background(), poolAcquireTestTimeout)
	defer cancel()
	_, rel, err := pool.AcquireOrderRequest(ctx, manager)
	if rel != nil {
		rel()
	}
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestAcquireExecReportFromPool(t *testing.T) {
	manager := pool.NewPoolManager()
	require.NoError(t, manager.RegisterPool("ExecReport", 1, func() interface{} {
		return &schema.ExecReport{}
	}))

	report, release, err := pool.AcquireExecReport(context.Background(), manager)
	require.NoError(t, err)
	require.NotNil(t, report)
	require.False(t, report.IsReturned())

	report.ClientOrderID = "order-1"
	release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	obj, releaseAgain, err := pool.AcquireExecReport(ctx, manager)
	require.NoError(t, err)
	require.Equal(t, "", obj.ClientOrderID)
	releaseAgain()
}

func TestAcquireExecReportTimeout(t *testing.T) {
	manager := pool.NewPoolManager()
	require.NoError(t, manager.RegisterPool("ExecReport", 1, func() interface{} {
		return &schema.ExecReport{}
	}))

	report, release, err := pool.AcquireExecReport(context.Background(), manager)
	require.NoError(t, err)
	defer release()
	require.NotNil(t, report)

	ctx, cancel := context.WithTimeout(context.Background(), poolAcquireTestTimeout)
	defer cancel()
	_, rel, err := pool.AcquireExecReport(ctx, manager)
	if rel != nil {
		rel()
	}
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

const poolAcquireTestTimeout = 120 * time.Millisecond
