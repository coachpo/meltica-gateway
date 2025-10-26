package backtest

import (
	"context"
	"sync"

	"github.com/coachpo/meltica/internal/lambda"
	"github.com/coachpo/meltica/internal/schema"
)

// SimulatedExchange is an interface for a simulated exchange that can process orders.
type SimulatedExchange interface {
	SubmitOrder(ctx context.Context, req schema.OrderRequest) error
}

// simulatedExchange is an implementation of the SimulatedExchange interface.
type simulatedExchange struct {
	mu         sync.RWMutex
	orderBooks map[string]*OrderBook
	lambda     lambda.TradingStrategy
}

// NewSimulatedExchange creates a new simulated exchange.
func NewSimulatedExchange(lambda lambda.TradingStrategy) SimulatedExchange {
	return &simulatedExchange{
		orderBooks: make(map[string]*OrderBook),
		lambda:     lambda,
	}
}

// SubmitOrder submits an order to the simulated exchange.
func (se *simulatedExchange) SubmitOrder(ctx context.Context, req schema.OrderRequest) error {
	se.mu.Lock()
	defer se.mu.Unlock()

	ob, ok := se.orderBooks[req.Symbol]
	if !ok {
		ob = NewOrderBook()
		se.orderBooks[req.Symbol] = ob
	}

	trades := ob.Match(&req)

	for _, trade := range trades {
		// In a real implementation, we would create a proper execution report.
		// For now, we'll just call the OnOrderFilled method on the strategy.
		se.lambda.OnOrderFilled(ctx, &schema.Event{}, schema.ExecReportPayload{
			ClientOrderID: req.ClientOrderID,
			FilledQuantity: trade.Quantity,
			AvgFillPrice:   trade.Price,
		})
	}

	return nil
}
