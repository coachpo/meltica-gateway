package runtime

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coachpo/meltica/internal/infra/config"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	cfg := config.AppConfig{
		Strategies: config.StrategiesConfig{
			Directory: filepath.Join("..", "..", "..", "..", "strategies"),
		},
	}
	manager, err := NewManager(cfg, nil, nil, nil, log.New(io.Discard, "", 0), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager
}

func TestManagerRefreshJavaScriptStrategies(t *testing.T) {
	dir := t.TempDir()
	initialSource := `module.exports = {
	  metadata: {
	    name: "alpha",
	    displayName: "Alpha",
	    description: "Alpha v1",
	    config: [],
	    events: ["Trade"]
	  },
	  create: function () {
	    return {};
	  }
};
`
	if err := os.WriteFile(filepath.Join(dir, "alpha.js"), []byte(initialSource), 0o600); err != nil {
		t.Fatalf("write strategy: %v", err)
	}

	cfg := config.AppConfig{
		Environment: config.EnvDev,
		Eventbus:    config.EventbusConfig{BufferSize: 1},
		Pools: config.PoolConfig{
			Event:        config.ObjectPoolConfig{Size: 1, WaitQueueSize: 1},
			OrderRequest: config.ObjectPoolConfig{Size: 1, WaitQueueSize: 1},
		},
		Risk: config.RiskConfig{
			MaxPositionSize:  "1",
			MaxNotionalValue: "1",
			NotionalCurrency: "USD",
			OrderThrottle:    1,
			OrderBurst:       1,
		},
		Telemetry: config.TelemetryConfig{ServiceName: "test"},
		APIServer: config.APIServerConfig{Addr: ":0"},
		Strategies: config.StrategiesConfig{
			Directory: dir,
		},
	}

	mgr, err := NewManager(cfg, nil, nil, nil, log.New(io.Discard, "", 0), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	meta, ok := mgr.StrategyDetail("alpha")
	if !ok {
		t.Fatalf("expected alpha strategy to be registered")
	}
	if meta.DisplayName != "Alpha" {
		t.Fatalf("unexpected display name %q", meta.DisplayName)
	}

	updatedSource := strings.ReplaceAll(initialSource, "Alpha v1", "Alpha v2")
	if err := mgr.UpsertStrategy("alpha.js", []byte(updatedSource)); err != nil {
		t.Fatalf("UpsertStrategy: %v", err)
	}

	meta, ok = mgr.StrategyDetail("alpha")
	if !ok {
		t.Fatalf("alpha strategy missing after upsert")
	}
	if meta.Description != "Alpha v1" {
		t.Fatalf("expected stale metadata before refresh, got %q", meta.Description)
	}

	if err := mgr.RefreshJavaScriptStrategies(context.Background()); err != nil {
		t.Fatalf("RefreshJavaScriptStrategies: %v", err)
	}

	meta, ok = mgr.StrategyDetail("alpha")
	if !ok {
		t.Fatalf("alpha strategy missing after refresh")
	}
	if meta.Description != "Alpha v2" {
		t.Fatalf("expected refreshed metadata, got %q", meta.Description)
	}

	source, err := mgr.StrategySource("alpha.js")
	if err != nil {
		t.Fatalf("StrategySource: %v", err)
	}
	if !strings.Contains(string(source), "Alpha v2") {
		t.Fatalf("expected updated source, got %q", string(source))
	}
}

func baseLambdaSpec() config.LambdaSpec {
	return config.LambdaSpec{
		ID:        "alpha",
		Strategy:  config.LambdaStrategySpec{Identifier: "logging", Config: map[string]any{"logger_prefix": "[test]"}},
		Providers: []string{"okx-spot"},
		ProviderSymbols: map[string]config.ProviderSymbols{
			"okx-spot": {Symbols: []string{"BTC-USDT"}},
		},
	}
}

func TestManagerUpdateImmutableFields(t *testing.T) {
	t.Run("strategy immutable", func(t *testing.T) {
		mgr := newTestManager(t)
		spec := baseLambdaSpec()
		if err := mgr.StartFromManifest(config.LambdaManifest{Lambdas: []config.LambdaSpec{spec}}); err != nil {
			t.Fatalf("register spec: %v", err)
		}

		spec.Strategy.Identifier = "delay"
		if err := mgr.Update(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "strategy is immutable") {
			t.Fatalf("expected strategy immutability error, got %v", err)
		}
	})

	t.Run("providers immutable", func(t *testing.T) {
		mgr := newTestManager(t)
		spec := baseLambdaSpec()
		if err := mgr.StartFromManifest(config.LambdaManifest{Lambdas: []config.LambdaSpec{spec}}); err != nil {
			t.Fatalf("register spec: %v", err)
		}

		spec.ProviderSymbols = nil
		spec.Providers = []string{"binance-spot"}
		if err := mgr.Update(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "providers are immutable") {
			t.Fatalf("expected providers immutability error, got %v", err)
		}
	})

	t.Run("scope immutable", func(t *testing.T) {
		mgr := newTestManager(t)
		spec := baseLambdaSpec()
		if err := mgr.StartFromManifest(config.LambdaManifest{Lambdas: []config.LambdaSpec{spec}}); err != nil {
			t.Fatalf("register spec: %v", err)
		}

		spec.ProviderSymbols = map[string]config.ProviderSymbols{
			"okx-spot": {Symbols: []string{"ETH-USDT"}},
		}
		if err := mgr.Update(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "scope assignments are immutable") {
			t.Fatalf("expected scope immutability error, got %v", err)
		}
	})

	t.Run("config mutable", func(t *testing.T) {
		mgr := newTestManager(t)
		spec := baseLambdaSpec()
		if err := mgr.StartFromManifest(config.LambdaManifest{Lambdas: []config.LambdaSpec{spec}}); err != nil {
			t.Fatalf("register spec: %v", err)
		}

		spec.Strategy.Config = map[string]any{"logger_prefix": "[updated]"}
		if err := mgr.Update(context.Background(), spec); err != nil {
			t.Fatalf("expected config update to succeed, got %v", err)
		}

		snapshot, ok := mgr.Instance(spec.ID)
		if !ok {
			t.Fatalf("instance %s not found after update", spec.ID)
		}
		prefix, ok := snapshot.Strategy.Config["logger_prefix"].(string)
		if !ok || prefix != "[updated]" {
			t.Fatalf("expected updated logger_prefix, got %v", snapshot.Strategy.Config["logger_prefix"])
		}
	})
}
