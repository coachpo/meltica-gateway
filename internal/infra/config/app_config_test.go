package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(context.Background(), filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatalf("expected error when config file missing")
	}
}

func TestLoadDuplicateProviderName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	yaml := `
environment: dev
providers:
  BinanceSpot: {}
  binanceSpot: {}
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	_, err := Load(context.Background(), path)
	if err == nil {
		t.Fatalf("expected error when duplicate provider names supplied")
	}
	if !strings.Contains(err.Error(), `duplicate provider name "binanceSpot"`) {
		t.Fatalf("expected duplicate provider name error, got %v", err)
	}
}

func TestLoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	yaml := `
environment: DEV
providers:
  BinanceSpot:
    adapter:
      identifier: binance
      config:
        option: value
eventbus:
  bufferSize: 128
  fanoutWorkers: 4
pools:
  event:
    size: 100
    waitQueueSize: 110
  orderRequest:
    size: 50
    waitQueueSize: 60
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
      scope:
        binance-spot:
          symbols:
            - BTC-USDT
      strategy:
        identifier: delay
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

	ex, ok := cfg.Providers[Provider("binanceSpot")]
	if !ok {
		t.Fatalf("expected binance adapter config")
	}
	rawAdapter := ex["adapter"]
	adapterCfg, ok := rawAdapter.(map[string]any)
	if !ok {
		t.Fatalf("expected adapter config map, got %T", rawAdapter)
	}
	if id := adapterCfg["identifier"]; id != "binance" {
		t.Fatalf("expected identifier binance, got %v", id)
	}
	rawNested := adapterCfg["config"]
	nestedCfg, ok := rawNested.(map[string]any)
	if !ok {
		t.Fatalf("expected nested config map, got %T", rawNested)
	}
	if got := nestedCfg["option"]; got != "value" {
		t.Fatalf("expected adapter option value, got %v", got)
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

	if cfg.Pools.Event.Size != 100 {
		t.Fatalf("expected pool event size 100, got %d", cfg.Pools.Event.Size)
	}
	if cfg.Pools.Event.WaitQueueSize != 110 {
		t.Fatalf("expected pool event queue size 110, got %d", cfg.Pools.Event.WaitQueueSize)
	}
	if cfg.Pools.OrderRequest.Size != 50 {
		t.Fatalf("expected pool order request size 50, got %d", cfg.Pools.OrderRequest.Size)
	}
	if cfg.Pools.OrderRequest.WaitQueueSize != 60 {
		t.Fatalf("expected pool order request queue size 60, got %d", cfg.Pools.OrderRequest.WaitQueueSize)
	}

	if len(cfg.LambdaManifest.Lambdas) != 1 {
		t.Fatalf("expected 1 lambda, got %d", len(cfg.LambdaManifest.Lambdas))
	}
	manifest := cfg.LambdaManifest.Lambdas[0]
	if manifest.ID != "test-lambda" {
		t.Fatalf("unexpected lambda id %s", manifest.ID)
	}
	if manifest.Strategy.Identifier != "delay" {
		t.Fatalf("unexpected strategy identifier %s", manifest.Strategy.Identifier)
	}
	if manifest.AutoStart {
		t.Fatalf("expected test-lambda autostart disabled")
	}
	if len(manifest.Providers) != 1 || manifest.Providers[0] != "binance-spot" {
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

func TestLoadAppliesRiskDefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	yaml := `
environment: dev
eventbus:
  bufferSize: 256
  fanoutWorkers: 4
pools:
  event:
    size: 128
    waitQueueSize: 128
  orderRequest:
    size: 64
    waitQueueSize: 64
apiServer:
  addr: ":8080"
telemetry:
  otlpEndpoint: ""
  serviceName: default-service
  otlpInsecure: false
  enableMetrics: true
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !reflect.DeepEqual(cfg.Risk, defaultRiskConfig()) {
		t.Fatalf("expected risk config defaults: %#v", cfg.Risk)
	}
}

func loadConfigWithFanout(t *testing.T, fanoutLine string) AppConfig {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	yaml := fmt.Sprintf(`
environment: dev
providers:
  binance-spot:
    adapter:
      identifier: binance
      config:
        option: value
eventbus:
  bufferSize: 128
%spools:
  event:
    size: 100
  orderRequest:
    size: 50
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
      scope:
        binance-spot:
          symbols:
            - BTC-USDT
      strategy:
        identifier: delay
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
