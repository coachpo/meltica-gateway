// Package okx wires the OKX provider into the adapter registry.
package okx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/app/provider"
	"github.com/coachpo/meltica/internal/infra/pool"
)

// RegisterFactory installs the OKX provider factory into the registry.
func RegisterFactory(reg *provider.Registry) {
	if reg == nil {
		return
	}
	reg.RegisterWithMetadata(okxPublicMetadata.identifier, func(ctx context.Context, pools *pool.PoolManager, cfg map[string]any) (provider.Instance, error) {
		if pools == nil {
			return nil, fmt.Errorf("okx provider requires pool manager")
		}

		var opts Options
		opts.Pools = pools

		if alias, ok := stringFromConfig(cfg, "provider_name"); ok {
			opts.Config.Name = alias
		} else if raw, ok := stringFromConfig(cfg, "name"); ok {
			opts.Config.Name = raw
		}

		userCfg := cfg
		if nested, ok := mapFromConfig(cfg, "config"); ok {
			userCfg = nested
		}

		if depth, ok := intFromConfig(userCfg, "snapshot_depth"); ok {
			opts.Config.SnapshotDepth = depth
		}
		if timeout, ok := durationFromConfig(userCfg, "http_timeout"); ok {
			opts.Config.HTTPTimeout = timeout
		}
		if refresh, ok := durationFromConfig(userCfg, "instrument_refresh_interval"); ok {
			opts.Config.InstrumentRefresh = refresh
		}

		provider := NewProvider(opts)
		if err := provider.Start(ctx); err != nil {
			return nil, fmt.Errorf("start okx provider: %w", err)
		}
		return provider, nil
	}, okxAdapterMetadata)
}

func stringFromConfig(cfg map[string]any, key string) (string, bool) {
	if cfg == nil {
		return "", false
	}
	raw, ok := cfg[key]
	if !ok {
		return "", false
	}
	value, ok := raw.(string)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func mapFromConfig(cfg map[string]any, key string) (map[string]any, bool) {
	raw, ok := cfg[key]
	if !ok {
		return nil, false
	}
	out, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	return out, true
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
		dur, err := time.ParseDuration(trimmed)
		if err != nil {
			return 0, false
		}
		return dur, true
	case int:
		return time.Duration(v) * time.Second, true
	case int64:
		return time.Duration(v) * time.Second, true
	case float64:
		return time.Duration(v) * time.Second, true
	}
	return 0, false
}
