package pool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

func TestNewPoolManager(t *testing.T) {
	pm := NewPoolManager()
	if pm == nil {
		t.Fatal("expected non-nil pool manager")
	}
	if pm.pools == nil {
		t.Error("expected pools map to be initialized")
	}
}

func TestRegisterPool(t *testing.T) {
	pm := NewPoolManager()
	
	factory := func() any {
		return &schema.Event{}
	}
	
	err := pm.RegisterPool("test-pool", 10, factory)
	if err != nil {
		t.Fatalf("RegisterPool failed: %v", err)
	}
	
	// Try to register same pool again
	err = pm.RegisterPool("test-pool", 10, factory)
	if err == nil {
		t.Error("expected error when registering duplicate pool")
	}
}

func TestRegisterPoolInvalidCapacity(t *testing.T) {
	pm := NewPoolManager()
	
	factory := func() any {
		return &schema.Event{}
	}
	
	err := pm.RegisterPool("test-pool", 0, factory)
	if err == nil {
		t.Error("expected error for zero capacity")
	}
	
	err = pm.RegisterPool("test-pool", -1, factory)
	if err == nil {
		t.Error("expected error for negative capacity")
	}
}

func TestGetAndPut(t *testing.T) {
	pm := NewPoolManager()
	
	factory := func() any {
		return &schema.Event{}
	}
	
	err := pm.RegisterPool("events", 5, factory)
	if err != nil {
		t.Fatalf("RegisterPool failed: %v", err)
	}
	
	ctx := context.Background()
	
	// Borrow object
	obj, err := pm.Get(ctx, "events")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if obj == nil {
		t.Fatal("expected non-nil object")
	}
	
	// Verify it's the right type
	evt, ok := obj.(*schema.Event)
	if !ok {
		t.Fatalf("expected *schema.Event, got %T", obj)
	}
	
	// Use the object
	evt.EventID = "test-123"
	evt.Provider = "test-provider"
	
	// Return object
	pm.Put("events", obj)
	
	// Borrow again - should be reset
	obj2, err := pm.Get(ctx, "events")
	if err != nil {
		t.Fatalf("second Get failed: %v", err)
	}
	
	evt2, ok := obj2.(*schema.Event)
	if !ok {
		t.Fatalf("expected *schema.Event, got %T", obj2)
	}
	
	// Should be reset
	if evt2.EventID != "" {
		t.Errorf("expected reset EventID, got %q", evt2.EventID)
	}
	if evt2.Provider != "" {
		t.Errorf("expected reset Provider, got %q", evt2.Provider)
	}
	
	pm.Put("events", obj2)
}

