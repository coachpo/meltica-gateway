package config

import (
	"os"
	"testing"
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
