package provider

import (
	"context"
	"testing"

	"github.com/coachpo/meltica/internal/infra/config"
)

func TestSanitizedProviderSpecsRemovesSensitiveFields(t *testing.T) {
	manager := NewManager(nil, nil, nil, nil, nil)

	spec := config.ProviderSpec{
		Name:    "binance",
		Adapter: "binance",
		Config: map[string]any{
			"identifier":    "binance",
			"provider_name": "binance",
			"api_key":       "super-secret",
			"config": map[string]any{
				"rest_timeout": "1s",
				"token":        "hidden",
				"depth":        100,
			},
		},
	}

	if _, err := manager.Create(context.Background(), spec, false); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	sanitized := manager.SanitizedProviderSpecs()
	if len(sanitized) != 1 {
		t.Fatalf("expected 1 sanitized spec, got %d", len(sanitized))
	}

	cfg := sanitized[0].Config
	if cfg == nil {
		t.Fatal("expected sanitized config to be present")
	}
	if _, ok := cfg["api_key"]; ok {
		t.Fatal("expected api_key to be removed from sanitized config")
	}

	nested, _ := cfg["config"].(map[string]any)
	if nested == nil {
		t.Fatal("expected nested config to be present")
	}
	if _, ok := nested["token"]; ok {
		t.Fatal("expected token to be removed from nested config")
	}
	if nested["rest_timeout"] != "1s" {
		t.Fatalf("expected rest_timeout to be retained, got %v", nested["rest_timeout"])
	}
}
