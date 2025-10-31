package config

import "testing"

func TestBuildProviderSpecs(t *testing.T) {
	configs := map[Provider]map[string]any{
		"binanceSpot": {
			"adapter": map[string]any{
				"identifier": "binance",
				"config": map[string]any{
					"rest_timeout": "5s",
				},
			},
		},
		"COINBASE": {
			"adapter": map[string]any{
				"identifier": "Coinbase",
				"config": map[string]any{
					"ws_url": "wss://stream.exchange",
				},
			},
		},
	}

	specs, err := BuildProviderSpecs(configs)
	if err != nil {
		t.Fatalf("BuildProviderSpecs failed: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}

	lookup := make(map[string]ProviderSpec, len(specs))
	for _, spec := range specs {
		lookup[spec.Name] = spec
	}

	primary, ok := lookup["binanceSpot"]
	if !ok {
		t.Fatalf("expected binanceSpot spec present")
	}
	if primary.Adapter != "binance" {
		t.Fatalf("expected adapter binance, got %s", primary.Adapter)
	}
	nestedRaw, _ := primary.Config["config"]
	nestedCfg, _ := nestedRaw.(map[string]any)
	if _, ok := nestedCfg["rest_timeout"]; !ok {
		t.Fatalf("expected nested config to include rest_timeout")
	}
	if primary.Config["identifier"] != "binance" {
		t.Fatalf("expected identifier to be binance, got %v", primary.Config["identifier"])
	}
	if primary.Config["provider_name"] != "binanceSpot" {
		t.Fatalf("expected provider_name to be binanceSpot, got %v", primary.Config["provider_name"])
	}

	secondary, ok := lookup["COINBASE"]
	if !ok {
		t.Fatalf("expected COINBASE spec present")
	}
	if secondary.Adapter != "coinbase" {
		t.Fatalf("expected canonical adapter coinbase, got %s", secondary.Adapter)
	}
}

func TestBuildProviderSpecsErrors(t *testing.T) {
	t.Run("missing providers", func(t *testing.T) {
		if _, err := BuildProviderSpecs(nil); err == nil {
			t.Fatal("expected error for nil providers")
		}
	})

	t.Run("missing adapter block", func(t *testing.T) {
		_, err := BuildProviderSpecs(map[Provider]map[string]any{
			"binance": {},
		})
		if err == nil {
			t.Fatal("expected error for missing adapter block")
		}
	})

	t.Run("invalid adapter map", func(t *testing.T) {
		_, err := BuildProviderSpecs(map[Provider]map[string]any{
			"binance": {
				"adapter": "not-a-map",
			},
		})
		if err == nil {
			t.Fatal("expected error for non-map adapter block")
		}
	})

	t.Run("missing adapter identifier", func(t *testing.T) {
		_, err := BuildProviderSpecs(map[Provider]map[string]any{
			"binance": {
				"adapter": map[string]any{},
			},
		})
		if err == nil {
			t.Fatal("expected error for missing adapter.identifier")
		}
	})
}
