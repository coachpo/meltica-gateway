// Package databus defines pub/sub interfaces for canonical events.
package databus

import (
	"context"

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
	BufferSize int
}

func (c MemoryConfig) normalize() MemoryConfig {
	if c.BufferSize <= 0 {
		c.BufferSize = 64
	}
	return c
}
