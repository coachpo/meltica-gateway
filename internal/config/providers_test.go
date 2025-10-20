package config

import "testing"

func TestBuildProviderSpecs(t *testing.T) {
	configs := map[Exchange]map[string]any{
		"fake": {
			"exchange":          "fake",
			"ticker_interval":   "500ms",
			"book_snapshot":     "5s",
			"custom_param":      42,
			"another_parameter": true,
		},
		"BINANCE": {
			"exchange": "Binance",
			"venue":    "spot",
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
	if _, ok := fakeSpec.Config["exchange"]; ok {
		t.Fatalf("expected exchange key to be omitted from config")
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

	t.Run("empty exchange", func(t *testing.T) {
		_, err := BuildProviderSpecs(map[Exchange]map[string]any{
			"binance": {
				"exchange": "",
			},
		})
		if err == nil {
			t.Fatal("expected error for empty exchange")
		}
	})
}
