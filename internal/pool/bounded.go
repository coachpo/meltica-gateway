// Package pool contains bounded object pooling primitives and helpers.
package pool

import (
	"context"
	"fmt"
	"sync"
)

// BoundedPool wraps sync.Pool with bounded capacity, timeout semantics, and
// optional debug instrumentation to track pooled object ownership.
type BoundedPool struct {
	name  string
	sem   chan struct{}
	pool  sync.Pool
	debug *debugState
}

// NewBoundedPool constructs a bounded pool with the provided capacity and
// constructor. Capacity must be positive and newFunc must return objects that
// satisfy the PooledObject interface.
func NewBoundedPool(name string, capacity int, newFunc func() interface{}) *BoundedPool {
	if name == "" {
		panic("pool name must be non-empty")
	}
	if capacity <= 0 {
		panic(fmt.Sprintf("pool %s: capacity must be positive", name))
	}
	if newFunc == nil {
		panic(fmt.Sprintf("pool %s: newFunc must be provided", name))
	}

	bp := new(BoundedPool)
	bp.name = name
	bp.sem = make(chan struct{}, capacity)
	bp.debug = newDebugState(name)

	for i := 0; i < capacity; i++ {
		bp.sem <- struct{}{}
	}

	bp.pool.New = func() interface{} {
		obj := newFunc()
		po, ok := obj.(PooledObject)
		if !ok {
			panic(fmt.Sprintf("pool %s: object does not implement PooledObject: %T", name, obj))
		}
		return po
	}

	return bp
}

// Get acquires an object from the pool, blocking until one is available or the
// provided context is done. When ctx is nil, a background context is used.
func (p *BoundedPool) Get(ctx context.Context) (PooledObject, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("pool %s: %w", p.name, ctx.Err())
	case <-p.sem:
	}

	obj := p.pool.Get()
	if obj == nil {
		obj = p.pool.New()
	}

	po, ok := obj.(PooledObject)
	if !ok {
		panic(fmt.Sprintf("pool %s: retrieved object does not implement PooledObject: %T", p.name, obj))
	}

	p.debug.clear(po)
	markAcquired(po)
	p.debug.recordAcquire(po)
	return po, nil
}

// Put returns the pooled object after Reset() and double-Put detection. It
// panics if the object is nil or was already returned.
func (p *BoundedPool) Put(obj PooledObject) {
	if obj == nil {
		panic(fmt.Sprintf("pool %s: cannot put nil object", p.name))
	}

	ensureReturnable(obj, p.name)
	obj.Reset()
	p.debug.poison(obj)
	markReturned(obj)
	p.debug.recordRelease(obj)
	p.pool.Put(obj)
	p.release()
}

func (p *BoundedPool) release() {
	select {
	case p.sem <- struct{}{}:
	default:
		panic(fmt.Sprintf("pool %s: release called with full semaphore", p.name))
	}
}

func (p *BoundedPool) activeStacks() []string {
	if p == nil {
		return nil
	}
	return p.debug.activeStacks()
}
