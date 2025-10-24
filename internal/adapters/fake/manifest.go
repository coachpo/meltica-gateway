package fake

import (
	"context"
	"fmt"
	"time"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/provider"
)

// RegisterFactory registers the fake provider with the global provider registry.
func RegisterFactory(reg *provider.Registry) {
	reg.Register("fake", func(ctx context.Context, pools *pool.PoolManager, cfg map[string]any) (provider.Instance, error) {
		if pools == nil {
			return nil, fmt.Errorf("fake provider requires pool manager")
		}

		opts := Options{
			Name:                      "",
			TickerInterval:            0,
			TradeInterval:             0,
			BookSnapshotInterval:      0,
			Pools:                     pools,
			Instruments:               nil,
			InstrumentRefreshInterval: 0,
			InstrumentRefresh:         nil,
		}
		if name, ok := cfg["name"].(string); ok {
			opts.Name = name
		}
		if ticker, ok := durationFromConfig(cfg, "ticker_interval"); ok {
			opts.TickerInterval = ticker
		}
		if trade, ok := durationFromConfig(cfg, "trade_interval"); ok {
			opts.TradeInterval = trade
		}
		if book, ok := durationFromConfig(cfg, "book_snapshot_interval"); ok {
			opts.BookSnapshotInterval = book
		}

		inst := NewProvider(opts)
		if err := inst.Start(ctx); err != nil {
			return nil, fmt.Errorf("start fake provider: %w", err)
		}
		return inst, nil
	})
}

func durationFromConfig(cfg map[string]any, key string) (time.Duration, bool) {
	v, ok := cfg[key]
	if !ok {
		return 0, false
	}
	switch value := v.(type) {
	case string:
		d, err := time.ParseDuration(value)
		if err != nil {
			return 0, false
		}
		return d, true
	case int:
		return time.Duration(value) * time.Second, true
	case float64:
		return time.Duration(value) * time.Second, true
	default:
		return 0, false
	}
}
