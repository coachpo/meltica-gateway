package config

import (
	"reflect"
	"testing"
)

func TestLambdaManifestValidate(t *testing.T) {
	manifest := LambdaManifest{
		Lambdas: []LambdaSpec{{
			ID:        "test-lambda",
			Providers: []string{"fake"},
			Symbol:    "BTC-USDT",
			Strategy:  "delay",
			Config:    map[string]any{"min_delay": "100ms"},
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
			ID:        "",
			Providers: []string{"fake"},
			Symbol:    "BTC-USDT",
			Strategy:  "delay",
		}},
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected validation error for missing id")
	}
}

func TestLambdaManifestProviderAssignments(t *testing.T) {
	spec := LambdaSpec{
		ID:       "lambda-map",
		Strategy: "delay",
		ProviderAssignments: map[string]ProviderAssignment{
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
	if !reflect.DeepEqual(normalized.ProviderSymbols("binance"), []string{"BTC-USDT", "ETH-USDT"}) {
		t.Fatalf("unexpected symbols for binance: %+v", normalized.ProviderSymbols("binance"))
	}
	if !reflect.DeepEqual(normalized.ProviderSymbols("binance-futures"), []string{"BTC-USDT"}) {
		t.Fatalf("unexpected symbols for binance-futures: %+v", normalized.ProviderSymbols("binance-futures"))
	}
	if !reflect.DeepEqual(normalized.ProviderSymbols("binance-futures-coin"), []string{"ETH-USDT"}) {
		t.Fatalf("unexpected symbols for binance-futures-coin: %+v", normalized.ProviderSymbols("binance-futures-coin"))
	}
}

func TestLambdaManifestSingleSymbolAssignment(t *testing.T) {
	spec := LambdaSpec{
		ID:       "lambda-single",
		Strategy: "delay",
		ProviderAssignments: map[string]ProviderAssignment{
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
	if !reflect.DeepEqual(normalized.ProviderSymbols("binance"), []string{"BTC-USDT"}) {
		t.Fatalf("unexpected symbols for binance: %+v", normalized.ProviderSymbols("binance"))
	}
}
