package binance

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/provider"
)

// RegisterFactory installs the Binance provider factory into the registry.
func RegisterFactory(reg *provider.Registry) {
	reg.Register("binance", func(ctx context.Context, pools *pool.PoolManager, cfg map[string]any) (provider.Instance, error) {
		if pools == nil {
			return nil, fmt.Errorf("binance provider requires pool manager")
		}

		opts := Options{
			Pools: pools,
		}

		if raw, ok := stringFromConfig(cfg, "name"); ok {
			opts.Name = raw
		}
		if raw, ok := stringFromConfig(cfg, "venue"); ok {
			opts.Venue = strings.ToUpper(strings.TrimSpace(raw))
		}
		if raw, ok := stringFromConfig(cfg, "api_base_url"); ok {
			opts.APIBaseURL = raw
		}
		if raw, ok := stringFromConfig(cfg, "websocket_url"); ok {
			opts.WebsocketBaseURL = raw
		}
		if raw, ok := stringSliceFromConfig(cfg, "symbols"); ok {
			opts.Symbols = raw
		}
		if depth, ok := intFromConfig(cfg, "snapshot_depth"); ok {
			opts.SnapshotDepth = depth
		}
		if timeout, ok := durationFromConfig(cfg, "http_timeout"); ok {
			opts.HTTPTimeout = timeout
		}
		if refresh, ok := durationFromConfig(cfg, "instrument_refresh_interval"); ok {
			opts.InstrumentRefreshInterval = refresh
		}

		provider := NewProvider(opts)
		if err := provider.Start(ctx); err != nil {
			return nil, fmt.Errorf("start binance provider: %w", err)
		}
		return provider, nil
	})
}

func stringFromConfig(cfg map[string]any, key string) (string, bool) {
	if cfg == nil {
		return "", false
	}
	raw, ok := cfg[key]
	if !ok {
		return "", false
	}
	if value, ok := raw.(string); ok {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	}
	return "", false
}

func stringSliceFromConfig(cfg map[string]any, key string) ([]string, bool) {
	raw, ok := cfg[key]
	if !ok {
		return nil, false
	}
	switch v := raw.(type) {
	case []string:
		return v, true
	case []any:
		out := make([]string, 0, len(v))
		for _, entry := range v {
			out = append(out, fmt.Sprint(entry))
		}
		return out, true
	case string:
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	default:
		return nil, false
	}
}

func intFromConfig(cfg map[string]any, key string) (int, bool) {
	raw, ok := cfg[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, false
		}
		var parsed int
		if _, err := fmt.Sscanf(trimmed, "%d", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func durationFromConfig(cfg map[string]any, key string) (time.Duration, bool) {
	raw, ok := cfg[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, false
		}
		d, err := time.ParseDuration(trimmed)
		if err != nil {
			return 0, false
		}
		return d, true
	case int:
		return time.Duration(v) * time.Second, true
	case int64:
		return time.Duration(v) * time.Second, true
	case float64:
		return time.Duration(v) * time.Second, true
	}
	return 0, false
}
