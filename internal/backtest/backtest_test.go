package backtest

import (
	"context"
	"testing"

	"github.com/coachpo/meltica/internal/lambda/strategies"
)

func TestEngine_Run(t *testing.T) {
	strategy := &strategies.NoOp{}
	feeder := &mockDataFeeder{}
	exchange := NewSimulatedExchange(strategy)

	engine := NewEngine(feeder, exchange, strategy)

	// Run the backtest for a short duration.
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.Run(ctx)
	}()

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("engine run failed: %v", err)
	}
	if analytics := engine.Analytics(); analytics.TotalOrders != 0 {
		t.Fatalf("expected no orders recorded, got %d", analytics.TotalOrders)
	}
}
