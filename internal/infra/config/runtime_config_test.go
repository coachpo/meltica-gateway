package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
