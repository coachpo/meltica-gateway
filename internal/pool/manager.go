package pool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/coachpo/meltica/internal/schema"
	"github.com/coachpo/meltica/internal/telemetry"
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
// Telemetry Metrics (all include attributes: pool.name, object.type):
// - pool.objects.borrowed: Counter of total objects borrowed
// - pool.objects.active: Gauge of currently borrowed objects across all pools
// - pool.borrow.duration: Histogram of time to acquire objects
// - pool.capacity: Observable gauge of each pool's total capacity
// - pool.available: Observable gauge of available objects per pool
//
// Note: Pool utilization can be computed as (active/capacity) in query layer.
//
//nolint:revive // PoolManager matches specification terminology.
type PoolManager struct {
	mu           sync.RWMutex
	pools        map[string]*objectPool
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
	inFlight     sync.WaitGroup
	activeCount  atomic.Int64

	objectsBorrowedCounter metric.Int64Counter
	activeObjectsGauge     metric.Int64UpDownCounter
	borrowDuration         metric.Float64Histogram
	poolCapacityGauge      metric.Int64ObservableGauge
	poolAvailableGauge     metric.Int64ObservableGauge
}

// NewPoolManager constructs an initialized pool manager ready for pool registration.
func NewPoolManager() *PoolManager {
	pm := new(PoolManager)
	pm.pools = make(map[string]*objectPool)
	pm.shutdownCh = make(chan struct{})

	meter := otel.Meter("pool")
	pm.objectsBorrowedCounter, _ = meter.Int64Counter("pool.objects.borrowed",
		metric.WithDescription("Number of objects borrowed from pools"),
		metric.WithUnit("{object}"))
	pm.activeObjectsGauge, _ = meter.Int64UpDownCounter("pool.objects.active",
		metric.WithDescription("Number of currently active borrowed objects"),
		metric.WithUnit("{object}"))
	pm.borrowDuration, _ = meter.Float64Histogram("pool.borrow.duration",
		metric.WithDescription("Time taken to borrow an object from pool"),
		metric.WithUnit("ms"))

	pm.poolCapacityGauge, _ = meter.Int64ObservableGauge("pool.capacity",
		metric.WithDescription("Total capacity of each pool"),
		metric.WithUnit("{object}"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			pm.mu.RLock()
			defer pm.mu.RUnlock()
			for name, pool := range pm.pools {
				if pool != nil {
					observer.Observe(int64(pool.getCapacity()), metric.WithAttributes(
						attribute.String("environment", telemetry.Environment()),
						attribute.String("pool_name", name),
						attribute.String("object_type", pool.getObjectType())))
				}
			}
			return nil
		}))

	pm.poolAvailableGauge, _ = meter.Int64ObservableGauge("pool.available",
		metric.WithDescription("Number of available objects in each pool"),
		metric.WithUnit("{object}"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			pm.mu.RLock()
			defer pm.mu.RUnlock()
			for name, pool := range pm.pools {
				if pool != nil {
					observer.Observe(pool.getAvailable(), metric.WithAttributes(
						attribute.String("environment", telemetry.Environment()),
						attribute.String("pool_name", name),
						attribute.String("object_type", pool.getObjectType())))
				}
			}
			return nil
		}))

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

	var objectType string
	factory := func() PooledObject {
		obj := newFunc()
		po, ok := obj.(PooledObject)
		if !ok {
			panic(fmt.Sprintf("pool manager: object does not implement PooledObject: %T", obj))
		}
		if objectType == "" {
			objectType = fmt.Sprintf("%T", po)
		}
		return po
	}

	sampleObj := factory()
	sampleObj.Reset()

	pool, err := newObjectPool(name, objectType, capacity, factory)
	if err != nil {
		return err
	}
	pm.pools[name] = pool
	return nil
}

// Get acquires an object from the named pool respecting manager shutdown state.
func (pm *PoolManager) Get(ctx context.Context, poolName string) (PooledObject, error) {
	start := time.Now()

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

	objectType := pool.getObjectType()
	if pm.objectsBorrowedCounter != nil {
		pm.objectsBorrowedCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("environment", telemetry.Environment()),
			attribute.String("pool_name", poolName),
			attribute.String("object_type", objectType)))
	}
	pm.recordActiveGauge(ctx, pool, poolName, 1)
	if pm.borrowDuration != nil {
		duration := time.Since(start).Milliseconds()
		pm.borrowDuration.Record(ctx, float64(duration), metric.WithAttributes(
			attribute.String("environment", telemetry.Environment()),
			attribute.String("pool_name", poolName),
			attribute.String("object_type", objectType)))
	}

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
	pm.recordActiveGauge(context.Background(), pool, poolName, 1)
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

	pm.recordActiveGauge(context.Background(), pool, poolName, -1)
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
		pm.recordActiveGauge(context.Background(), pool, poolName, -1)
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
			pm.recordActiveGauge(context.Background(), pool, poolName, -1)
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

