package config

import "testing"

func TestLambdaManifestValidate(t *testing.T) {
	manifest := LambdaManifest{
		Lambdas: []LambdaSpec{{
			ID:        "test-lambda",
			Provider:  "fake",
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
			ID:       "",
			Provider: "fake",
			Symbol:   "BTC-USDT",
			Strategy: "delay",
		}},
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected validation error for missing id")
	}
}
