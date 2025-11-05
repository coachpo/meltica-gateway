package postgres

import "testing"

func TestNewStoreAllowsNilPool(t *testing.T) {
	store := New(nil)
	if store == nil {
		t.Fatalf("expected store instance")
	}
	if store.Pool() != nil {
		t.Fatalf("expected nil pool passthrough")
	}
}
