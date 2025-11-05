package postgres

import (
	"context"
	"testing"

	"github.com/coachpo/meltica/internal/domain/providerstore"
)

func TestProviderStoreNilPool(t *testing.T) {
	store := NewProviderStore(nil)
	ctx := context.Background()
	snapshot := providerstore.Snapshot{Name: "binance", Adapter: "binance"}
	if err := store.SaveProvider(ctx, snapshot); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if err := store.DeleteProvider(ctx, "binance"); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if _, err := store.LoadProviders(ctx); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	routes := []providerstore.RouteSnapshot{{}}
	if err := store.SaveRoutes(ctx, "binance", routes); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if err := store.DeleteRoutes(ctx, "binance"); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if _, err := store.LoadRoutes(ctx, "binance"); err == nil {
		t.Fatalf("expected error when pool nil")
	}
}
