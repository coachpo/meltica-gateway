//go:build !debug

package pool

type debugState struct{}

func newDebugState(string) *debugState { return nil }

func (d *debugState) recordAcquire(PooledObject) {}

func (d *debugState) recordRelease(PooledObject) {}

func (d *debugState) activeStacks() []string { return nil }

func (d *debugState) poison(PooledObject) {}

func (d *debugState) clear(PooledObject) {}
