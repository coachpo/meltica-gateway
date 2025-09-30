// Package recycler provides pooling utilities with debug protections.
package recycler

import (
	"fmt"
	"runtime/debug"
	"unsafe"
)

const poisonPattern uint64 = 0xDEADBEEFDEADBEEF

func (r *RecyclerImpl) guardDoublePut(ptr unsafe.Pointer) {
	if ptr == nil {
		return
	}
	if _, loaded := r.putTracker.LoadOrStore(ptr, struct{}{}); loaded {
		r.metrics.incDoublePut()
		panic(fmt.Sprintf("recycler: double-put detected for %p\n%s", ptr, debug.Stack()))
	}
}

func (r *RecyclerImpl) releasePointer(ptr unsafe.Pointer) {
	if ptr == nil {
		return
	}
	r.putTracker.Delete(ptr)
}

func poisonEventMemory(ptr unsafe.Pointer) {
	if ptr == nil {
		return
	}
	*(*uint64)(ptr) = poisonPattern
}
