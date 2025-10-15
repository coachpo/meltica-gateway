package recycler

import (
	"sync"
	"sync/atomic"
)

var (
	globalInstance atomic.Pointer[RecyclerImpl]
	initOnce       sync.Once
)

// InitGlobal initializes the singleton recycler instance. Subsequent calls are no-ops.
func InitGlobal(eventPool, execReportPool *sync.Pool, metrics *RecyclerMetrics) {
	initOnce.Do(func() {
		instance := NewRecycler(eventPool, execReportPool, metrics)
		globalInstance.Store(instance)
	})
}

// Global returns the initialized singleton recycler instance.
func Global() *RecyclerImpl {
	instance := globalInstance.Load()
	if instance == nil {
		panic("recycler: global instance not initialized")
	}
	return instance
}
