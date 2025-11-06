// Package outboxstore defines persistence contracts for durable event publishing.
package outboxstore

import (
	"context"
	"time"

	json "github.com/goccy/go-json"
)

// Event encapsulates a single outbox entry ready to be enqueued.
type Event struct {
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       json.RawMessage
	Headers       map[string]any
	AvailableAt   time.Time
}

// EventRecord captures the persisted state of an outbox entry.
type EventRecord struct {
	ID            int64
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       json.RawMessage
	Headers       map[string]any
	AvailableAt   time.Time
	PublishedAt   *time.Time
	Attempts      int
	LastError     string
	Delivered     bool
	CreatedAt     time.Time
}

// Store abstracts persistence operations for the outbox.
type Store interface {
	Enqueue(ctx context.Context, evt Event) (EventRecord, error)
	ListPending(ctx context.Context, limit int) ([]EventRecord, error)
	MarkDelivered(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, lastError string) error
	Delete(ctx context.Context, id int64) error
}
