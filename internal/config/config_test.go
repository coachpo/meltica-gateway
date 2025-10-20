package config

import (
	"os"
	"testing"
	"time"
)

// Example unit test for internal package
func TestDefaultEnvironment(t *testing.T) {
	cfg := Default()
	
	if cfg.Environment != EnvProd {
		t.Errorf("expected prod environment, got %s", cfg.Environment)
	}
}

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Environment != EnvProd {
		t.Errorf("expected default environment to be prod, got %s", cfg.Environment)
	}

	binance, ok := cfg.Exchanges[ExchangeBinance]
	if !ok {
		t.Fatal("expected Binance exchange settings")
	}

	if binance.REST[BinanceRESTSurfaceSpot] == "" {
		t.Error("expected Binance spot URL")
	}
	if binance.HTTPTimeout == 0 {
		t.Error("expected non-zero HTTP timeout")
	}
}

func TestFromEnv(t *testing.T) {
	// Save and restore environment
	oldEnv := os.Getenv("MELTICA_ENV")
	defer os.Setenv("MELTICA_ENV", oldEnv)

	os.Setenv("MELTICA_ENV", "dev")
	cfg := FromEnv()

	if cfg.Environment != EnvDev {
		t.Errorf("expected env=dev, got %s", cfg.Environment)
	}
}

func TestFromEnvBinanceURLs(t *testing.T) {
	// Save and restore environment
	oldEnvs := map[string]string{
		"BINANCE_SPOT_BASE_URL":    os.Getenv("BINANCE_SPOT_BASE_URL"),
		"BINANCE_LINEAR_BASE_URL":  os.Getenv("BINANCE_LINEAR_BASE_URL"),
		"BINANCE_INVERSE_BASE_URL": os.Getenv("BINANCE_INVERSE_BASE_URL"),
	}
	defer func() {
		for k, v := range oldEnvs {
			os.Setenv(k, v)
		}
	}()

	os.Setenv("BINANCE_SPOT_BASE_URL", "https://spot.test.com")
	os.Setenv("BINANCE_LINEAR_BASE_URL", "https://linear.test.com")
	os.Setenv("BINANCE_INVERSE_BASE_URL", "https://inverse.test.com")
	
	cfg := FromEnv()
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.REST[BinanceRESTSurfaceSpot] != "https://spot.test.com" {
		t.Error("expected spot URL from env")
	}
	if binance.REST[BinanceRESTSurfaceLinear] != "https://linear.test.com" {
		t.Error("expected linear URL from env")
	}
	if binance.REST[BinanceRESTSurfaceInverse] != "https://inverse.test.com" {
		t.Error("expected inverse URL from env")
	}
}

func TestFromEnvBinanceWebsockets(t *testing.T) {
	oldEnvs := map[string]string{
		"BINANCE_WS_PUBLIC_URL":  os.Getenv("BINANCE_WS_PUBLIC_URL"),
		"BINANCE_WS_PRIVATE_URL": os.Getenv("BINANCE_WS_PRIVATE_URL"),
	}
	defer func() {
		for k, v := range oldEnvs {
			os.Setenv(k, v)
		}
	}()

	os.Setenv("BINANCE_WS_PUBLIC_URL", "wss://ws-public.test.com")
	os.Setenv("BINANCE_WS_PRIVATE_URL", "wss://ws-private.test.com")
	
	cfg := FromEnv()
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.Websocket.PublicURL != "wss://ws-public.test.com" {
		t.Error("expected public websocket URL from env")
	}
	if binance.Websocket.PrivateURL != "wss://ws-private.test.com" {
		t.Error("expected private websocket URL from env")
	}
}

func TestFromEnvBinanceTimeouts(t *testing.T) {
	oldEnvs := map[string]string{
		"BINANCE_HTTP_TIMEOUT":        os.Getenv("BINANCE_HTTP_TIMEOUT"),
		"BINANCE_WS_HANDSHAKE_TIMEOUT": os.Getenv("BINANCE_WS_HANDSHAKE_TIMEOUT"),
	}
	defer func() {
		for k, v := range oldEnvs {
			os.Setenv(k, v)
		}
	}()

	os.Setenv("BINANCE_HTTP_TIMEOUT", "30s")
	os.Setenv("BINANCE_WS_HANDSHAKE_TIMEOUT", "15s")
	
	cfg := FromEnv()
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.HTTPTimeout != 30*time.Second {
		t.Errorf("expected 30s HTTP timeout, got %v", binance.HTTPTimeout)
	}
	if binance.HandshakeTimeout != 15*time.Second {
		t.Errorf("expected 15s handshake timeout, got %v", binance.HandshakeTimeout)
	}
}

func TestFromEnvBinanceCredentials(t *testing.T) {
	oldEnvs := map[string]string{
		"BINANCE_API_KEY":    os.Getenv("BINANCE_API_KEY"),
		"BINANCE_API_SECRET": os.Getenv("BINANCE_API_SECRET"),
	}
	defer func() {
		for k, v := range oldEnvs {
			os.Setenv(k, v)
		}
	}()

	os.Setenv("BINANCE_API_KEY", "test-key-123")
	os.Setenv("BINANCE_API_SECRET", "test-secret-456")
	
	cfg := FromEnv()
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.Credentials.APIKey != "test-key-123" {
		t.Error("expected API key from env")
	}
	if binance.Credentials.APISecret != "test-secret-456" {
		t.Error("expected API secret from env")
	}
}

