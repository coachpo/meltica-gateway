// Package main provides a CLI for running strategy backtests against historical data.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/coachpo/meltica/internal/app/lambda/core"
	"github.com/coachpo/meltica/internal/app/lambda/js"
	"github.com/coachpo/meltica/internal/support/backtest"
)

func main() {
	dataPath := flag.String("data", "", "Path to the historical data file (CSV)")
	strategyName := flag.String("strategy", "noop", "Strategy selector to backtest (name, name:tag, or name@hash)")
	strategiesDir := flag.String("strategies.dir", "strategies", "Directory containing JavaScript strategies")

	// Grid strategy parameters.
	gridLevels := flag.Int("grid.levels", 5, "Number of grid levels")
	gridSpacing := flag.Float64("grid.spacing", 0.5, "Grid spacing (percent)")
	gridOrderSize := flag.String("grid.orderSize", "1", "Order size for each grid level")

	flag.Parse()

	if strings.TrimSpace(*dataPath) == "" {
		log.Fatal("data path is required")
	}

	feeder, err := backtest.NewCSVFeeder(*dataPath)
	if err != nil {
		log.Fatalf("create csv feeder: %v", err)
	}

	selector := strings.TrimSpace(*strategyName)
	if selector == "" {
		log.Fatal("strategy selector is required")
	}

	absStrategiesPath, err := filepath.Abs(*strategiesDir)
	if err != nil {
		log.Fatalf("resolve strategies dir: %v", err)
	}

	loader, err := js.NewLoader(absStrategiesPath)
	if err != nil {
		log.Fatalf("create strategy loader: %v", err)
	}
	if err := loader.Refresh(context.Background()); err != nil {
		log.Fatalf("load strategies: %v", err)
	}

	resolution, err := loader.ResolveReference(selector)
	if err != nil {
		log.Fatalf("resolve strategy %q: %v", selector, err)
	}
	strategyID := resolution.Name

	config := map[string]any{}
	switch strategyID {
	case "grid":
		config = map[string]any{
			"grid_levels":  *gridLevels,
			"grid_spacing": *gridSpacing,
			"order_size":   *gridOrderSize,
			"base_price":   0.0,
			"dry_run":      true,
		}
	case "noop":
		// no additional configuration
	default:
		config = map[string]any{}
	}

	strategyLogger := log.New(os.Stdout, "[strategy] ", log.LstdFlags)
	jsStrategy, err := js.NewStrategy(resolution.Module, config, strategyLogger)
	if err != nil {
		log.Fatalf("instantiate strategy %q: %v", strategyID, err)
	}

	base := core.NewBaseLambda(
		"backtest",
		core.Config{Providers: []string{"backtest"}, ProviderSymbols: nil, DryRun: true},
		nil,
		nil,
		nil,
		jsStrategy,
		nil,
	)
	jsStrategy.Attach(base)
	base.EnableTrading(true)

	strategy := core.TradingStrategy(jsStrategy)

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
