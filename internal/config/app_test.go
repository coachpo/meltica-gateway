package config

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	ctx := context.Background()

	// Load without YAML file (should use defaults)
	cfg, err := Load(ctx, "/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify defaults
	if cfg.Environment != EnvProd {
		t.Errorf("expected environment %s, got %s", EnvProd, cfg.Environment)
	}

	if len(cfg.Exchanges) == 0 {
		t.Error("expected default exchanges")
	}

	binance, ok := cfg.Exchanges[ExchangeBinance]
	if !ok {
		t.Fatal("expected binance exchange in defaults")
	}

	if binance.Websocket.PublicURL == "" {
		t.Error("expected binance websocket URL in defaults")
	}

	if cfg.Eventbus.BufferSize != 1024 {
		t.Errorf("expected eventbus buffer size 1024, got %d", cfg.Eventbus.BufferSize)
	}

	if cfg.Telemetry.ServiceName != "meltica-gateway" {
		t.Errorf("expected service name meltica-gateway, got %s", cfg.Telemetry.ServiceName)
	}

	if cfg.ManifestPath != "config/runtime.yaml" {
		t.Errorf("expected default manifest path config/runtime.yaml, got %s", cfg.ManifestPath)
	}
}

func TestLoad_WithEnv(t *testing.T) {
	ctx := context.Background()

	// Set environment variables
	os.Setenv("MELTICA_ENV", "dev")
	os.Setenv("BINANCE_API_KEY", "test_key_123")
	os.Setenv("BINANCE_API_SECRET", "test_secret_456")
	os.Setenv("OTEL_SERVICE_NAME", "test-service")
	os.Setenv("MELTICA_MANIFEST", "/tmp/runtime.yaml")
	defer func() {
		os.Unsetenv("MELTICA_ENV")
		os.Unsetenv("BINANCE_API_KEY")
		os.Unsetenv("BINANCE_API_SECRET")
		os.Unsetenv("OTEL_SERVICE_NAME")
		os.Unsetenv("MELTICA_MANIFEST")
	}()

	cfg, err := Load(ctx, "/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify env overrides
	if cfg.Environment != EnvDev {
		t.Errorf("expected environment dev, got %s", cfg.Environment)
	}

	binance, ok := cfg.Exchanges[ExchangeBinance]
	if !ok {
		t.Fatal("expected binance exchange")
	}

	if binance.Credentials.APIKey != "test_key_123" {
		t.Errorf("expected API key from env, got %s", binance.Credentials.APIKey)
	}

	if binance.Credentials.APISecret != "test_secret_456" {
		t.Errorf("expected API secret from env, got %s", binance.Credentials.APISecret)
	}

	if cfg.Telemetry.ServiceName != "test-service" {
		t.Errorf("expected service name from env, got %s", cfg.Telemetry.ServiceName)
	}

	if cfg.ManifestPath != "/tmp/runtime.yaml" {
		t.Errorf("expected manifest path from env, got %s", cfg.ManifestPath)
	}
}

func TestDefaultAppConfig(t *testing.T) {
	cfg := defaultAppConfig()

	if cfg.Environment != EnvProd {
		t.Errorf("expected prod environment, got %s", cfg.Environment)
	}

	if len(cfg.Exchanges) == 0 {
		t.Error("expected default exchanges")
	}

	if cfg.Adapters.Binance.BookRefreshInterval != 1*time.Minute {
		t.Errorf("expected book refresh interval 1m, got %v", cfg.Adapters.Binance.BookRefreshInterval)
	}

	if cfg.Dispatcher.Routes == nil {
		t.Error("expected initialized routes map")
	}

	if cfg.Telemetry.EnableMetrics != true {
		t.Error("expected metrics enabled by default")
	}

	if cfg.ManifestPath != "config/runtime.yaml" {
		t.Errorf("expected default manifest path, got %s", cfg.ManifestPath)
	}
}
