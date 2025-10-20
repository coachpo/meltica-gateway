package config

import (
	"context"
	"os"
	"path/filepath"
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
    option: value
dispatcher:
  routes:
    TICKER:
      wsTopics:
        - ticker.BTCUSDT
      restFns:
        - name: price
          endpoint: https://example.com
          interval: 1s
          parser: priceParser
      filters:
        - field: symbol
          op: eq
          value: BTC-USDT
eventbus:
  bufferSize: 128
  fanoutWorkers: 4
pools:
  eventSize: 100
  orderRequestSize: 50
apiServer:
  addr: ":9999"
telemetry:
  otlpEndpoint: http://localhost:4318
  serviceName: test-service
  otlpInsecure: true
  enableMetrics: false
manifest: config/runtime.yaml
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
	if got := ex["option"]; got != "value" {
		t.Fatalf("expected exchange option value, got %v", got)
	}

	if cfg.Eventbus.BufferSize != 128 {
		t.Fatalf("expected buffer size 128, got %d", cfg.Eventbus.BufferSize)
	}
	if cfg.Eventbus.FanoutWorkers != 4 {
		t.Fatalf("expected fanout workers 4, got %d", cfg.Eventbus.FanoutWorkers)
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

	route, ok := cfg.Dispatcher.Routes["TICKER"]
	if !ok {
		t.Fatalf("expected dispatcher route TICKER")
	}
	if len(route.RestFns) != 1 {
		t.Fatalf("expected one rest fn, got %d", len(route.RestFns))
	}
	if route.RestFns[0].Interval.String() != "1s" {
		t.Fatalf("expected interval 1s, got %v", route.RestFns[0].Interval)
	}
}
