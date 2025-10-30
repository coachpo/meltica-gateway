package fake

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/app/provider"
	"github.com/coachpo/meltica/internal/infra/pool"
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
			PriceModel: marketModelOptions{
				Drift:            0,
				Volatility:       0,
				ShockProbability: 0,
				ShockMagnitude:   0,
			},
			TradeModel: tradeModelOptions{
				MinQuantity: 0,
				MaxQuantity: 0,
			},
			OrderBook: orderBookOptions{
				Levels:           0,
				MaxMutationWidth: 0,
			},
			VenueBehavior: venueBehaviorOptions{
				LatencyMin:       0,
				LatencyMax:       0,
				TransientError:   0,
				DisconnectChance: 0,
				DisconnectFor:    0,
			},
			KlineInterval:         0,
			BalanceUpdateInterval: 0,
		}

		if raw, ok := cfg["provider_name"].(string); ok {
			if trimmed := strings.TrimSpace(raw); trimmed != "" {
				opts.Name = trimmed
			}
		}
		if name, ok := cfg["name"].(string); ok {
			if trimmed := strings.TrimSpace(name); trimmed != "" && strings.TrimSpace(opts.Name) == "" {
				opts.Name = trimmed
			}
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
		if refresh, ok := durationFromConfig(cfg, "instrument_refresh_interval"); ok {
			opts.InstrumentRefreshInterval = refresh
		}
		if balance, ok := durationFromConfig(cfg, "balance_update_interval"); ok {
			opts.BalanceUpdateInterval = balance
		}
		if kline, ok := durationFromConfig(cfg, "kline_interval"); ok {
			opts.KlineInterval = kline
		}

		if priceModelCfg, ok := mapFromConfig(cfg, "price_model"); ok {
			if drift, ok := floatFromConfig(priceModelCfg, "drift"); ok {
				opts.PriceModel.Drift = drift
			}
			if vol, ok := floatFromConfig(priceModelCfg, "volatility"); ok {
				opts.PriceModel.Volatility = vol
			}
			if prob, ok := floatFromConfig(priceModelCfg, "shock_probability"); ok {
				opts.PriceModel.ShockProbability = prob
			}
			if mag, ok := floatFromConfig(priceModelCfg, "shock_magnitude"); ok {
				opts.PriceModel.ShockMagnitude = mag
			}
		}

		if tradeModelCfg, ok := mapFromConfig(cfg, "trade_model"); ok {
			if minQty, ok := floatFromConfig(tradeModelCfg, "min_quantity"); ok {
				opts.TradeModel.MinQuantity = minQty
			}
			if maxQty, ok := floatFromConfig(tradeModelCfg, "max_quantity"); ok {
				opts.TradeModel.MaxQuantity = maxQty
			}
		}

		if bookCfg, ok := mapFromConfig(cfg, "order_book"); ok {
			if levels, ok := intFromConfig(bookCfg, "levels"); ok {
				opts.OrderBook.Levels = levels
			}
			if width, ok := intFromConfig(bookCfg, "max_mutation_width"); ok {
				opts.OrderBook.MaxMutationWidth = width
			}
		}

		if venueCfg, ok := mapFromConfig(cfg, "venue_behavior"); ok {
			if minLatency, ok := durationFromConfig(venueCfg, "latency_min"); ok {
				opts.VenueBehavior.LatencyMin = minLatency
			}
			if maxLatency, ok := durationFromConfig(venueCfg, "latency_max"); ok {
				opts.VenueBehavior.LatencyMax = maxLatency
			}
			if errRate, ok := floatFromConfig(venueCfg, "transient_error"); ok {
				opts.VenueBehavior.TransientError = errRate
			}
			if discChance, ok := floatFromConfig(venueCfg, "disconnect_chance"); ok {
				opts.VenueBehavior.DisconnectChance = discChance
			}
			if discFor, ok := durationFromConfig(venueCfg, "disconnect_for"); ok {
				opts.VenueBehavior.DisconnectFor = discFor
			}
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
	case int64:
		return time.Duration(value) * time.Second, true
	case float64:
		return time.Duration(value) * time.Second, true
	default:
		return 0, false
	}
}

func floatFromConfig(cfg map[string]any, key string) (float64, bool) {
	v, ok := cfg[key]
	if !ok {
		return 0, false
	}
	switch value := v.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint64:
		return float64(value), true
	case string:
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func intFromConfig(cfg map[string]any, key string) (int, bool) {
	v, ok := cfg[key]
	if !ok {
		return 0, false
	}
	switch value := v.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case uint64:
		limit := uint64(^uint(0) >> 1)
		if value > limit {
			return 0, false
		}
		return int(int64(value)), true
	case float64:
		return int(value), true
	case string:
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func mapFromConfig(cfg map[string]any, key string) (map[string]any, bool) {
	v, ok := cfg[key]
	if !ok {
		return nil, false
	}
	result, ok := v.(map[string]any)
	return result, ok
}
