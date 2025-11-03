package config

import (
	"reflect"
	"testing"
)

func TestLambdaManifestValidate(t *testing.T) {
	manifest := LambdaManifest{
		Lambdas: []LambdaSpec{{
			ID: "test-lambda",
			Strategy: LambdaStrategySpec{
				Identifier: "delay",
				Config:     map[string]any{"min_delay": "100ms"},
			},
			ProviderSymbols: map[string]ProviderSymbols{
				"binance-spot": {
					Symbols: []string{"BTC-USDT"},
				},
			},
		}},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
}

func TestLambdaManifestValidateAllowsEmpty(t *testing.T) {
	manifest := LambdaManifest{}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("expected empty manifest to pass validation, got %v", err)
	}
}

func TestLambdaManifestValidateMissingFields(t *testing.T) {
	manifest := LambdaManifest{
		Lambdas: []LambdaSpec{{
			ID:       "",
			Strategy: LambdaStrategySpec{Identifier: "delay"},
			ProviderSymbols: map[string]ProviderSymbols{
				"binance-spot": {
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
		ID:       "lambda-map",
		Strategy: LambdaStrategySpec{Identifier: "delay"},
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
		ID:       "lambda-single",
		Strategy: LambdaStrategySpec{Identifier: "delay"},
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

func TestLambdaStrategySpecNormalizeTrimsSelectors(t *testing.T) {
	spec := LambdaStrategySpec{
		Identifier: " delay ",
		Config:     nil,
		Selector:   " delay:v1.0.0 ",
		Tag:        " v1.0.0 ",
		Hash:       " SHA256:ABC ",
		Version:    " 1.0.0 ",
	}
	spec.Normalize()
	if spec.Identifier != "delay" {
		t.Fatalf("expected identifier normalized, got %q", spec.Identifier)
	}
	if spec.Selector != "delay:v1.0.0" {
		t.Fatalf("expected selector trimmed, got %q", spec.Selector)
	}
	if spec.Tag != "v1.0.0" {
		t.Fatalf("expected tag trimmed, got %q", spec.Tag)
	}
	if spec.Hash != "SHA256:ABC" {
		t.Fatalf("expected hash trimmed, got %q", spec.Hash)
	}
	if spec.Version != "1.0.0" {
		t.Fatalf("expected version trimmed, got %q", spec.Version)
	}
	if spec.Config == nil {
		t.Fatalf("expected config map initialized")
	}
}
