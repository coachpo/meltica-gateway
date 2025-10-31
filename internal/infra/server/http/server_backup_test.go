package httpserver

import (
	"testing"

	"github.com/coachpo/meltica/internal/infra/config"
)

func TestBuildProviderConfigSnapshotMasksSensitiveKeys(t *testing.T) {
	specs := []config.ProviderSpec{
		{
			Name:    "binance",
			Adapter: "binance",
			Config: map[string]any{
				"identifier": "binance",
				"api_key":    "super-secret",
				"rest": map[string]any{
					"passphrase": "top-secret",
					"timeout":    "1s",
				},
				"config": map[string]any{
					"wsSecret": "hidden",
					"depth":    100,
				},
			},
		},
	}

	snapshot := buildProviderConfigSnapshot(specs)
	entry, ok := snapshot["binance"]
	if !ok {
		t.Fatalf("expected snapshot entry for provider")
	}

	adapter, ok := entry["adapter"].(map[string]any)
	if !ok {
		t.Fatalf("expected adapter map in snapshot")
	}

	if _, found := adapter["api_key"]; found {
		t.Fatalf("expected api_key to be omitted from snapshot")
	}

	if id := adapter["identifier"]; id != "binance" {
		t.Fatalf("expected identifier to be preserved, got %v", id)
	}

	restConfig, hasRest := adapter["rest"].(map[string]any)
	if !hasRest {
		t.Fatalf("expected rest config to be present")
	}
	if _, found := restConfig["passphrase"]; found {
		t.Fatalf("expected passphrase to be omitted from rest config")
	}
	if got := restConfig["timeout"]; got != "1s" {
		t.Fatalf("expected timeout to be retained, got %v", got)
	}

	nestedConfig, hasConfig := adapter["config"].(map[string]any)
	if !hasConfig {
		t.Fatalf("expected nested config to be present")
	}
	if _, found := nestedConfig["wsSecret"]; found {
		t.Fatalf("expected wsSecret to be omitted from nested config")
	}
	if got := nestedConfig["depth"]; got != 100 {
		t.Fatalf("expected depth to be retained, got %v", got)
	}

	if _, found := specs[0].Config["api_key"]; !found {
		t.Fatalf("original provider spec config should remain unchanged")
	}
	if rest := specs[0].Config["rest"].(map[string]any); rest["passphrase"] == nil {
		t.Fatalf("original rest config should remain unchanged")
	}
}
