package config

import (
	"os"
	"testing"
	"time"
)

func TestDefaultEnvironment(t *testing.T) {
	cfg := Default()
	if cfg.Environment != EnvProd {
		t.Errorf("expected default environment %s, got %s", EnvProd, cfg.Environment)
	}
	if cfg.Exchanges == nil {
		t.Fatal("expected exchanges map to be initialised")
	}
	if len(cfg.Exchanges) != 0 {
		t.Fatalf("expected no default exchanges, got %d", len(cfg.Exchanges))
	}
}

func TestFromEnvOverridesEnvironment(t *testing.T) {
	original := os.Getenv("MELTICA_ENV")
	t.Cleanup(func() {
		_ = os.Setenv("MELTICA_ENV", original)
	})

	if err := os.Setenv("MELTICA_ENV", "dev"); err != nil {
		t.Fatalf("set env: %v", err)
	}

	cfg := FromEnv()
	if cfg.Environment != EnvDev {
		t.Errorf("expected environment %s, got %s", EnvDev, cfg.Environment)
	}
}

func TestApplyWithExchangeOptions(t *testing.T) {
	base := Default()
	exchangeName := "example"

	cfg := Apply(base,
		WithExchangeRESTEndpoint(exchangeName, "spot", "https://spot.example.com"),
		WithExchangeWebsocketEndpoints(exchangeName, "wss://pub.example.com", "wss://priv.example.com", 15*time.Second),
		WithExchangeHTTPTimeout(exchangeName, 10*time.Second),
		WithExchangeCredentials(exchangeName, "key", "secret"),
	)

	settings, ok := cfg.Exchange(Exchange(exchangeName))
	if !ok {
		t.Fatalf("expected exchange settings for %s", exchangeName)
	}

	if got := settings.REST["spot"]; got != "https://spot.example.com" {
		t.Errorf("unexpected REST endpoint: %s", got)
	}
	if settings.Websocket.PublicURL != "wss://pub.example.com" {
		t.Errorf("unexpected websocket public URL: %s", settings.Websocket.PublicURL)
	}
	if settings.Websocket.PrivateURL != "wss://priv.example.com" {
		t.Errorf("unexpected websocket private URL: %s", settings.Websocket.PrivateURL)
	}
	if settings.HandshakeTimeout != 15*time.Second {
		t.Errorf("unexpected handshake timeout: %v", settings.HandshakeTimeout)
	}
	if settings.HTTPTimeout != 10*time.Second {
		t.Errorf("unexpected HTTP timeout: %v", settings.HTTPTimeout)
	}
	if settings.Credentials.APIKey != "key" || settings.Credentials.APISecret != "secret" {
		t.Errorf("unexpected credentials: %+v", settings.Credentials)
	}

	// base should remain unchanged
	if _, ok := base.Exchanges[Exchange(exchangeName)]; ok {
		t.Fatal("expected base exchanges to remain unchanged")
	}
}

func TestExchangeMissing(t *testing.T) {
	cfg := Default()
	if _, ok := cfg.Exchange("unknown"); ok {
		t.Fatal("expected missing exchange to return ok=false")
	}
}

func TestDefaultExchangeSettingsMissing(t *testing.T) {
	if _, ok := DefaultExchangeSettings("unknown"); ok {
		t.Fatal("expected no default exchange settings for unknown exchange")
	}
}

func TestMutateExchangeOptionNormalisesName(t *testing.T) {
	cfg := Apply(Default(), WithExchangeRESTEndpoint(" Example ", "spot", "https://example.com"))

	if _, ok := cfg.Exchanges[Exchange("example")]; !ok {
		t.Fatalf("expected exchange key to be normalised")
	}
}