func TestGetNonExistentPool(t *testing.T) {
	pm := NewPoolManager()
	
	ctx := context.Background()
	_, err := pm.Get(ctx, "non-existent")
	if err == nil {
		t.Error("expected error for non-existent pool")
	}
	// Error is wrapped, check the error contains expected message
	if err != nil && !errors.Is(err, ErrPoolNotRegistered) {
		// Check if error message contains expected text
		if !contains(err.Error(), "not registered") {
			t.Errorf("expected error about pool not registered, got %v", err)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestTryGet(t *testing.T) {
	pm := NewPoolManager()
	
	factory := func() any {
		return &schema.Event{}
	}
	
	err := pm.RegisterPool("events", 2, factory)
	if err != nil {
		t.Fatalf("RegisterPool failed: %v", err)
	}
	
	// TryGet should succeed
	obj, ok, err := pm.TryGet("events")
	if err != nil {
		t.Fatalf("TryGet failed: %v", err)
	}
	if !ok {
		t.Fatal("TryGet returned false")
	}
	if obj == nil {
		t.Fatal("expected non-nil object")
	}
	
	pm.Put("events", obj)
}

func TestGetMany(t *testing.T) {
	pm := NewPoolManager()
	
	factory := func() any {
		return &schema.Event{}
	}
	
	err := pm.RegisterPool("events", 10, factory)
	if err != nil {
		t.Fatalf("RegisterPool failed: %v", err)
	}
	
	ctx := context.Background()
	
	// Get multiple objects
	objs, err := pm.GetMany(ctx, "events", 3)
	if err != nil {
		t.Fatalf("GetMany failed: %v", err)
	}
	if len(objs) != 3 {
		t.Errorf("expected 3 objects, got %d", len(objs))
	}
	
	// All should be non-nil
	for i, obj := range objs {
		if obj == nil {
			t.Errorf("object %d is nil", i)
		}
	}
	
	// Return them
	pm.PutMany("events", objs)
}

func TestGetManyZeroCount(t *testing.T) {
	pm := NewPoolManager()
	
	factory := func() any {
		return &schema.Event{}
	}
	
	err := pm.RegisterPool("events", 10, factory)
	if err != nil {
		t.Fatalf("RegisterPool failed: %v", err)
	}
	
	ctx := context.Background()
	
	objs, err := pm.GetMany(ctx, "events", 0)
	if err != nil {
		t.Errorf("GetMany with 0 count failed: %v", err)
	}
	if len(objs) != 0 {
		t.Errorf("expected empty slice, got %d objects", len(objs))
	}
}

func TestTryPut(t *testing.T) {
	pm := NewPoolManager()
	
	factory := func() any {
		return &schema.Event{}
	}
	
	err := pm.RegisterPool("events", 2, factory)
	if err != nil {
		t.Fatalf("RegisterPool failed: %v", err)
	}
	
	ctx := context.Background()
	
	obj, err := pm.Get(ctx, "events")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	
	// TryPut should succeed
	ok, err := pm.TryPut("events", obj)
	if err != nil {
		t.Fatalf("TryPut failed: %v", err)
	}
	if !ok {
		t.Error("TryPut returned false")
	}
}

func TestShutdown(t *testing.T) {
	pm := NewPoolManager()
	
	factory := func() any {
		return &schema.Event{}
	}
	
	err := pm.RegisterPool("events", 5, factory)
	if err != nil {
		t.Fatalf("RegisterPool failed: %v", err)
	}
	
	ctx := context.Background()
	
	// Borrow and return an object
	obj, err := pm.Get(ctx, "events")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	pm.Put("events", obj)
	
	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err = pm.Shutdown(shutdownCtx)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
	
	// Operations after shutdown should fail
	_, err = pm.Get(ctx, "events")
	if err != ErrPoolManagerClosed {
		t.Errorf("expected ErrPoolManagerClosed after shutdown, got %v", err)
	}
}

func TestBorrowEventInst(t *testing.T) {
	pm := NewPoolManager()
	
	// Register canonical event pool
	err := pm.RegisterPool("Event", 10, func() any {
		return &schema.Event{}
	})
	if err != nil {
		t.Fatalf("RegisterPool failed: %v", err)
	}
	
	ctx := context.Background()
	
	evt, err := pm.BorrowEventInst(ctx)
	if err != nil {
		t.Fatalf("BorrowEventInst failed: %v", err)
	}
	if evt == nil {
		t.Fatal("expected non-nil event")
	}
	
	// Should be clean
	if evt.EventID != "" || evt.Provider != "" {
		t.Error("expected reset event")
	}
	
	pm.ReturnEventInst(evt)
}

func TestReturnEventInst(t *testing.T) {
	pm := NewPoolManager()
	
	err := pm.RegisterPool("Event", 10, func() any {
		return &schema.Event{}
	})
	if err != nil {
		t.Fatalf("RegisterPool failed: %v", err)
	}
	
	ctx := context.Background()
	
	evt, err := pm.BorrowEventInst(ctx)
	if err != nil {
		t.Fatalf("BorrowEventInst failed: %v", err)
	}
	
	evt.EventID = "test-123"
	
	pm.ReturnEventInst(evt)
	
	// Borrow again
	evt2, err := pm.BorrowEventInst(ctx)
	if err != nil {
		t.Fatalf("second BorrowEventInst failed: %v", err)
	}
	
	// Should be reset
	if evt2.EventID != "" {
		t.Error("expected reset event")
	}
	
	pm.ReturnEventInst(evt2)
}

func TestBorrowEventInsts(t *testing.T) {
	pm := NewPoolManager()
	
	err := pm.RegisterPool("Event", 20, func() any {
		return &schema.Event{}
	})
	if err != nil {
		t.Fatalf("RegisterPool failed: %v", err)
	}
	
	ctx := context.Background()
	
	events, err := pm.BorrowEventInsts(ctx, 5)
	if err != nil {
		t.Fatalf("BorrowEventInsts failed: %v", err)
	}
	if len(events) != 5 {
		t.Errorf("expected 5 events, got %d", len(events))
	}
	
	for i, evt := range events {
		if evt == nil {
			t.Errorf("event %d is nil", i)
		}
	}
	
	pm.ReturnEventInsts(events)
}

func TestReturnEventInstsNil(t *testing.T) {
	pm := NewPoolManager()
	
	// Should not panic
	pm.ReturnEventInsts(nil)
	pm.ReturnEventInsts([]*schema.Event{})
}

func TestTryReturnEventInst(t *testing.T) {
	pm := NewPoolManager()
	
	err := pm.RegisterPool("Event", 10, func() any {
		return &schema.Event{}
	})
	if err != nil {
		t.Fatalf("RegisterPool failed: %v", err)
	}
	
	ctx := context.Background()
	
	evt, err := pm.BorrowEventInst(ctx)
	if err != nil {
		t.Fatalf("BorrowEventInst failed: %v", err)
	}
	
	ok := pm.TryReturnEventInst(evt)
	if !ok {
		t.Error("TryReturnEventInst returned false")
	}
}
