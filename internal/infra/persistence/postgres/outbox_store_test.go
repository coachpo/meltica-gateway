package postgres

import (
	"context"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/domain/outboxstore"
)

func TestOutboxStoreNilPool(t *testing.T) {
	store := NewOutboxStore(nil)
	ctx := context.Background()
	event := outboxstore.Event{
		AggregateType: "eventbus",
		AggregateID:   "evt-1",
		EventType:     "Trade",
		Payload:       json.RawMessage(`{"eventId":"evt-1"}`),
	}
	if _, err := store.Enqueue(ctx, event); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if _, err := store.ListPending(ctx, 1); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if err := store.MarkDelivered(ctx, 1); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if err := store.MarkFailed(ctx, 1, "error"); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if err := store.Delete(ctx, 1); err == nil {
		t.Fatalf("expected error when pool nil")
	}
}
