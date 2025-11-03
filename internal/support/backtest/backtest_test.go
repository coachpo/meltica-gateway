package backtest

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"

	"github.com/coachpo/meltica/internal/app/lambda/core"
	"github.com/coachpo/meltica/internal/app/lambda/js"
)

func loadTestStrategy(t *testing.T, name string) core.TradingStrategy {
	t.Helper()

	dir := filepath.Join("..", "..", "..", "strategies")
	loader, err := js.NewLoader(dir)
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}
	if err := loader.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh loader: %v", err)
	}
	module, err := loader.Get(name)
	if err != nil {
		t.Fatalf("get module %q: %v", name, err)
	}

	jsStrategy, err := js.NewStrategy(module, map[string]any{}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("instantiate strategy %q: %v", name, err)
	}

	base := core.NewBaseLambda("test", core.Config{Providers: []string{"test"}, DryRun: true}, nil, nil, nil, jsStrategy, nil)
	jsStrategy.Attach(base)
	base.EnableTrading(true)
	return jsStrategy
}

func TestEngine_Run(t *testing.T) {
	strategy := loadTestStrategy(t, "noop")
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
