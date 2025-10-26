package backtest

import (
	"context"
	"testing"

	"github.com/coachpo/meltica/internal/lambda/strategies"
	"github.com/coachpo/meltica/internal/schema"
)

func TestSimulatedExchange_SubmitOrder(t *testing.T) {
	strategy := &strategies.NoOp{}
	se := NewSimulatedExchange(strategy)

	price := "100"
	order := schema.OrderRequest{
		Symbol: "BTC-USDT",
		Side:   schema.TradeSideBuy,
		Price:  &price,
	}

	if err := se.SubmitOrder(context.Background(), order); err != nil {
		t.Fatalf("SubmitOrder failed: %v", err)
	}
}
