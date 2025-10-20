package pool

import (
	"context"
	"fmt"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

const poolAcquireTimeout = 100 * time.Millisecond

// AcquireOrderRequest obtains an OrderRequest from the pool with a 100ms timeout.
func AcquireOrderRequest(ctx context.Context, pools *PoolManager) (*schema.OrderRequest, func(), error) {
	obj, release, err := acquireFromPool(ctx, pools, "OrderRequest")
	if err != nil {
		return nil, nil, err
	}
	request, ok := obj.(*schema.OrderRequest)
	if !ok {
		release()
		return nil, nil, fmt.Errorf("pool OrderRequest: unexpected type %T", obj)
	}
	request.Reset()
	return request, release, nil
}

func acquireFromPool(ctx context.Context, pools *PoolManager, poolName string) (PooledObject, func(), error) {
	if pools == nil {
		if poolName == "OrderRequest" {
			return new(schema.OrderRequest), func() {}, nil
		}
		return nil, func() {}, fmt.Errorf("pool %s not available", poolName)
	}

	var cancel context.CancelFunc
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		ctx, cancel = context.WithTimeout(ctx, poolAcquireTimeout)
	}
	if cancel != nil {
		defer cancel()
	}

	obj, err := pools.Get(ctx, poolName)
	if err != nil {
		return nil, func() {}, fmt.Errorf("pool %s: %w", poolName, err)
	}
	release := func() {
		pools.Put(poolName, obj)
	}
	return obj, release, nil
}
