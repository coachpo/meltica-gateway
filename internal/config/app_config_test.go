package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(context.Background(), filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatalf("expected error when config file missing")
	}
}

func TestLoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	yaml := `
environment: DEV
exchanges:
  Fake:
    exchange:
      name: fake
      option: value
eventbus:
  bufferSize: 128
  fanoutWorkers: 4
pools:
  eventSize: 100
  orderRequestSize: 50
risk:
  maxPositionSize: "10"
  maxNotionalValue: "1000"
  notionalCurrency: "USD"
  orderThrottle: 5

apiServer:
  addr: ":9999"
telemetry:
  otlpEndpoint: http://localhost:4318
  serviceName: test-service
  otlpInsecure: true
  enableMetrics: false
lambdaManifest:
  lambdas:
    - id: test-lambda
      providers: [fake]
      symbol: BTC-USDT
      strategy: delay
      auto_start: false
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Environment != EnvDev {
		t.Fatalf("expected environment %s, got %s", EnvDev, cfg.Environment)
	}

	ex, ok := cfg.Exchanges[Exchange("fake")]
	if !ok {
		t.Fatalf("expected fake exchange config")
	}
	rawExchange := ex["exchange"]
	exchangeCfg, ok := rawExchange.(map[string]any)
	if !ok {
		t.Fatalf("expected exchange config map, got %T", rawExchange)
	}
	if got := exchangeCfg["option"]; got != "value" {
		t.Fatalf("expected exchange option value, got %v", got)
	}

	if cfg.Eventbus.BufferSize != 128 {
		t.Fatalf("expected buffer size 128, got %d", cfg.Eventbus.BufferSize)
	}
	if workers := cfg.Eventbus.FanoutWorkerCount(); workers != 4 {
		t.Fatalf("expected fanout workers 4, got %d", workers)
	}

	if cfg.APIServer.Addr != ":9999" {
		t.Fatalf("expected api server addr :9999, got %s", cfg.APIServer.Addr)
	}

	if cfg.Telemetry.ServiceName != "test-service" {
		t.Fatalf("expected telemetry service name test-service, got %s", cfg.Telemetry.ServiceName)
	}
	if cfg.Telemetry.EnableMetrics {
		t.Fatalf("expected telemetry metrics disabled")
	}

	if cfg.Pools.EventSize != 100 {
		t.Fatalf("expected pool event size 100, got %d", cfg.Pools.EventSize)
	}
	if cfg.Pools.OrderRequestSize != 50 {
		t.Fatalf("expected pool order request size 50, got %d", cfg.Pools.OrderRequestSize)
	}

	if len(cfg.LambdaManifest.Lambdas) != 1 {
		t.Fatalf("expected 1 lambda, got %d", len(cfg.LambdaManifest.Lambdas))
	}
	manifest := cfg.LambdaManifest.Lambdas[0]
	if manifest.ID != "test-lambda" {
		t.Fatalf("unexpected lambda id %s", manifest.ID)
	}
	if manifest.AutoStart {
		t.Fatalf("expected test-lambda autostart disabled")
	}
	if len(manifest.Providers) != 1 || manifest.Providers[0] != "fake" {
		t.Fatalf("unexpected providers: %+v", manifest.Providers)
	}

	if cfg.Risk.OrderBurst != 1 {
		t.Fatalf("expected default order burst 1, got %d", cfg.Risk.OrderBurst)
	}
	if cfg.Risk.MaxConcurrentOrders != 0 {
		t.Fatalf("expected default max concurrent orders 0, got %d", cfg.Risk.MaxConcurrentOrders)
	}
}

func TestFanoutWorkersAuto(t *testing.T) {
	cfg := loadConfigWithFanout(t, "  fanoutWorkers: auto\n")
	expected := runtime.NumCPU()
	if expected <= 0 {
		expected = 4
	}
	if workers := cfg.Eventbus.FanoutWorkerCount(); workers != expected {
		t.Fatalf("expected fanout workers %d, got %d", expected, workers)
	}
}

func TestFanoutWorkersDefaultString(t *testing.T) {
	cfg := loadConfigWithFanout(t, "  fanoutWorkers: default\n")
	if workers := cfg.Eventbus.FanoutWorkerCount(); workers != 4 {
		t.Fatalf("expected default fanout workers 4, got %d", workers)
	}
}

func TestFanoutWorkersMissing(t *testing.T) {
	cfg := loadConfigWithFanout(t, "")
	if workers := cfg.Eventbus.FanoutWorkerCount(); workers != 4 {
		t.Fatalf("expected missing fanout workers to default to 4, got %d", workers)
	}
}

func loadConfigWithFanout(t *testing.T, fanoutLine string) AppConfig {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	yaml := fmt.Sprintf(`
environment: dev
exchanges:
  fake:
    exchange:
      name: fake
      option: value
eventbus:
  bufferSize: 128
%spools:
  eventSize: 100
  orderRequestSize: 50
risk:
  maxPositionSize: "10"
  maxNotionalValue: "1000"
  notionalCurrency: "USD"
  orderThrottle: 5

apiServer:
  addr: ":9999"
telemetry:
  otlpEndpoint: http://localhost:4318
  serviceName: test-service
  otlpInsecure: true
  enableMetrics: false
lambdaManifest:
  lambdas:
    - id: test-lambda
      providers: [fake]
      symbol: BTC-USDT
      strategy: delay
      auto_start: false
`, fanoutLine)

	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	return cfg
}
