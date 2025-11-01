package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	json "github.com/goccy/go-json"
	"gopkg.in/yaml.v3"
)

func TestLoadRuntimeSnapshotNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if _, err := LoadRuntimeSnapshot(path); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			t.Fatalf("expected error when loading missing snapshot")
		}
		t.Fatalf("expected not-exist error, got %v", err)
	}
}

func TestRuntimeStorePersistenceReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.json")
	initial := DefaultRuntimeConfig()
	store, err := NewRuntimeStoreWithPersistence(initial, func(cfg RuntimeConfig) error {
		return SaveRuntimeSnapshot(path, cfg)
	})
	if err != nil {
		t.Fatalf("create runtime store: %v", err)
	}

	updated := initial.Clone()
	updated.Telemetry.ServiceName = "persisted-gateway"

	if _, err := store.Replace(updated); err != nil {
		t.Fatalf("replace runtime config: %v", err)
	}

	loaded, err := LoadRuntimeSnapshot(path)
	if err != nil {
		t.Fatalf("load persisted snapshot: %v", err)
	}
	if loaded.Telemetry.ServiceName != "persisted-gateway" {
		t.Fatalf("expected telemetry service name to persist, got %s", loaded.Telemetry.ServiceName)
	}
}

func TestRuntimeStorePersistenceUpdateRisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.json")
	initial := DefaultRuntimeConfig()
	store, err := NewRuntimeStoreWithPersistence(initial, func(cfg RuntimeConfig) error {
		return SaveRuntimeSnapshot(path, cfg)
	})
	if err != nil {
		t.Fatalf("create runtime store: %v", err)
	}

	updatedRisk := initial.Risk
	updatedRisk.MaxPositionSize = "999"

	if _, err := store.UpdateRisk(updatedRisk); err != nil {
		t.Fatalf("update risk: %v", err)
	}

	loaded, err := LoadRuntimeSnapshot(path)
	if err != nil {
		t.Fatalf("load persisted snapshot: %v", err)
	}
	if loaded.Risk.MaxPositionSize != "999" {
		t.Fatalf("expected max position size to persist, got %s", loaded.Risk.MaxPositionSize)
	}
}

func TestRuntimeConfigNormalisePreservesExplicitKillSwitchDisable(t *testing.T) {
	var risk RiskConfig
	if err := yaml.Unmarshal([]byte("kill_switch_enabled: false\n"), &risk); err != nil {
		t.Fatalf("unmarshal risk config: %v", err)
	}
	cfg := RuntimeConfig{Risk: risk}
	cfg.Normalise()
	if cfg.Risk.KillSwitchEnabled {
		t.Fatalf("expected kill switch to remain disabled")
	}
	defaults := DefaultRiskConfig()
	if cfg.Risk.MaxPositionSize != defaults.MaxPositionSize {
		t.Fatalf("expected max position size default, got %s", cfg.Risk.MaxPositionSize)
	}
	if cfg.Risk.OrderThrottle != defaults.OrderThrottle {
		t.Fatalf("expected order throttle default, got %f", cfg.Risk.OrderThrottle)
	}
	if len(cfg.Risk.AllowedOrderTypes) != len(defaults.AllowedOrderTypes) {
		t.Fatalf("expected allowed order types default length, got %d", len(cfg.Risk.AllowedOrderTypes))
	}
}

func TestRuntimeConfigNormalisePreservesKillSwitchFromJSON(t *testing.T) {
	var risk RiskConfig
	if err := json.Unmarshal([]byte(`{"killSwitchEnabled":false}`), &risk); err != nil {
		t.Fatalf("unmarshal risk config json: %v", err)
	}
	cfg := RuntimeConfig{Risk: risk}
	cfg.Normalise()
	if cfg.Risk.KillSwitchEnabled {
		t.Fatalf("expected kill switch to remain disabled after JSON decode")
	}
	defaults := DefaultRiskConfig()
	if cfg.Risk.MaxNotionalValue != defaults.MaxNotionalValue {
		t.Fatalf("expected max notional default, got %s", cfg.Risk.MaxNotionalValue)
	}
}
