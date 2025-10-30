// Package pool provides object pooling for high-frequency allocations.
package pool

// PooledObject describes objects managed by a bounded pool.
type PooledObject interface {
	Reset()
	SetReturned(bool)
	IsReturned() bool
}
