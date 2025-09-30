package config

import (
	"testing"
	"time"
)

func TestDefaultConfigProvidesBinanceSettings(t *testing.T) {
	cfg := Default()
	if cfg.Environment != EnvProd {
		t.Fatalf("expected default environment prod, got %s", cfg.Environment)
	}
	binance, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatalf("expected binance exchange settings")
	}
	if binance.REST[BinanceRESTSurfaceSpot] == "" || binance.Websocket.PublicURL == "" {
		t.Fatalf("expected default REST and websocket URLs")
	}

	defaultBinance, ok := DefaultExchangeSettings(ExchangeBinance)
	if !ok {
		t.Fatalf("expected default exchange settings to resolve")
	}
	defaultBinance.REST[BinanceRESTSurfaceSpot] = "mutated"
	if cfgDefault, _ := DefaultExchangeSettings(ExchangeBinance); cfgDefault.REST[BinanceRESTSurfaceSpot] == "mutated" {
		t.Fatalf("expected DefaultExchangeSettings to return clone")
	}
}

func TestFromEnvOverridesValues(t *testing.T) {
	t.Setenv("MELTICA_ENV", "STAGING")
	t.Setenv("BINANCE_SPOT_BASE_URL", "https://spot.test")
	t.Setenv("BINANCE_LINEAR_BASE_URL", "https://lin.test")
	t.Setenv("BINANCE_INVERSE_BASE_URL", "https://inv.test")
	t.Setenv("BINANCE_WS_PUBLIC_URL", "wss://pub.test")
	t.Setenv("BINANCE_WS_PRIVATE_URL", "wss://priv.test")
	t.Setenv("BINANCE_HTTP_TIMEOUT", "15s")
	t.Setenv("BINANCE_WS_HANDSHAKE_TIMEOUT", "20s")
	t.Setenv("BINANCE_API_KEY", "key")
	t.Setenv("BINANCE_API_SECRET", "secret")

	cfg := FromEnv()
	if cfg.Environment != EnvStaging {
		t.Fatalf("expected staging environment, got %s", cfg.Environment)
	}
	bin, ok := cfg.Exchange(ExchangeBinance)
	if !ok {
		t.Fatalf("expected binance exchange settings")
	}
	if bin.REST[BinanceRESTSurfaceSpot] != "https://spot.test" {
		t.Fatalf("expected env override spot URL, got %s", bin.REST[BinanceRESTSurfaceSpot])
	}
	if bin.Websocket.PrivateURL != "wss://priv.test" {
		t.Fatalf("expected websocket private override")
	}
	if bin.HTTPTimeout != 15*time.Second || bin.HandshakeTimeout != 20*time.Second {
		t.Fatalf("expected timeout overrides, got %s/%s", bin.HTTPTimeout, bin.HandshakeTimeout)
	}
	if bin.Credentials.APIKey != "key" || bin.Credentials.APISecret != "secret" {
		t.Fatalf("expected credential overrides")
	}
}

func TestApplyOptionsCloneAndMutate(t *testing.T) {
	base := Default()
	timeout := 25 * time.Second
	interval := 2 * time.Minute

	applied := Apply(base,
		WithEnvironment(EnvDev),
		WithExchangeRESTEndpoint("BINANCE", BinanceRESTSurfaceSpot, "https://override"),
		WithExchangeWebsocketEndpoints("binance", "wss://pub", "wss://priv", 5*time.Second),
		WithExchangeHTTPTimeout("binance", timeout),
		WithExchangeCredentials("binance", " KEY ", " SECRET "),
		WithBinanceRESTEndpoints("https://spot2", "https://lin2", "https://inv2"),
		WithBinanceWebsocketEndpoints("wss://pub2", "wss://priv2", 10*time.Second),
		WithBinanceHTTPTimeout(30*time.Second),
		WithBinanceAPI("key2", "secret2"),
		WithBinanceSymbolRefreshInterval(interval),
		WithExchangeRESTEndpoint("binance", "", ""),
		WithExchangeWebsocketEndpoints("binance", "", "", 0),
		WithExchangeCredentials("binance", " ", " "),
		mutateExchangeOption("", nil),
	)

	if applied.Environment != EnvDev {
		t.Fatalf("expected environment override, got %s", applied.Environment)
	}
	if base.Environment == EnvDev {
		t.Fatalf("expected base environment to remain unchanged")
	}

	bin, ok := applied.Exchange(ExchangeBinance)
	if !ok {
		t.Fatalf("expected binance exchange settings")
	}
	if bin.REST[BinanceRESTSurfaceSpot] != "https://spot2" {
		t.Fatalf("expected binance REST overrides to apply, got %s", bin.REST[BinanceRESTSurfaceSpot])
	}
	if bin.Websocket.PublicURL != "wss://pub2" || bin.HandshakeTimeout != 10*time.Second {
		t.Fatalf("expected websocket overrides to apply, got %s / %s", bin.Websocket.PublicURL, bin.HandshakeTimeout)
	}
	if bin.HTTPTimeout != 30*time.Second {
		t.Fatalf("expected HTTP timeout override, got %s", bin.HTTPTimeout)
	}
	if bin.Credentials.APIKey != "key2" || bin.Credentials.APISecret != "secret2" {
		t.Fatalf("expected credential override, got %v", bin.Credentials)
	}
	if bin.SymbolRefreshInterval != interval {
		t.Fatalf("expected symbol refresh interval override, got %s", bin.SymbolRefreshInterval)
	}

	// Ensure clone semantics: mutating result should not touch base.
	bin.REST["custom"] = "value"
	baseBin, _ := base.Exchange(ExchangeBinance)
	if _, exists := baseBin.REST["custom"]; exists {
		t.Fatalf("expected base exchanges to remain unchanged")
	}

	// Exchange lookup should normalize names.
	if _, ok := applied.Exchange(Exchange("BINANCE")); !ok {
		t.Fatalf("expected normalized exchange lookup to succeed")
	}

	// Missing exchange should return false.
	if _, ok := applied.Exchange(Exchange("missing")); ok {
		t.Fatalf("expected missing exchange lookup to fail")
	}
}
