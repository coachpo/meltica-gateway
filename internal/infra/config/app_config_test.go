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

func TestLoadOrDefaultReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.yaml")
	cfg, loaded, err := LoadOrDefault(context.Background(), path)
	if err != nil {
		t.Fatalf("LoadOrDefault returned error: %v", err)
	}
	if loaded {
		t.Fatalf("expected loaded=false for missing configuration file")
	}
	def := DefaultAppConfig()
	if cfg.Environment != def.Environment {
		t.Fatalf("expected default environment %s, got %s", def.Environment, cfg.Environment)
	}
	if !reflect.DeepEqual(cfg.Runtime, def.Runtime) {
		t.Fatalf("expected default runtime config, got %#v", cfg.Runtime)
	}
	if cfg.Providers != nil && len(cfg.Providers) != 0 {
		t.Fatalf("expected providers to be empty, got %#v", cfg.Providers)
	}
	if len(cfg.LambdaManifest.Lambdas) != 0 {
		t.Fatalf("expected no lambdas in default manifest")
	}
}

func TestLoadDuplicateProviderName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	yaml := `
environment: dev
runtime:
  eventbus:
    buffer_size: 1
    fanout_workers: 1
  pools:
    event:
      size: 1
    order_request:
      size: 1
  risk:
    max_position_size: "1"
    max_notional_value: "1"
    notional_currency: USD
    order_throttle: 1
    order_burst: 1
    max_concurrent_orders: 0
    price_band_percent: 0
    allowed_order_types: []
    kill_switch_enabled: false
    max_risk_breaches: 0
    circuit_breaker:
      enabled: false
      threshold: 0
      cooldown: "1s"
  api_server:
    addr: ":1"
  telemetry:
    otlp_endpoint: ""
    service_name: test
    otlp_insecure: true
    enable_metrics: false
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
meta:
  name: Meltica
  version: v1
runtime:
  eventbus:
    buffer_size: 128
    fanout_workers: 4
  pools:
    event:
      size: 100
      wait_queue_size: 110
    order_request:
      size: 50
      wait_queue_size: 60
  risk:
    max_position_size: "10"
    max_notional_value: "1000"
    notional_currency: "USD"
    order_throttle: 5
    order_burst: 1
    max_concurrent_orders: 0
    price_band_percent: 0
    allowed_order_types: []
    kill_switch_enabled: false
    max_risk_breaches: 0
    circuit_breaker:
      enabled: false
      threshold: 0
      cooldown: "1s"
  api_server:
    addr: ":9999"
  telemetry:
    otlp_endpoint: http://localhost:4318
    service_name: test-service
    otlp_insecure: true
    enable_metrics: false
providers:
  BinanceSpot:
    adapter:
      identifier: binance
      config:
        option: value
lambda_manifest:
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

	if cfg.Runtime.Eventbus.BufferSize != 128 {
		t.Fatalf("expected buffer size 128, got %d", cfg.Runtime.Eventbus.BufferSize)
	}
	if workers := cfg.Runtime.Eventbus.FanoutWorkerCount(); workers != 4 {
		t.Fatalf("expected fanout workers 4, got %d", workers)
	}

	if cfg.Runtime.APIServer.Addr != ":9999" {
		t.Fatalf("expected api server addr :9999, got %s", cfg.Runtime.APIServer.Addr)
	}

	if cfg.Runtime.Telemetry.ServiceName != "test-service" {
		t.Fatalf("expected telemetry service name test-service, got %s", cfg.Runtime.Telemetry.ServiceName)
	}
	if cfg.Runtime.Telemetry.EnableMetrics {
		t.Fatalf("expected telemetry metrics disabled")
	}

	if cfg.Runtime.Pools.Event.Size != 100 {
		t.Fatalf("expected pool event size 100, got %d", cfg.Runtime.Pools.Event.Size)
	}
	if cfg.Runtime.Pools.Event.WaitQueueSize != 110 {
		t.Fatalf("expected pool event queue size 110, got %d", cfg.Runtime.Pools.Event.WaitQueueSize)
	}
	if cfg.Runtime.Pools.OrderRequest.Size != 50 {
		t.Fatalf("expected pool order request size 50, got %d", cfg.Runtime.Pools.OrderRequest.Size)
	}
	if cfg.Runtime.Pools.OrderRequest.WaitQueueSize != 60 {
		t.Fatalf("expected pool order request queue size 60, got %d", cfg.Runtime.Pools.OrderRequest.WaitQueueSize)
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

	if cfg.Runtime.Risk.OrderBurst != 1 {
		t.Fatalf("expected default order burst 1, got %d", cfg.Runtime.Risk.OrderBurst)
	}
	if cfg.Runtime.Risk.MaxConcurrentOrders != 0 {
		t.Fatalf("expected default max concurrent orders 0, got %d", cfg.Runtime.Risk.MaxConcurrentOrders)
	}
}

func TestFanoutWorkersAuto(t *testing.T) {
	cfg := loadConfigWithFanout(t, "    fanout_workers: auto\n")
	expected := runtime.NumCPU()
	if expected <= 0 {
		expected = 4
	}
	if workers := cfg.Runtime.Eventbus.FanoutWorkerCount(); workers != expected {
		t.Fatalf("expected fanout workers %d, got %d", expected, workers)
	}
}

func TestFanoutWorkersDefaultString(t *testing.T) {
	cfg := loadConfigWithFanout(t, "    fanout_workers: default\n")
	if workers := cfg.Runtime.Eventbus.FanoutWorkerCount(); workers != 4 {
		t.Fatalf("expected default fanout workers 4, got %d", workers)
	}
}

func TestFanoutWorkersMissing(t *testing.T) {
	cfg := loadConfigWithFanout(t, "")
	if workers := cfg.Runtime.Eventbus.FanoutWorkerCount(); workers != 4 {
		t.Fatalf("expected missing fanout workers to default to 4, got %d", workers)
	}
}

func loadConfigWithFanout(t *testing.T, fanoutLine string) AppConfig {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	yaml := fmt.Sprintf(`
environment: dev
runtime:
  eventbus:
    buffer_size: 128
%s  pools:
    event:
      size: 100
    order_request:
      size: 50
  risk:
    max_position_size: "10"
    max_notional_value: "1000"
    notional_currency: "USD"
    order_throttle: 5
    order_burst: 1
    max_concurrent_orders: 0
    price_band_percent: 0
    allowed_order_types: []
    kill_switch_enabled: false
    max_risk_breaches: 0
    circuit_breaker:
      enabled: false
      threshold: 0
      cooldown: "1s"
  api_server:
    addr: ":9999"
  telemetry:
    otlp_endpoint: http://localhost:4318
    service_name: test-service
    otlp_insecure: true
    enable_metrics: false
providers:
  binance-spot:
    adapter:
      identifier: binance
      config:
        option: value
lambda_manifest:
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
