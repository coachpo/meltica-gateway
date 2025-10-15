package pool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

var (
	// ErrPoolNotRegistered indicates the requested pool has not been registered.
	ErrPoolNotRegistered = errors.New("pool manager: pool not registered")
	// ErrPoolManagerClosed indicates the manager is shutting down and cannot service requests.
	ErrPoolManagerClosed = errors.New("pool manager: shutdown in progress")
)

// PoolManager coordinates named bounded pools, providing lifecycle management,
// active-object tracking, and graceful shutdown semantics for pooled resources.
//
//nolint:revive // PoolManager matches specification terminology.
type PoolManager struct {
	mu           sync.RWMutex
	pools        map[string]*objectPool
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
	inFlight     sync.WaitGroup
	activeCount  atomic.Int64
}

// NewPoolManager constructs an initialized pool manager ready for pool registration.
func NewPoolManager() *PoolManager {
	pm := new(PoolManager)
	pm.pools = make(map[string]*objectPool)
	pm.shutdownCh = make(chan struct{})
	return pm
}

// RegisterPool registers a bounded pool with the provided name, capacity, and constructor.
func (pm *PoolManager) RegisterPool(name string, capacity int, newFunc func() any) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	select {
	case <-pm.shutdownCh:
		return ErrPoolManagerClosed
	default:
	}

	if _, exists := pm.pools[name]; exists {
		return fmt.Errorf("pool manager: pool %s already registered", name)
	}

	factory := func() PooledObject {
		obj := newFunc()
		po, ok := obj.(PooledObject)
		if !ok {
			panic(fmt.Sprintf("pool manager: object does not implement PooledObject: %T", obj))
		}
		return po
	}
	pool, err := newObjectPool(name, capacity, factory)
	if err != nil {
		return err
	}
	pm.pools[name] = pool
	return nil
}

// Get acquires an object from the named pool respecting manager shutdown state.
func (pm *PoolManager) Get(ctx context.Context, poolName string) (PooledObject, error) {
	select {
	case <-pm.shutdownCh:
		return nil, ErrPoolManagerClosed
	default:
	}

	pool, err := pm.lookup(poolName)
	if err != nil {
		return nil, err
	}

	obj, err := pool.get(ctx)
	if err != nil {
		return nil, fmt.Errorf("pool manager: get %s: %w", poolName, err)
	}

	pm.inFlight.Add(1)
	pm.activeCount.Add(1)
	return obj, nil
}

// GetMany acquires multiple objects from the named pool.
func (pm *PoolManager) GetMany(ctx context.Context, poolName string, count int) ([]PooledObject, error) {
	if count <= 0 {
		return nil, nil
	}

	objects := make([]PooledObject, 0, count)
	for i := 0; i < count; i++ {
		obj, err := pm.Get(ctx, poolName)
		if err != nil {
			pm.PutMany(poolName, objects)
			return nil, err
		}
		objects = append(objects, obj)
	}
	return objects, nil
}

// TryGet attempts to acquire an object from the named pool without blocking.
// It returns (nil, false, nil) when no objects are currently available.
func (pm *PoolManager) TryGet(poolName string) (PooledObject, bool, error) {
	select {
	case <-pm.shutdownCh:
		return nil, false, ErrPoolManagerClosed
	default:
	}

	pool, err := pm.lookup(poolName)
	if err != nil {
		return nil, false, err
	}

	obj, ok, err := pool.tryGet()
	if err != nil || !ok {
		return nil, ok, err
	}

	pm.inFlight.Add(1)
	pm.activeCount.Add(1)
	return obj, true, nil
}

// TryGetMany attempts to acquire multiple objects without blocking.
func (pm *PoolManager) TryGetMany(poolName string, count int) ([]PooledObject, bool, error) {
	if count <= 0 {
		return nil, true, nil
	}

	objects := make([]PooledObject, 0, count)
	for i := 0; i < count; i++ {
		obj, ok, err := pm.TryGet(poolName)
		if err != nil {
			pm.PutMany(poolName, objects)
			return nil, false, err
		}
		if !ok {
			pm.PutMany(poolName, objects)
			return nil, false, nil
		}
		objects = append(objects, obj)
	}
	return objects, true, nil
}

