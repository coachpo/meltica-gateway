package runtime

import (
	"context"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/coachpo/meltica/internal/infra/config"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(config.AppConfig{}, nil, nil, nil, log.New(io.Discard, "", 0), nil)
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
