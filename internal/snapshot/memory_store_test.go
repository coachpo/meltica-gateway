package snapshot

import (
	"context"
	"testing"
	"time"
)

func TestNewMemoryStore(t *testing.T) {
	store := NewMemoryStore()
	
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	
	store.Close()
}

func TestMemoryStoreGetNotFound(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	
	ctx := context.Background()
	key := Key{
		Market:     "BINANCE-SPOT",
		Instrument: "BTC-USD",
		Type:       "TICKER",
	}
	
	_, err := store.Get(ctx, key)
	if err == nil {
		t.Error("expected error for non-existent key")
	}
}

func TestMemoryStorePutAndGet(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	
	ctx := context.Background()
	key := Key{
		Market:     "BINANCE-SPOT",
		Instrument: "BTC-USD",
		Type:       "TICKER",
	}
	
	record := Record{
		Key:  key,
		Seq:  1,
		Data: map[string]any{"price": "50000"},
	}
	
	// Put
	saved, err := store.Put(ctx, record)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	
	if saved.Version != 1 {
		t.Errorf("expected version 1, got %d", saved.Version)
	}
	
	// Get
	retrieved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	
	if retrieved.Key.Instrument != "BTC-USD" {
		t.Errorf("expected instrument BTC-USD, got %s", retrieved.Key.Instrument)
	}
	if retrieved.Seq != 1 {
		t.Errorf("expected seq 1, got %d", retrieved.Seq)
	}
}

func TestMemoryStoreCompareAndSwapSuccess(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	
	ctx := context.Background()
	key := Key{
		Market:     "BINANCE-SPOT",
		Instrument: "ETH-USD",
		Type:       "TICKER",
	}
	
	// Initial put
	record := Record{
		Key:  key,
		Seq:  1,
		Data: map[string]any{"price": "3000"},
	}
	
	saved, err := store.Put(ctx, record)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	
	// CAS with correct version
	updated := Record{
		Key:  key,
		Seq:  2,
		Data: map[string]any{"price": "3100"},
	}
	
	result, err := store.CompareAndSwap(ctx, saved.Version, updated)
	if err != nil {
		t.Fatalf("CompareAndSwap() error = %v", err)
	}
	
	if result.Version != 2 {
		t.Errorf("expected version 2, got %d", result.Version)
	}
	if result.Seq != 2 {
		t.Errorf("expected seq 2, got %d", result.Seq)
	}
}

func TestMemoryStoreCompareAndSwapVersionMismatch(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	
	ctx := context.Background()
	key := Key{
		Market:     "BINANCE-SPOT",
		Instrument: "BTC-USD",
		Type:       "TICKER",
	}
	
	// Initial put
	record := Record{
		Key:  key,
		Seq:  1,
		Data: map[string]any{"price": "50000"},
	}
	
	_, err := store.Put(ctx, record)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	
	// CAS with wrong version
	updated := Record{
		Key:  key,
		Seq:  2,
		Data: map[string]any{"price": "51000"},
	}
	
	_, err = store.CompareAndSwap(ctx, 999, updated) // Wrong version
	if err == nil {
		t.Error("expected error for version mismatch")
	}
}

func TestMemoryStoreCompareAndSwapNotFound(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	
	ctx := context.Background()
	key := Key{
		Market:     "BINANCE-SPOT",
		Instrument: "BTC-USD",
		Type:       "TICKER",
	}
	
	record := Record{
		Key:  key,
		Seq:  1,
		Data: map[string]any{},
	}
	
	_, err := store.CompareAndSwap(ctx, 1, record)
	if err == nil {
		t.Error("expected error for non-existent key")
	}
}

func TestMemoryStoreTTLExpiration(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	
	ctx := context.Background()
	key := Key{
		Market:     "BINANCE-SPOT",
		Instrument: "BTC-USD",
		Type:       "TICKER",
	}
	
	record := Record{
		Key:       key,
		Seq:       1,
		Data:      map[string]any{"price": "50000"},
		TTL:       50 * time.Millisecond,
		UpdatedAt: time.Now().UTC(),
	}
	
	_, err := store.Put(ctx, record)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	
	// Wait for expiration
	time.Sleep(100 * time.Millisecond)
	
	// Get should return stale marker
	retrieved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	
	if stale, ok := retrieved.Data["stale"].(bool); !ok || !stale {
		t.Error("expected stale marker for expired record")
	}
}

func TestMemoryStorePruneExpired(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	
	ctx := context.Background()
	
	// Add expired record
	key1 := Key{Market: "BINANCE-SPOT", Instrument: "BTC-USD", Type: "TICKER"}
	record1 := Record{
		Key:       key1,
		Seq:       1,
		Data:      map[string]any{},
		TTL:       10 * time.Millisecond,
		UpdatedAt: time.Now().Add(-100 * time.Millisecond),
	}
	store.Put(ctx, record1)
	
	// Add non-expired record
	key2 := Key{Market: "BINANCE-SPOT", Instrument: "ETH-USD", Type: "TICKER"}
	record2 := Record{
		Key:       key2,
		Seq:       1,
		Data:      map[string]any{},
		TTL:       time.Hour,
		UpdatedAt: time.Now(),
	}
	store.Put(ctx, record2)
	
	// Run pruning
	store.pruneExpired()
	
	// Expired should be gone
	_, err1 := store.Get(ctx, key1)
	if err1 == nil {
		t.Error("expected expired record to be pruned")
	}
	
	// Non-expired should still exist
	_, err2 := store.Get(ctx, key2)
	if err2 != nil {
		t.Error("expected non-expired record to still exist")
	}
}

func TestRecordClone(t *testing.T) {
	original := Record{
		Key: Key{Market: "BINANCE-SPOT", Instrument: "BTC-USD", Type: "TICKER"},
		Seq: 1,
		Data: map[string]any{
			"price": "50000",
			"volume": "100",
		},
		Version: 1,
	}
	
	clone := original.Clone()
	
	// Verify clone has same values
	if clone.Seq != original.Seq {
		t.Error("clone seq mismatch")
	}
	if clone.Version != original.Version {
		t.Error("clone version mismatch")
	}
	
	// Modify clone
	clone.Data["price"] = "51000"
	
	// Original should be unchanged
	if original.Data["price"] != "50000" {
		t.Error("original data was modified")
	}
}

func TestMemoryStoreContextCancellation(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately
	
	key := Key{
		Market:     "BINANCE-SPOT",
		Instrument: "BTC-USD",
		Type:       "TICKER",
	}
	
	record := Record{Key: key, Seq: 1, Data: map[string]any{}}
	
	// Operations should handle canceled context
	_, err := store.Put(ctx, record)
	if err == nil {
		t.Error("expected error for canceled context")
	}
}