// Put returns an object to the named pool, panicking if the pool is unknown.
func (pm *PoolManager) Put(poolName string, obj PooledObject) {
	pool, err := pm.lookup(poolName)
	if err != nil {
		panic(err)
	}

	defer pm.inFlight.Done()
	defer pm.activeCount.Add(-1)
	if err := pool.put(obj); err != nil {
		panic(err)
	}
}

// PutMany returns multiple objects to the named pool.
func (pm *PoolManager) PutMany(poolName string, objects []PooledObject) {
	for _, obj := range objects {
		if obj == nil {
			continue
		}
		pm.Put(poolName, obj)
	}
}

// TryPut attempts to return an object to the pool without blocking.
// It returns false when the pool rejected the object (for example due to a double put).
func (pm *PoolManager) TryPut(poolName string, obj PooledObject) (bool, error) {
	pool, err := pm.lookup(poolName)
	if err != nil {
		return false, err
	}

	success, putErr := pool.tryPut(obj)
	if success {
		pm.inFlight.Done()
		pm.activeCount.Add(-1)
	} else if putErr == nil {
		log.Printf("pool manager: try put rejected for pool %s", poolName)
	}
	return success, putErr
}

// TryPutMany attempts to return multiple objects without blocking.
func (pm *PoolManager) TryPutMany(poolName string, objects []PooledObject) (bool, error) {
	if len(objects) == 0 {
		return true, nil
	}

	pool, err := pm.lookup(poolName)
	if err != nil {
		return false, err
	}

	success := true
	for _, obj := range objects {
		if obj == nil {
			continue
		}
		putSuccess, putErr := pool.tryPut(obj)
		if putSuccess {
			pm.inFlight.Done()
			pm.activeCount.Add(-1)
			continue
		}
		if putErr != nil {
			return false, putErr
		}
		log.Printf("pool manager: try put many rejected for pool %s", poolName)
		success = false
	}
	return success, nil
}

// Shutdown waits for all in-flight pooled objects to be returned or cancels
// after the provided context (defaulting to 5 seconds). Outstanding objects
// are logged with acquisition stacks when available.
func (pm *PoolManager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok {
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
	}
	if cancel != nil {
		defer cancel()
	}

	pm.shutdownOnce.Do(func() {
		close(pm.shutdownCh)
		pm.mu.Lock()
		for _, pool := range pm.pools {
			if pool != nil {
				pool.close()
			}
		}
		pm.mu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		pm.inFlight.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		remaining := pm.activeCount.Load()
		pm.logOutstanding(remaining)
		return fmt.Errorf("shutdown timeout: %d pooled objects unreturned", remaining)
	}
}

func (pm *PoolManager) lookup(name string) (*objectPool, error) {
	pm.mu.RLock()
	pool, ok := pm.pools[name]
	pm.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPoolNotRegistered, name)
	}
	return pool, nil
}

func (pm *PoolManager) logOutstanding(remaining int64) {
	if remaining <= 0 {
		return
	}
	log.Printf("pool manager: shutdown timed out with %d objects in flight", remaining)
	log.Printf("pool manager: outstanding pools may still hold borrowed objects")
}

// BorrowCanonicalEvent acquires a CanonicalEvent from the pool.
func (pm *PoolManager) BorrowCanonicalEvent(ctx context.Context) (*schema.Event, error) {
	if pm == nil {
		return nil, fmt.Errorf("canonical event pool unavailable")
	}
	obj, err := pm.Get(ctx, "CanonicalEvent")
	if err != nil {
		return nil, err
	}
	evt, ok := obj.(*schema.Event)
	if !ok {
		pm.Put("CanonicalEvent", obj)
		return nil, fmt.Errorf("canonical event pool: unexpected type %T", obj)
	}
	evt.Reset()
	return evt, nil
}

