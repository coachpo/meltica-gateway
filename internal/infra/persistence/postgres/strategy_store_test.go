package postgres

import (
	"context"
	"testing"

	"github.com/coachpo/meltica/internal/domain/strategystore"
)

func TestStrategyStoreNilPool(t *testing.T) {
	store := NewStrategyStore(nil)
	ctx := context.Background()
	snapshot := strategystore.Snapshot{ID: "alpha"}
	if err := store.Save(ctx, snapshot); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if err := store.Delete(ctx, "alpha"); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if _, err := store.Load(ctx); err == nil {
		t.Fatalf("expected error when pool nil")
	}
}
