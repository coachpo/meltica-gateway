package config

import (
	"reflect"
	"testing"
)

func TestLambdaManifestValidate(t *testing.T) {
	manifest := LambdaManifest{
		Lambdas: []LambdaSpec{{
			ID:       "test-lambda",
			Strategy: "delay",
			Config:   map[string]any{"min_delay": "100ms"},
			ProviderSymbols: map[string]ProviderSymbols{
				"fake": {
					Symbols: []string{"BTC-USDT"},
				},
			},
			AutoStart: true,
		}},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
}

func TestLambdaManifestValidateMissingEntries(t *testing.T) {
	manifest := LambdaManifest{}
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected validation error for empty manifest")
	}
}

func TestLambdaManifestValidateMissingFields(t *testing.T) {
	manifest := LambdaManifest{
		Lambdas: []LambdaSpec{{
			ID:       "",
			Strategy: "delay",
			ProviderSymbols: map[string]ProviderSymbols{
				"fake": {
					Symbols: []string{"BTC-USDT"},
				},
			},
		}},
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected validation error for missing id")
	}
}

func TestLambdaManifestProviderSymbols(t *testing.T) {
	spec := LambdaSpec{
		ID:        "lambda-map",
		Strategy:  "delay",
		AutoStart: true,
		ProviderSymbols: map[string]ProviderSymbols{
			"binance": {
				Symbols: []string{"BTC-USDT", "eth-usdt"},
			},
			"binance-futures": {
				Symbols: []string{"btc-usdt"},
			},
			"binance-futures-coin": {
				Symbols: []string{"eth-usdt"},
			},
		},
	}
	manifest := LambdaManifest{
		Lambdas: []LambdaSpec{spec},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
	normalized := manifest.Lambdas[0]
	if !reflect.DeepEqual(normalized.Providers, []string{"binance", "binance-futures", "binance-futures-coin"}) {
		t.Fatalf("unexpected providers: %+v", normalized.Providers)
	}
	if !reflect.DeepEqual(normalized.SymbolsForProvider("binance"), []string{"BTC-USDT", "ETH-USDT"}) {
		t.Fatalf("unexpected symbols for binance: %+v", normalized.SymbolsForProvider("binance"))
	}
	if !reflect.DeepEqual(normalized.SymbolsForProvider("binance-futures"), []string{"BTC-USDT"}) {
		t.Fatalf("unexpected symbols for binance-futures: %+v", normalized.SymbolsForProvider("binance-futures"))
	}
	if !reflect.DeepEqual(normalized.SymbolsForProvider("binance-futures-coin"), []string{"ETH-USDT"}) {
		t.Fatalf("unexpected symbols for binance-futures-coin: %+v", normalized.SymbolsForProvider("binance-futures-coin"))
	}
}

func TestLambdaManifestSingleSymbolAssignment(t *testing.T) {
	spec := LambdaSpec{
		ID:        "lambda-single",
		Strategy:  "delay",
		AutoStart: true,
		ProviderSymbols: map[string]ProviderSymbols{
			"binance": {
				Symbols: []string{"BTC-USDT"},
			},
		},
	}
	manifest := LambdaManifest{
		Lambdas: []LambdaSpec{spec},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
	normalized := manifest.Lambdas[0]
	if len(normalized.Providers) != 1 || normalized.Providers[0] != "binance" {
		t.Fatalf("unexpected providers: %+v", normalized.Providers)
	}
	if !reflect.DeepEqual(normalized.SymbolsForProvider("binance"), []string{"BTC-USDT"}) {
		t.Fatalf("unexpected symbols for binance: %+v", normalized.SymbolsForProvider("binance"))
	}
}