func TestApply(t *testing.T) {
	base := Default()
	
	modified := Apply(base, WithEnvironment(EnvStaging))

	if modified.Environment != EnvStaging {
		t.Errorf("expected staging environment, got %s", modified.Environment)
	}
	
	// Verify base wasn't modified
	if base.Environment != EnvProd {
		t.Error("expected base config to remain unchanged")
	}
}

func TestSettingsExchange(t *testing.T) {
	cfg := Default()
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected to find Binance exchange")
	}
	
	if binance.REST[BinanceRESTSurfaceSpot] == "" {
		t.Error("expected spot URL")
	}
}

func TestWithBinanceAPI(t *testing.T) {
	cfg := Apply(Default(), WithBinanceAPI("test-key", "test-secret"))
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.Credentials.APIKey != "test-key" {
		t.Errorf("expected API key test-key, got %s", binance.Credentials.APIKey)
	}
	if binance.Credentials.APISecret != "test-secret" {
		t.Errorf("expected API secret test-secret, got %s", binance.Credentials.APISecret)
	}
}

func TestDefaultExchangeSettings(t *testing.T) {
	settings, ok := DefaultExchangeSettings(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange settings")
	}
	
	if settings.HTTPTimeout == 0 {
		t.Error("expected non-zero HTTP timeout")
	}
	if settings.HandshakeTimeout == 0 {
		t.Error("expected non-zero handshake timeout")
	}
}

func TestWithExchangeRESTEndpoint(t *testing.T) {
	cfg := Apply(Default(), WithExchangeRESTEndpoint(string(ExchangeBinance), "test", "https://test.example.com"))
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.REST["test"] != "https://test.example.com" {
		t.Errorf("expected REST endpoint to be set")
	}
}

func TestWithExchangeWebsocketEndpoints(t *testing.T) {
	cfg := Apply(Default(), WithExchangeWebsocketEndpoints(string(ExchangeBinance), "wss://public.test", "wss://private.test", 5*time.Second))
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.Websocket.PublicURL != "wss://public.test" {
		t.Errorf("expected public URL to be set")
	}
	if binance.Websocket.PrivateURL != "wss://private.test" {
		t.Errorf("expected private URL to be set")
	}
}

func TestWithExchangeHTTPTimeout(t *testing.T) {
	timeout := 30 * time.Second
	cfg := Apply(Default(), WithExchangeHTTPTimeout(string(ExchangeBinance), timeout))
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.HTTPTimeout != timeout {
		t.Errorf("expected HTTP timeout %v, got %v", timeout, binance.HTTPTimeout)
	}
}

func TestWithBinanceRESTEndpoints(t *testing.T) {
	cfg := Apply(Default(), WithBinanceRESTEndpoints("https://spot.test", "https://linear.test", "https://inverse.test"))
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.REST[BinanceRESTSurfaceSpot] != "https://spot.test" {
		t.Error("expected spot URL to be updated")
	}
	if binance.REST[BinanceRESTSurfaceLinear] != "https://linear.test" {
		t.Error("expected linear URL to be updated")
	}
	if binance.REST[BinanceRESTSurfaceInverse] != "https://inverse.test" {
		t.Error("expected inverse URL to be updated")
	}
}

func TestWithBinanceWebsocketEndpoints(t *testing.T) {
	cfg := Apply(Default(), WithBinanceWebsocketEndpoints("wss://binance.public", "wss://binance.private", 10*time.Second))
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.Websocket.PublicURL != "wss://binance.public" {
		t.Error("expected public websocket URL to be updated")
	}
	if binance.Websocket.PrivateURL != "wss://binance.private" {
		t.Error("expected private websocket URL to be updated")
	}
}

func TestWithBinanceHTTPTimeout(t *testing.T) {
	timeout := 20 * time.Second
	cfg := Apply(Default(), WithBinanceHTTPTimeout(timeout))
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.HTTPTimeout != timeout {
		t.Errorf("expected HTTP timeout %v, got %v", timeout, binance.HTTPTimeout)
	}
}

func TestWithBinanceSymbolRefreshInterval(t *testing.T) {
	interval := 5 * time.Minute
	cfg := Apply(Default(), WithBinanceSymbolRefreshInterval(interval))
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.SymbolRefreshInterval != interval {
		t.Errorf("expected symbol refresh interval %v, got %v", interval, binance.SymbolRefreshInterval)
	}
}

func TestWithExchangeCredentials(t *testing.T) {
	cfg := Apply(Default(), WithExchangeCredentials(string(ExchangeBinance), "key1", "secret1"))
	
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatal("expected Binance exchange")
	}
	
	if binance.Credentials.APIKey != "key1" {
		t.Errorf("expected API key key1, got %s", binance.Credentials.APIKey)
	}
	if binance.Credentials.APISecret != "secret1" {
		t.Errorf("expected API secret secret1, got %s", binance.Credentials.APISecret)
	}
}