// BorrowCanonicalEvents acquires multiple CanonicalEvents from the pool.
func (pm *PoolManager) BorrowCanonicalEvents(ctx context.Context, count int) ([]*schema.Event, error) {
	if pm == nil {
		return nil, fmt.Errorf("canonical event pool unavailable")
	}
	if count <= 0 {
		return nil, nil
	}

	objects, err := pm.GetMany(ctx, "CanonicalEvent", count)
	if err != nil {
		return nil, err
	}

	events := make([]*schema.Event, len(objects))
	for i, obj := range objects {
		evt, ok := obj.(*schema.Event)
		if !ok {
			pm.PutMany("CanonicalEvent", objects)
			return nil, fmt.Errorf("canonical event pool: unexpected type %T", obj)
		}
		evt.Reset()
		events[i] = evt
	}
	return events, nil
}

// TryBorrowCanonicalEvent attempts to acquire a CanonicalEvent without blocking.
func (pm *PoolManager) TryBorrowCanonicalEvent() (*schema.Event, bool, error) {
	if pm == nil {
		return nil, false, fmt.Errorf("canonical event pool unavailable")
	}
	obj, ok, err := pm.TryGet("CanonicalEvent")
	if err != nil || !ok {
		return nil, ok, err
	}
	evt, typeOK := obj.(*schema.Event)
	if !typeOK {
		pm.Put("CanonicalEvent", obj)
		return nil, false, fmt.Errorf("canonical event pool: unexpected type %T", obj)
	}
	evt.Reset()
	return evt, true, nil
}

// TryBorrowCanonicalEvents attempts to acquire multiple CanonicalEvents without blocking.
func (pm *PoolManager) TryBorrowCanonicalEvents(count int) ([]*schema.Event, bool, error) {
	if pm == nil {
		return nil, false, fmt.Errorf("canonical event pool unavailable")
	}
	if count <= 0 {
		return nil, true, nil
	}

	objects, ok, err := pm.TryGetMany("CanonicalEvent", count)
	if err != nil || !ok {
		return nil, ok, err
	}

	events := make([]*schema.Event, len(objects))
	for i, obj := range objects {
		evt, typeOK := obj.(*schema.Event)
		if !typeOK {
			pm.PutMany("CanonicalEvent", objects)
			return nil, false, fmt.Errorf("canonical event pool: unexpected type %T", obj)
		}
		evt.Reset()
		events[i] = evt
	}
	return events, true, nil
}

// RecycleCanonicalEvent returns the event to the pool.
func (pm *PoolManager) RecycleCanonicalEvent(evt *schema.Event) {
	if pm == nil || evt == nil {
		return
	}
	pm.Put("CanonicalEvent", evt)
}

// TryRecycleCanonicalEvent attempts to recycle the event without blocking.
func (pm *PoolManager) TryRecycleCanonicalEvent(evt *schema.Event) bool {
	if pm == nil || evt == nil {
		log.Printf("pool manager: try recycle canonical event skipped (manager=%v, event=nil? %t)", pm, evt == nil)
		return false
	}
	ok, err := pm.TryPut("CanonicalEvent", evt)
	if err != nil {
		log.Printf("pool manager: try recycle canonical event error: %v", err)
	}
	return err == nil && ok
}

// RecycleCanonicalEvents recycles a slice of events to the pool.
func (pm *PoolManager) RecycleCanonicalEvents(events []*schema.Event) {
	if pm == nil || len(events) == 0 {
		return
	}
	objects := make([]PooledObject, 0, len(events))
	for _, evt := range events {
		if evt == nil {
			continue
		}
		objects = append(objects, evt)
	}
	pm.PutMany("CanonicalEvent", objects)
}

// TryRecycleCanonicalEvents attempts to recycle multiple events without blocking.
func (pm *PoolManager) TryRecycleCanonicalEvents(events []*schema.Event) bool {
	if pm == nil {
		log.Printf("pool manager: try recycle canonical events skipped (manager nil)")
		return false
	}
	if len(events) == 0 {
		return true
	}

	objects := make([]PooledObject, 0, len(events))
	for _, evt := range events {
		if evt == nil {
			log.Printf("pool manager: try recycle canonical events encountered nil event")
			continue
		}
		objects = append(objects, evt)
	}
	ok, err := pm.TryPutMany("CanonicalEvent", objects)
	if err != nil {
		log.Printf("pool manager: try recycle canonical events error: %v", err)
		return false
	}
	if !ok {
		log.Printf("pool manager: try recycle canonical events rejected by pool")
	}
	return ok
}
