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
func (a *orderStrategyAdapter) SubmitOrder(ctx context.Context, side schema.TradeSide, quantity string, price *float64) error {
	var priceStr *string
	if price != nil {
		formatted := fmt.Sprintf("%f", *price)
		priceStr = &formatted
	}
	return a.base.SubmitOrder(ctx, side, quantity, priceStr)
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
			GridLevels:  *gridLevels,
			GridSpacing: *gridSpacing,
			OrderSize:   *gridOrderSize,
		}
		baseLambda := lambda.NewBaseLambda("backtest", lambda.Config{}, nil, nil, nil, gridStrategy, nil)
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

	fmt.Println("Backtest finished successfully")
}
