package pool

// PooledObject describes objects managed by a bounded pool.
type PooledObject interface {
	Reset()
	SetReturned(bool)
	IsReturned() bool
}
