package pool

import (
	"fmt"
	"runtime/debug"
)

func ensureReturnable(obj PooledObject, poolName string) {
	if !obj.IsReturned() {
		return
	}
	panic(fmt.Sprintf("pool %s: double-Put() detected for %T\n%s", poolName, obj, debug.Stack()))
}

func markAcquired(obj PooledObject) {
	obj.SetReturned(false)
}

func markReturned(obj PooledObject) {
	obj.SetReturned(true)
}
