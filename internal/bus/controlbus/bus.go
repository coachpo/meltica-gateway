// Package controlbus provides in-memory control plane messaging primitives.
package controlbus

import (
	"context"

	"github.com/coachpo/meltica/internal/schema"
)

// Message encapsulates a command and reply channel for consumers.
type Message struct {
	Command schema.ControlMessage
	Reply   chan<- schema.ControlAcknowledgement
}

// Bus allows control-plane commands to be distributed to interested consumers.
type Bus interface {
	Send(ctx context.Context, cmd schema.ControlMessage) (schema.ControlAcknowledgement, error)
	Consume(ctx context.Context) (<-chan Message, error)
	Close()
}

// MemoryConfig configures the in-memory control bus buffer sizing.
type MemoryConfig struct {
	BufferSize int
}

func (c MemoryConfig) normalize() MemoryConfig {
	if c.BufferSize <= 0 {
		c.BufferSize = 1
	}
	return c
}
