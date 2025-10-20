// Package eventbus defines pub/sub interfaces for canonical events.
package eventbus

import (
	"context"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// SubscriptionID uniquely identifies a bus subscription.
type SubscriptionID string

// Bus delivers canonical events to interested subscribers.
type Bus interface {
	Publish(ctx context.Context, evt *schema.Event) error
	Subscribe(ctx context.Context, typ schema.EventType) (SubscriptionID, <-chan *schema.Event, error)
	Unsubscribe(id SubscriptionID)
	Close()
}

// MemoryConfig configures the in-memory bus buffers.
type MemoryConfig struct {
	BufferSize    int
	FanoutWorkers int
	Pools         *pool.PoolManager
}

func (c MemoryConfig) normalize() MemoryConfig {
	if c.BufferSize <= 0 {
		c.BufferSize = 64
	}
	if c.FanoutWorkers <= 0 {
		c.FanoutWorkers = 4
	}
	return c
}
