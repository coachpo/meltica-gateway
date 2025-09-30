package unit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/config"
)

func TestLoadStreamingConfigV2(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "streaming_v2.yaml")
	yaml := `providers:
- name: binance
  ws_endpoint: ws://binance
  rest_endpoint: https://binance
  symbols: ["BTC-USDT"]
  book_refresh_interval: 1s
- name: coinbase
  ws_endpoint: ws://coinbase
  rest_endpoint: https://coinbase
  symbols: ["BTC-USDT"]
  book_refresh_interval: 1s
orchestrator:
  merge_rules:
    - merge_key: trade
      providers: ["binance", "coinbase"]
      window_duration: 1s
      max_events: 5
      partial_policy: suppress
dispatcher:
  stream_ordering:
    lateness_tolerance: 1s
    flush_interval: 1s
    max_buffer_size: 16
  backpressure:
    token_rate_per_stream: 10
    token_burst: 20
  coalescable_types: ["TRADE"]
consumers:
  - name: gateway
    consumer_id: consumer-1
    trading_switch: enabled
    subscriptions:
      - symbol: BTC-USDT
        providers: ["binance", "coinbase"]
        event_types: ["TRADE"]
        merged: true
telemetry:
  otlpEndpoint: http://otel
  serviceName: meltica-gateway
`
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	cfg, err := config.LoadStreamingConfigV2(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, cfg.Providers, 2)
	require.Equal(t, "binance", cfg.Providers[0].Name)
	require.Equal(t, time.Second, cfg.Providers[0].BookRefreshInterval)
	require.Len(t, cfg.Consumers, 1)
	require.Equal(t, "gateway", cfg.Consumers[0].Name)
}

func TestLoadStreamingConfigFallbackExample(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	root := filepath.Dir(filepath.Dir(wd))
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { require.NoError(t, os.Chdir(wd)) })

	cfg, err := config.LoadStreamingConfig(context.Background(), filepath.Join(t.TempDir(), "missing.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Dispatcher.Routes)
}
