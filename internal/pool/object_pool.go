package pool

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	concpool "github.com/sourcegraph/conc/pool"
)

var (
	errPoolClosed = errors.New("pool: closed")
)

// objectPool manages a bounded set of reusable objects by handing each request
// off to a long-lived worker goroutine. Each worker owns exactly one object at
// a time, ensuring the pool never lends out more than its capacity.
type objectPool struct {
	name      string
	factory   func() PooledObject
	requests  chan *poolRequest
	stop      chan struct{}
	leases    sync.Map // map[uintptr]*lease
	workers   *concpool.Pool
	closed    atomic.Bool
	capacity  int
	waitGroup sync.WaitGroup
}

type poolRequest struct {
	ctx    context.Context
	result chan PooledObject
}

type lease struct {
	obj      PooledObject
	returnCh chan PooledObject
}

func newPoolRequest(ctx context.Context) *poolRequest {
	if ctx == nil {
		ctx = context.Background()
	}
	return &poolRequest{
		ctx:    ctx,
		result: make(chan PooledObject, 1),
	}
}

func newObjectPool(name string, capacity int, factory func() PooledObject) (*objectPool, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("pool %s: capacity must be positive", name)
	}
	if factory == nil {
		return nil, fmt.Errorf("pool %s: factory required", name)
	}
	op := &objectPool{
		name:     name,
		factory:  factory,
		requests: make(chan *poolRequest),
		stop:     make(chan struct{}),
		capacity: capacity,
		workers:  concpool.New().WithMaxGoroutines(capacity),
	}
	for i := 0; i < capacity; i++ {
		op.waitGroup.Add(1)
		op.workers.Go(op.worker)
	}
	return op, nil
}

func (op *objectPool) worker() {
	defer op.waitGroup.Done()

	obj := op.factory()
	if obj == nil {
		panic(fmt.Sprintf("pool %s: factory returned nil object", op.name))
	}
	obj.Reset()
	obj.SetReturned(true)

	for {
		req, ok := op.nextRequest()
		if !ok {
			return
		}
		lease := op.checkout(obj)
		if lease == nil {
			continue
		}
		if !op.deliver(req, obj) {
			op.cancelLease(lease)
			obj.SetReturned(true)
			continue
		}
		ret, ok := op.waitForReturn(lease)
		if !ok {
			return
		}
		obj = ret
		obj.Reset()
		obj.SetReturned(true)
	}
}

func (op *objectPool) nextRequest() (*poolRequest, bool) {
	select {
	case <-op.stop:
		return nil, false
	case req, ok := <-op.requests:
		if !ok {
			return nil, false
		}
		return req, true
	}
}

func (op *objectPool) deliver(req *poolRequest, obj PooledObject) bool {
	if req == nil {
		return false
	}
	for {
		select {
		case <-op.stop:
			return false
		case <-req.ctx.Done():
			return false
		case req.result <- obj:
			obj.SetReturned(false)
			return true
		}
	}
}

func (op *objectPool) checkout(obj PooledObject) *lease {
	l := &lease{
		obj:      obj,
		returnCh: make(chan PooledObject, 1),
	}
	op.leases.Store(pointerKey(obj), l)
	return l
}

func (op *objectPool) cancelLease(l *lease) {
	if l == nil {
		return
	}
	op.leases.Delete(pointerKey(l.obj))
	close(l.returnCh)
}

func (op *objectPool) waitForReturn(l *lease) (PooledObject, bool) {
	for {
		select {
		case <-op.stop:
			// Continue waiting to avoid leaking the object. Shutdown waits for
			// all callers to return objects.
		case returned, ok := <-l.returnCh:
			op.leases.Delete(pointerKey(l.obj))
			if !ok {
				return nil, false
			}
			return returned, true
		}
	}
}

func (op *objectPool) get(ctx context.Context) (PooledObject, error) {
	if op.closed.Load() {
		return nil, errPoolClosed
	}

	req := newPoolRequest(ctx)
	select {
	case <-op.stop:
		return nil, errPoolClosed
	case op.requests <- req:
	case <-req.ctx.Done():
		return nil, req.ctx.Err()
	}

	select {
	case <-op.stop:
		return nil, errPoolClosed
	case obj := <-req.result:
		return obj, nil
	case <-req.ctx.Done():
		return nil, req.ctx.Err()
	}
}

func (op *objectPool) tryGet() (PooledObject, bool, error) {
	if op.closed.Load() {
		return nil, false, errPoolClosed
	}

	req := newPoolRequest(context.Background())

	select {
	case <-op.stop:
		return nil, false, errPoolClosed
	case op.requests <- req:
	default:
		return nil, false, nil
	}

	select {
	case <-op.stop:
		return nil, false, errPoolClosed
	case obj := <-req.result:
		return obj, true, nil
	}
}

func (op *objectPool) put(obj PooledObject) error {
	if obj == nil {
		return fmt.Errorf("pool %s: nil object returned", op.name)
	}
	key := pointerKey(obj)
	value, ok := op.leases.Load(key)
	if !ok {
		return fmt.Errorf("pool %s: double put detected for %T", op.name, obj)
	}
	l, ok := value.(*lease)
	if !ok {
		op.leases.Delete(key)
		return fmt.Errorf("pool %s: invalid lease type %T", op.name, value)
	}
	obj.Reset()
	obj.SetReturned(true)
	select {
	case l.returnCh <- obj:
		return nil
	default:
		op.leases.Delete(key)
		return fmt.Errorf("pool %s: unexpected lease state for %T", op.name, obj)
	}
}

func (op *objectPool) tryPut(obj PooledObject) (bool, error) {
	if obj == nil {
		return false, fmt.Errorf("pool %s: nil object returned", op.name)
	}
	key := pointerKey(obj)
	value, ok := op.leases.Load(key)
	if !ok {
		return false, fmt.Errorf("pool %s: double put detected for %T", op.name, obj)
	}
	l, ok := value.(*lease)
	if !ok {
		op.leases.Delete(key)
		return false, fmt.Errorf("pool %s: invalid lease type %T", op.name, value)
	}
	obj.Reset()
	obj.SetReturned(true)
	select {
	case l.returnCh <- obj:
		return true, nil
	default:
		op.leases.Delete(key)
		return false, fmt.Errorf("pool %s: unexpected lease state for %T", op.name, obj)
	}
}

func (op *objectPool) close() {
	if op.closed.Swap(true) {
		return
	}
	close(op.stop)
	op.leases.Range(func(_, value any) bool {
		if l, ok := value.(*lease); ok {
			close(l.returnCh)
		}
		return true
	})
	op.workers.Wait()
	op.waitGroup.Wait()
}

func pointerKey(obj PooledObject) uintptr {
	if obj == nil {
		return 0
	}
	rv := reflect.ValueOf(obj)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		panic(fmt.Sprintf("pool object must be pointer, got %T", obj))
	}
	return rv.Pointer()
}
