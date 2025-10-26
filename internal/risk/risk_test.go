package risk

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/schema"
)

func TestManager_CheckOrder_Throttle(t *testing.T) {
	limits := Limits{
		MaxPositionSize:  decimal.NewFromInt(1_000),
		MaxNotionalValue: decimal.NewFromInt(1_000_000),
		OrderThrottle:    10,
		OrderBurst:       10,
	}
	manager := NewManager(limits)

	price := "1"
	req := &schema.OrderRequest{
		Provider:      "fake",
		Symbol:        "BTC-USDT",
		Side:          schema.TradeSideBuy,
		OrderType:     schema.OrderTypeLimit,
		Price:         &price,
		Quantity:      "1",
		ClientOrderID: "ord-0",
	}

	for i := 0; i < 10; i++ {
		req.ClientOrderID = fmt.Sprintf("ord-%d", i)
		if err := manager.CheckOrder(context.Background(), req); err != nil {
			t.Fatalf("order %d should have passed, but got error: %v", i+1, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req.ClientOrderID = "ord-11"
	if err := manager.CheckOrder(ctx, req); err == nil {
		t.Fatal("11th order should have been throttled, but it was not")
	} else {
		var breach *BreachError
		if !errors.As(err, &breach) {
			t.Fatalf("expected BreachError, got %v", err)
		}
		if breach.Type != BreachTypeRateLimit {
			t.Fatalf("expected breach type %s, got %s", BreachTypeRateLimit, breach.Type)
		}
	}
}

func TestManager_CheckOrder_PositionAndNotionalLimits(t *testing.T) {
	limits := Limits{
		MaxPositionSize:     decimal.NewFromInt(10),
		MaxNotionalValue:    decimal.NewFromInt(50),
		OrderThrottle:       100,
		OrderBurst:          5,
		MaxConcurrentOrders: 0,
	}
	manager := NewManager(limits)

	price := "10"
	firstOrder := &schema.OrderRequest{
		Provider:      "fake",
		Symbol:        "ETH-USDT",
		Side:          schema.TradeSideBuy,
		OrderType:     schema.OrderTypeLimit,
		Price:         &price,
		Quantity:      "5",
		ClientOrderID: "ord-long",
	}

	if err := manager.CheckOrder(context.Background(), firstOrder); err != nil {
		t.Fatalf("expected initial order to pass: %v", err)
	}

	manager.HandleExecution(firstOrder.Symbol, schema.ExecReportPayload{
		ClientOrderID:  firstOrder.ClientOrderID,
		Side:           schema.TradeSideBuy,
		FilledQuantity: firstOrder.Quantity,
		AvgFillPrice:   *firstOrder.Price,
		State:          schema.ExecReportStateFILLED,
	})

	overPosition := &schema.OrderRequest{
		Provider:      "fake",
		Symbol:        "ETH-USDT",
		Side:          schema.TradeSideBuy,
		OrderType:     schema.OrderTypeLimit,
		Price:         &price,
		Quantity:      "6",
		ClientOrderID: "ord-limit",
	}
	if err := manager.CheckOrder(context.Background(), overPosition); err == nil {
		t.Fatal("expected position limit breach")
	} else {
		var breach *BreachError
		if !errors.As(err, &breach) {
			t.Fatalf("expected BreachError, got %v", err)
		}
		if breach.Type != BreachTypePositionLimit {
			t.Fatalf("expected breach type %s, got %s", BreachTypePositionLimit, breach.Type)
		}
	}

	highPrice := "40"
	overNotional := &schema.OrderRequest{
		Provider:      "fake",
		Symbol:        "ETH-USDT",
		Side:          schema.TradeSideBuy,
		OrderType:     schema.OrderTypeLimit,
		Price:         &highPrice,
		Quantity:      "1",
		ClientOrderID: "ord-notional",
	}
	if err := manager.CheckOrder(context.Background(), overNotional); err == nil {
		t.Fatal("expected notional limit breach")
	} else {
		var breach *BreachError
		if !errors.As(err, &breach) {
			t.Fatalf("expected BreachError, got %v", err)
		}
		if breach.Type != BreachTypeNotionalLimit {
			t.Fatalf("expected breach type %s, got %s", BreachTypeNotionalLimit, breach.Type)
		}
	}
}

func TestManager_KillSwitchEngagesAfterBreaches(t *testing.T) {
	limits := Limits{
		MaxPositionSize:   decimal.NewFromInt(5),
		MaxNotionalValue:  decimal.NewFromInt(100),
		OrderThrottle:     100,
		OrderBurst:        2,
		KillSwitchEnabled: true,
		MaxRiskBreaches:   2,
	}
	manager := NewManager(limits)

	price := "10"
	req := &schema.OrderRequest{
		Provider:      "fake",
		Symbol:        "SOL-USDT",
		Side:          schema.TradeSideBuy,
		OrderType:     schema.OrderTypeLimit,
		Price:         &price,
		Quantity:      "500",
		ClientOrderID: "ord-risk",
	}

	for i := 0; i < 2; i++ {
		req.ClientOrderID = fmt.Sprintf("ord-risk-%d", i)
		if err := manager.CheckOrder(context.Background(), req); err == nil {
			t.Fatalf("expected risk breach on attempt %d", i+1)
		} else {
			var breach *BreachError
			if !errors.As(err, &breach) {
				t.Fatalf("expected BreachError, got %v", err)
			}
		}
	}

	req.ClientOrderID = "ord-after-breach"
	if err := manager.CheckOrder(context.Background(), req); !errors.Is(err, ErrKillSwitchEngaged) && !errors.Is(err, ErrCircuitBreakerOpen) {
		t.Fatalf("expected kill switch error, got %v", err)
	}
}