func (pm *PoolManager) recordActiveGauge(ctx context.Context, pool *objectPool, poolName string, delta int64) {
	if pm.activeObjectsGauge == nil || pool == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	pm.activeObjectsGauge.Add(ctx, delta, metric.WithAttributes(
		attribute.String("environment", telemetry.Environment()),
		attribute.String("pool_name", poolName),
		attribute.String("object_type", pool.getObjectType())))
}

func (pm *PoolManager) logOutstanding(remaining int64) {
	if remaining <= 0 {
		return
	}
	log.Printf("pool manager: shutdown timed out with %d objects in flight", remaining)
	log.Printf("pool manager: outstanding pools may still hold borrowed objects")
}

// BorrowEventInst acquires an Event from the pool.
func (pm *PoolManager) BorrowEventInst(ctx context.Context) (*schema.Event, error) {
	if pm == nil {
		return nil, fmt.Errorf("event pool unavailable")
	}
	obj, err := pm.Get(ctx, "Event")
	if err != nil {
		return nil, err
	}
	evt, ok := obj.(*schema.Event)
	if !ok {
		pm.Put("Event", obj)
		return nil, fmt.Errorf("event pool: unexpected type %T", obj)
	}
	evt.Reset()
	return evt, nil
}

// BorrowEventInsts acquires multiple Events from the pool.
func (pm *PoolManager) BorrowEventInsts(ctx context.Context, count int) ([]*schema.Event, error) {
	if pm == nil {
		return nil, fmt.Errorf("event pool unavailable")
	}
	if count <= 0 {
		return nil, nil
	}

	objects, err := pm.GetMany(ctx, "Event", count)
	if err != nil {
		return nil, err
	}

	events := make([]*schema.Event, len(objects))
	for i, obj := range objects {
		evt, ok := obj.(*schema.Event)
		if !ok {
			pm.PutMany("Event", objects)
			return nil, fmt.Errorf("event pool: unexpected type %T", obj)
		}
		evt.Reset()
		events[i] = evt
	}
	return events, nil
}

// TryBorrowEventInst attempts to acquire an Event without blocking.
func (pm *PoolManager) TryBorrowEventInst() (*schema.Event, bool, error) {
	if pm == nil {
		return nil, false, fmt.Errorf("event pool unavailable")
	}
	obj, ok, err := pm.TryGet("Event")
	if err != nil || !ok {
		return nil, ok, err
	}
	evt, typeOK := obj.(*schema.Event)
	if !typeOK {
		pm.Put("Event", obj)
		return nil, false, fmt.Errorf("event pool: unexpected type %T", obj)
	}
	evt.Reset()
	return evt, true, nil
}

// TryBorrowEventInsts attempts to acquire multiple Events without blocking.
func (pm *PoolManager) TryBorrowEventInsts(count int) ([]*schema.Event, bool, error) {
	if pm == nil {
		return nil, false, fmt.Errorf("event pool unavailable")
	}
	if count <= 0 {
		return nil, true, nil
	}

	objects, ok, err := pm.TryGetMany("Event", count)
	if err != nil || !ok {
		return nil, ok, err
	}

	events := make([]*schema.Event, len(objects))
	for i, obj := range objects {
		evt, typeOK := obj.(*schema.Event)
		if !typeOK {
			pm.PutMany("Event", objects)
			return nil, false, fmt.Errorf("event pool: unexpected type %T", obj)
		}
		evt.Reset()
		events[i] = evt
	}
	return events, true, nil
}

// ReturnEventInst returns the event to the pool.
func (pm *PoolManager) ReturnEventInst(evt *schema.Event) {
	if pm == nil || evt == nil {
		return
	}
	pm.Put("Event", evt)
}

// TryReturnEventInst attempts to return the event without blocking.
func (pm *PoolManager) TryReturnEventInst(evt *schema.Event) bool {
	if pm == nil || evt == nil {
		log.Printf("pool manager: try return event skipped (manager=%v, event=nil? %t)", pm, evt == nil)
		return false
	}
	ok, err := pm.TryPut("Event", evt)
	if err != nil {
		log.Printf("pool manager: try return event error: %v", err)
	}
	return err == nil && ok
}

// ReturnEventInsts returns a slice of events to the pool.
func (pm *PoolManager) ReturnEventInsts(events []*schema.Event) {
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
	pm.PutMany("Event", objects)
}

// TryReturnEventInsts attempts to return multiple events without blocking.
func (pm *PoolManager) TryReturnEventInsts(events []*schema.Event) bool {
	if pm == nil {
		log.Printf("pool manager: try return events skipped (manager nil)")
		return false
	}
	if len(events) == 0 {
		return true
	}

	objects := make([]PooledObject, 0, len(events))
	for _, evt := range events {
		if evt == nil {
			log.Printf("pool manager: try return events encountered nil event")
			continue
		}
		objects = append(objects, evt)
	}
	ok, err := pm.TryPutMany("Event", objects)
	if err != nil {
		log.Printf("pool manager: try return events error: %v", err)
		return false
	}
	if !ok {
		log.Printf("pool manager: try return events rejected by pool")
	}
	return ok
}
