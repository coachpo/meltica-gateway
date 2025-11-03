package js

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/coachpo/meltica/internal/domain/schema"
)

const sampleModule = `
module.exports = {
  metadata: {
    name: "noop",
    displayName: "No-Op",
    description: "No operation strategy",
    config: [],
    events: ["` + string(schema.EventTypeTrade) + `"]
  },
  create: function() {
    return {
      onTrade: function() {
        return "ok";
      }
    };
  }
};
`

func TestLoaderRefreshAndList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noop.js")
	if err := os.WriteFile(path, []byte(sampleModule), 0o600); err != nil {
		t.Fatalf("write sample module: %v", err)
	}

	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if err := loader.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	modules := loader.List()
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if modules[0].Name != "noop" {
		t.Fatalf("expected module name noop, got %s", modules[0].Name)
	}
	if modules[0].Metadata.Name != "noop" {
		t.Fatalf("metadata name mismatch: %s", modules[0].Metadata.Name)
	}
	if len(modules[0].Metadata.Events) != 1 || modules[0].Metadata.Events[0] != schema.EventTypeTrade {
		t.Fatalf("unexpected metadata events: %+v", modules[0].Metadata.Events)
	}
}

func TestInstanceCall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noop.js")
	if err := os.WriteFile(path, []byte(sampleModule), 0o600); err != nil {
		t.Fatalf("write sample module: %v", err)
	}

	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if err := loader.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	module, err := loader.Get("noop")
	if err != nil {
		t.Fatalf("Get module: %v", err)
	}
	instance, err := NewInstance(module)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	defer instance.Close()
	value, err := instance.Call("create")
	if err != nil {
		t.Fatalf("Call create: %v", err)
	}
	exported := value.Export()
	obj, ok := exported.(map[string]any)
	if !ok {
		t.Fatalf("expected exported object map, got %T", exported)
	}
	if _, ok := obj["onTrade"]; !ok {
		t.Fatalf("expected onTrade method on exported object")
	}
}

func TestWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if err := loader.Write("noop.js", []byte(sampleModule)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := loader.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	data, err := loader.Read("noop")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected data from Read")
	}
	summary, err := loader.Module("noop")
	if err != nil {
		t.Fatalf("Module summary: %v", err)
	}
	if summary.Metadata.Name != "noop" {
		t.Fatalf("unexpected metadata: %+v", summary.Metadata)
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noop.js")
	if err := os.WriteFile(path, []byte(sampleModule), 0o600); err != nil {
		t.Fatalf("write sample module: %v", err)
	}
	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if err := loader.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if err := loader.Delete("noop"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, got %v", err)
	}
	if err := loader.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh after delete: %v", err)
	}
	if modules := loader.List(); len(modules) != 0 {
		t.Fatalf("expected empty list after delete, got %d", len(modules))
	}
}
