package config

import "testing"

func TestBuildProviderSpecs(t *testing.T) {
	configs := map[Exchange]map[string]any{
		"fake": {
			"exchange": map[string]any{
				"name":              "fake",
				"ticker_interval":   "500ms",
				"book_snapshot":     "5s",
				"custom_param":      42,
				"another_parameter": true,
			},
		},
		"BINANCE": {
			"exchange": map[string]any{
				"name":  "Binance",
				"venue": "spot",
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

	fakeSpec, ok := lookup["fake"]
	if !ok {
		t.Fatalf("expected fake spec present")
	}
	if fakeSpec.Exchange != "fake" {
		t.Fatalf("expected exchange fake, got %s", fakeSpec.Exchange)
	}
	if _, ok := fakeSpec.Config["ticker_interval"]; !ok {
		t.Fatalf("expected config to include ticker_interval")
	}
	if fakeSpec.Config["name"] != "fake" {
		t.Fatalf("expected config name to be fake, got %v", fakeSpec.Config["name"])
	}

	binanceSpec, ok := lookup["BINANCE"]
	if !ok {
		t.Fatalf("expected BINANCE spec present")
	}
	if binanceSpec.Exchange != "binance" {
		t.Fatalf("expected canonical exchange binance, got %s", binanceSpec.Exchange)
	}
}

func TestBuildProviderSpecsErrors(t *testing.T) {
	t.Run("missing exchanges", func(t *testing.T) {
		if _, err := BuildProviderSpecs(nil); err == nil {
			t.Fatal("expected error for nil exchanges")
		}
	})

	t.Run("missing exchange block", func(t *testing.T) {
		_, err := BuildProviderSpecs(map[Exchange]map[string]any{
			"binance": {},
		})
		if err == nil {
			t.Fatal("expected error for missing exchange block")
		}
	})

	t.Run("invalid exchange map", func(t *testing.T) {
		_, err := BuildProviderSpecs(map[Exchange]map[string]any{
			"binance": {
				"exchange": "not-a-map",
			},
		})
		if err == nil {
			t.Fatal("expected error for non-map exchange block")
		}
	})

	t.Run("missing exchange name", func(t *testing.T) {
		_, err := BuildProviderSpecs(map[Exchange]map[string]any{
			"binance": {
				"exchange": map[string]any{},
			},
		})
		if err == nil {
			t.Fatal("expected error for missing exchange.name")
		}
	})
}
