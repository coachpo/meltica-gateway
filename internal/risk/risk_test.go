package risk

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/schema"
)

func TestManager_CheckOrder_Throttle(t *testing.T) {
	limits := Limits{
		OrderThrottle:   10, // 10 orders per second
		MaxPositionSize: decimal.NewFromInt(100),
	}
	manager := NewManager(limits)

	req := &schema.OrderRequest{
		Quantity: "1",
	}

	// First 10 orders should pass
	for i := 0; i < 10; i++ {
		if err := manager.CheckOrder(context.Background(), req); err != nil {
			t.Fatalf("order %d should have passed, but got error: %v", i+1, err)
		}
	}

	// 11th order should be throttled
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := manager.CheckOrder(ctx, req); err == nil {
		t.Fatal("11th order should have been throttled, but it was not")
	}
}

func TestManager_CheckOrder_PositionLimit(t *testing.T) {
	limits := Limits{
		OrderThrottle:   100,
		MaxPositionSize: decimal.NewFromInt(10),
	}
	manager := NewManager(limits)

	// Order that exceeds the position limit
	req := &schema.OrderRequest{
		Quantity: "11",
	}

	err := manager.CheckOrder(context.Background(), req)
	if err == nil {
		t.Fatal("order should have been rejected due to position limit, but it was not")
	}
}
