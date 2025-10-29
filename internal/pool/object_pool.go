package pool

import (
	"context"
	"errors"
	"fmt"
	"log"
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
//
// activeLeases tracks the number of objects currently borrowed (delivered to callers
// but not yet returned), enabling real-time availability and utilization metrics.
type objectPool struct {
	name         string
	objectType   string
	factory      func() PooledObject
	requests     chan *poolRequest
	queueSize    int
	stop         chan struct{}
	leases       sync.Map // map[uintptr]*lease
	workers      *concpool.Pool
	closed       atomic.Bool
	capacity     int
	activeLeases atomic.Int64
}

type poolRequest struct {
	ctx    context.Context
	result chan poolResult
}

type poolResult struct {
	obj PooledObject
	err error
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
		result: make(chan poolResult, 1),
	}
}

func newObjectPool(name string, objectType string, capacity int, queueSize int, factory func() PooledObject) (*objectPool, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("pool %s: capacity must be positive", name)
	}
	if factory == nil {
		return nil, fmt.Errorf("pool %s: factory required", name)
	}
	if queueSize <= 0 {
		queueSize = capacity
	}
	//nolint:exhaustruct // zero values for leases and closed are intentional
	op := &objectPool{
		name:       name,
		objectType: objectType,
		factory:    factory,
		requests:   make(chan *poolRequest, queueSize),
		queueSize:  queueSize,
		stop:       make(chan struct{}),
		capacity:   capacity,
		workers:    concpool.New().WithMaxGoroutines(capacity),
	}
	for i := 0; i < capacity; i++ {
		op.workers.Go(op.worker)
	}
	return op, nil
}

func (op *objectPool) worker() {
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
		case req.result <- poolResult{obj: obj, err: nil}:
			obj.Reset()
			obj.SetReturned(false)
			op.activeLeases.Add(1)
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
			op.activeLeases.Add(-1)
			return returned, true
		}
	}
}

func (op *objectPool) get(ctx context.Context) (PooledObject, error) {
	if op.closed.Load() {
		return nil, errPoolClosed
	}

	req := newPoolRequest(ctx)
	if err := op.enqueueRequest(req); err != nil {
		return nil, err
	}

	select {
	case <-op.stop:
		return nil, errPoolClosed
	case res, ok := <-req.result:
		if !ok {
			return nil, fmt.Errorf("pool %s: request closed", op.name)
		}
		if res.err != nil {
			return nil, res.err
		}
		if res.obj == nil {
			return nil, fmt.Errorf("pool %s: request delivered nil object", op.name)
		}
		return res.obj, nil
	case <-req.ctx.Done():
		return nil, fmt.Errorf("result wait context cancelled: %w", req.ctx.Err())
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
	case res, ok := <-req.result:
		if !ok {
			return nil, false, fmt.Errorf("pool %s: request closed", op.name)
		}
		if res.err != nil {
			return nil, false, res.err
		}
		if res.obj == nil {
			return nil, false, fmt.Errorf("pool %s: request delivered nil object", op.name)
		}
		return res.obj, true, nil
	}
}

func (op *objectPool) enqueueRequest(req *poolRequest) error {
	for {
		select {
		case <-op.stop:
			return errPoolClosed
		case <-req.ctx.Done():
			return fmt.Errorf("request context cancelled: %w", req.ctx.Err())
		default:
		}

		select {
		case op.requests <- req:
			return nil
		default:
			old := <-op.requests
			if old != nil {
				err := fmt.Errorf("pool %s: request dropped: queue full", op.name)
				old.fail(err)
				log.Printf("pool %s: request queue full (size=%d); dropping oldest borrower", op.name, op.queueSize)
			}
		}
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
}

func (op *objectPool) getCapacity() int {
	return op.capacity
}

func (op *objectPool) getAvailable() int64 {
	active := op.activeLeases.Load()
	available := int64(op.capacity) - active
	if available < 0 {
		available = 0
	}
	return available
}

func (op *objectPool) getObjectType() string {
	return op.objectType
}

func (req *poolRequest) fail(err error) {
	if req == nil {
		return
	}
	select {
	case req.result <- poolResult{obj: nil, err: err}:
	default:
	}
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
