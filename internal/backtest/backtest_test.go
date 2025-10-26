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
	go func() {
		if err := engine.Run(ctx); err != nil {
			// Backtest finished.
		}
	}()

	cancel()
}
