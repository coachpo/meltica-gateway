package config

import "testing"

func TestAppConfigStoreSetRuntimePersistsChanges(t *testing.T) {
	initial := DefaultAppConfig()
	var persisted []AppConfig
	store, err := NewAppConfigStore(initial, func(cfg AppConfig) error {
		persisted = append(persisted, cfg)
		return nil
	})
	if err != nil {
		t.Fatalf("NewAppConfigStore failed: %v", err)
	}

	updatedRuntime := initial.Runtime.Clone()
	updatedRuntime.Eventbus.BufferSize++

	if err := store.SetRuntime(updatedRuntime); err != nil {
		t.Fatalf("SetRuntime returned error: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("expected a single persisted snapshot, got %d", len(persisted))
	}
	if persisted[0].Runtime.Eventbus.BufferSize != updatedRuntime.Eventbus.BufferSize {
		t.Fatalf("persisted runtime buffer size mismatch, got %d", persisted[0].Runtime.Eventbus.BufferSize)
	}

	// Re-applying the same runtime should not trigger persistence again.
	if err := store.SetRuntime(updatedRuntime); err != nil {
		t.Fatalf("SetRuntime returned error: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("expected persistence to be skipped for unchanged runtime, got %d updates", len(persisted))
	}
}

func TestAppConfigStoreSetProvidersPersistsSnapshot(t *testing.T) {
	initial := DefaultAppConfig()
	var persisted []AppConfig
	store, err := NewAppConfigStore(initial, func(cfg AppConfig) error {
		persisted = append(persisted, cfg)
		return nil
	})
	if err != nil {
		t.Fatalf("NewAppConfigStore failed: %v", err)
	}

	specs := []ProviderSpec{
		{
			Name:    "binance",
			Adapter: "binance",
			Config: map[string]any{
				"identifier": "binance",
				"config": map[string]any{
					"rest_endpoint": "https://api.binance.com",
				},
			},
		},
	}

	if err := store.SetProviders(specs); err != nil {
		t.Fatalf("SetProviders returned error: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("expected one persisted snapshot, got %d", len(persisted))
	}

	snapshot := persisted[0]
	if snapshot.Providers == nil {
		t.Fatalf("expected providers map to be populated")
	}
	entry, ok := snapshot.Providers[Provider("binance")]
	if !ok {
		t.Fatalf("expected provider binance in persisted snapshot")
	}
	adapterRaw := entry["adapter"]
	adapterCfg, ok := adapterRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected adapter config map, got %T", adapterRaw)
	}
	if adapterCfg["identifier"] != "binance" {
		t.Fatalf("expected identifier binance in persisted snapshot, got %v", adapterCfg["identifier"])
	}
	nested, ok := adapterCfg["config"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested config map, got %T", adapterCfg["config"])
	}
	if nested["rest_endpoint"] != "https://api.binance.com" {
		t.Fatalf("expected rest endpoint to persist, got %v", nested["rest_endpoint"])
	}

	if err := store.SetProviders(specs); err != nil {
		t.Fatalf("SetProviders returned error: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("expected persistence to be skipped for unchanged providers, got %d updates", len(persisted))
	}
}
