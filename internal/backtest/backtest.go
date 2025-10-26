package backtest

import (
	"context"
	"io"

	"github.com/coachpo/meltica/internal/lambda"
	"github.com/coachpo/meltica/internal/schema"
)

// Engine orchestrates a backtest.
type Engine struct {
	feeder   DataFeeder
	exchange SimulatedExchange
	strategy lambda.TradingStrategy
}

// NewEngine creates a new backtest engine.
func NewEngine(feeder DataFeeder, exchange SimulatedExchange, strategy lambda.TradingStrategy) *Engine {
	return &Engine{
		feeder:   feeder,
		exchange: exchange,
		strategy: strategy,
	}
}

// Run starts the backtest.
func (e *Engine) Run(ctx context.Context) error {
	// This is a simplified implementation. A real backtest engine would
	// need to handle event sequencing, time synchronization, and more.
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			event, err := e.feeder.Next()
			if err != nil {
				if err == io.EOF {
					return nil // Backtest finished successfully.
				}
				return err
			}

			// Feed the event to the strategy.
			// In a real implementation, we would need to handle different event types.
			switch payload := event.Payload.(type) {
			case schema.TradePayload:
				// For simplicity, we are not handling the price conversion here.
				e.strategy.OnTrade(ctx, event, payload, 0)
			}

			// The simulated exchange would process orders and generate executions.
			// This part is omitted for brevity.
		}
	}
}
