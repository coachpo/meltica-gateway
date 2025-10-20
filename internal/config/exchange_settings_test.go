package config

import (
	"testing"
	"time"
)

func TestNewExchangeSettingsInitialisesFields(t *testing.T) {
	cfg := NewExchangeSettings()

	if cfg.REST == nil {
		t.Fatal("expected REST map to be initialised")
	}
	if len(cfg.REST) != 0 {
		t.Fatalf("expected empty REST map, got %d entries", len(cfg.REST))
	}
	if cfg.Websocket.PublicURL != "" || cfg.Websocket.PrivateURL != "" {
		t.Errorf("expected websocket URLs to start empty, got %+v", cfg.Websocket)
	}
	if cfg.Credentials.APIKey != "" || cfg.Credentials.APISecret != "" {
		t.Errorf("expected credentials to start empty, got %+v", cfg.Credentials)
	}
	if cfg.HTTPTimeout != 0 {
		t.Errorf("expected zero HTTP timeout, got %v", cfg.HTTPTimeout)
	}
	if cfg.HandshakeTimeout != 0 {
		t.Errorf("expected zero handshake timeout, got %v", cfg.HandshakeTimeout)
	}
	if cfg.SymbolRefreshInterval != 0 {
		t.Errorf("expected zero refresh interval, got %v", cfg.SymbolRefreshInterval)
	}
}

func TestCloneExchangeSettingsPerformsDeepCopy(t *testing.T) {
	original := NewExchangeSettings()
	original.REST["spot"] = "https://example.com"
	original.Websocket.PublicURL = "wss://public.example.com"
	original.Websocket.PrivateURL = "wss://private.example.com"
	original.Credentials.APIKey = "key"
	original.Credentials.APISecret = "secret"
	original.HTTPTimeout = 5 * time.Second
	original.HandshakeTimeout = 10 * time.Second
	original.SymbolRefreshInterval = time.Minute

	cloned := CloneExchangeSettings(original)

	if &cloned == &original {
		t.Fatal("expected cloned value to be a copy")
	}
	if cloned.REST["spot"] != original.REST["spot"] {
		t.Fatalf("expected REST entry to be copied, got %s", cloned.REST["spot"])
	}

	// mutate clone and ensure original unaffected
	cloned.REST["spot"] = "https://mutated.example.com"
	if original.REST["spot"] != "https://example.com" {
		t.Fatal("expected original REST map to remain unchanged")
	}
}

func TestNormalizeExchangeName(t *testing.T) {
	name := "  BiNaNcE  "
	if got := normalizeExchangeName(name); got != "binance" {
		t.Errorf("expected normalised name binance, got %s", got)
	}
}
