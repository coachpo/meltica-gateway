// Package main provides a CLI for running strategy backtests against historical data.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/coachpo/meltica/internal/backtest"
	"github.com/coachpo/meltica/internal/lambda"
	"github.com/coachpo/meltica/internal/lambda/strategies"
	"github.com/coachpo/meltica/internal/schema"
)

type orderStrategyAdapter struct {
	base *lambda.BaseLambda
}

func (a *orderStrategyAdapter) Logger() *log.Logger   { return a.base.Logger() }
func (a *orderStrategyAdapter) GetLastPrice() float64 { return a.base.GetLastPrice() }
func (a *orderStrategyAdapter) IsTradingActive() bool { return a.base.IsTradingActive() }
func (a *orderStrategyAdapter) Providers() []string   { return a.base.Providers() }
func (a *orderStrategyAdapter) SelectProvider(seed uint64) (string, error) {
	provider, err := a.base.SelectProvider(seed)
	if err != nil {
		return "", fmt.Errorf("select provider: %w", err)
	}
	return provider, nil
}
func (a *orderStrategyAdapter) SubmitOrder(ctx context.Context, provider string, side schema.TradeSide, quantity string, price *float64) error {
	var priceStr *string
	if price != nil {
		formatted := fmt.Sprintf("%f", *price)
		priceStr = &formatted
	}
	if err := a.base.SubmitOrder(ctx, provider, side, quantity, priceStr); err != nil {
		return fmt.Errorf("submit order: %w", err)
	}
	return nil
}

func main() {
	dataPath := flag.String("data", "", "Path to the historical data file (CSV)")
	strategyName := flag.String("strategy", "noop", "Name of the strategy to backtest")

	// Grid strategy parameters.
	gridLevels := flag.Int("grid.levels", 5, "Number of grid levels")
	gridSpacing := flag.Float64("grid.spacing", 0.5, "Grid spacing")
	gridOrderSize := flag.String("grid.orderSize", "1", "Order size for each grid level")

	flag.Parse()

	if *dataPath == "" {
		log.Fatal("data path is required")
	}

	feeder, err := backtest.NewCSVFeeder(*dataPath)
	if err != nil {
		log.Fatalf("create csv feeder: %v", err)
	}

	var strategy lambda.TradingStrategy
	switch *strategyName {
	case "noop":
		strategy = &strategies.NoOp{}
	case "grid":
		gridStrategy := &strategies.Grid{
			Lambda:      nil,
			GridLevels:  *gridLevels,
			GridSpacing: *gridSpacing,
			OrderSize:   *gridOrderSize,
			BasePrice:   0,
		}
		baseLambda := lambda.NewBaseLambda("backtest", lambda.Config{Symbol: "", Providers: []string{"backtest"}}, nil, nil, nil, gridStrategy, nil)
		gridStrategy.Lambda = &orderStrategyAdapter{base: baseLambda}
		strategy = gridStrategy
	default:
		log.Fatalf("unknown strategy: %s", *strategyName)
	}

	exchange := backtest.NewSimulatedExchange(strategy)

	engine := backtest.NewEngine(feeder, exchange, strategy)

	if err := engine.Run(context.Background()); err != nil {
		log.Fatalf("backtest failed: %v", err)
	}

	analytics := engine.Analytics()
	fmt.Printf("Backtest finished successfully\n")
	fmt.Printf("Orders: %d, Filled: %d, Volume: %s\n", analytics.TotalOrders, analytics.FilledOrders, analytics.TotalVolume.String())
	fmt.Printf("Gross PnL: %s, Fees: %s, Net PnL: %s, Max Drawdown: %s\n",
		analytics.GrossPnL.String(), analytics.Fees.String(), analytics.NetPnL.String(), analytics.MaxDrawdown.String())
}
