package js

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/meltica/internal/domain/schema"
)

const sampleModule = `
module.exports = {
  metadata: {
    name: "noop",
    tag: "v1.0.0",
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

func writeVersionedModule(t *testing.T, dir, name, tag string, source []byte) string {
	t.Helper()
	sum := sha256.Sum256(source)
	digest := hex.EncodeToString(sum[:])
	moduleDir := filepath.Join(dir, name, digest)
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}
	path := filepath.Join(moduleDir, fmt.Sprintf("%s.js", name))
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatalf("write module: %v", err)
	}
	return path
}

func writeRegistry(t *testing.T, root, name, tag, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read module: %v", err)
	}
	hash := sha256.Sum256(data)
	hashHex := hex.EncodeToString(hash[:])
	registry := fmt.Sprintf(`{
  "%s": {
    "tags": {
      "latest": "sha256:%[3]s",
      "%s": "sha256:%[3]s"
    },
    "hashes": {
      "sha256:%[3]s": {
        "tag": "%s",
        "path": "%s"
      }
    }
  }
}
`, name, tag, hashHex, tag, filepath.ToSlash(filepath.Join(name, hashHex, fmt.Sprintf("%s.js", name))))
	if err := os.WriteFile(filepath.Join(root, "registry.json"), []byte(registry), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
}

func TestLoaderRefreshAndList(t *testing.T) {
	dir := t.TempDir()
	modulePath := writeVersionedModule(t, dir, "noop", "v1.0.0", []byte(sampleModule))
	writeRegistry(t, dir, "noop", "v1.0.0", modulePath)

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
	if modules[0].Tag != "v1.0.0" {
		t.Fatalf("expected tag v1.0.0, got %s", modules[0].Tag)
	}
	if len(modules[0].Metadata.Events) != 1 || modules[0].Metadata.Events[0] != schema.EventTypeTrade {
		t.Fatalf("unexpected metadata events: %+v", modules[0].Metadata.Events)
	}
	if len(modules[0].TagAliases) == 0 || modules[0].TagAliases["latest"] == "" {
		t.Fatalf("expected tag aliases to include latest")
	}
	if len(modules[0].Revisions) != 1 {
		t.Fatalf("expected one revision summary, got %d", len(modules[0].Revisions))
	}
}

func TestLoaderRefreshOverridesMetadataTag(t *testing.T) {
	dir := t.TempDir()
	source := strings.Replace(sampleModule, "tag: \"v1.0.0\"", "tag: \"legacy\"", 1)
	modulePath := writeVersionedModule(t, dir, "noop", "v2.0.0", []byte(source))
	writeRegistry(t, dir, "noop", "v2.0.0", modulePath)

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
	if modules[0].Tag != "v2.0.0" {
		t.Fatalf("expected tag v2.0.0, got %s", modules[0].Tag)
	}
	if modules[0].Metadata.Tag != "v2.0.0" {
		t.Fatalf("metadata tag not normalized, got %s", modules[0].Metadata.Tag)
	}
}

func TestInstanceCall(t *testing.T) {
	dir := t.TempDir()
	modulePath := writeVersionedModule(t, dir, "noop", "v1.0.0", []byte(sampleModule))
	writeRegistry(t, dir, "noop", "v1.0.0", modulePath)

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

func TestWriteAndReadLegacyFallback(t *testing.T) {
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

func TestDeleteLegacyFallback(t *testing.T) {
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

func TestCompileModuleSyntaxErrorProducesDiagnostic(t *testing.T) {
	dir := t.TempDir()
	source := `
module.exports = {
  metadata: {
    name: "broken",
    displayName: "Broken Strategy",
    config: [],
    events: ["` + string(schema.EventTypeTrade) + `"]
  },
  create: function () {
    return {};
  // missing closing braces
`
	path := filepath.Join(dir, "broken.js")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write module: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat module: %v", err)
	}
	if _, err := compileModule(path, info); err == nil {
		t.Fatalf("expected compile error")
	} else {
		diagErr, ok := AsDiagnosticError(err)
		if !ok {
			t.Fatalf("expected diagnostic error, got %v", err)
		}
		diagnostics := diagErr.Diagnostics()
		if len(diagnostics) == 0 {
			t.Fatalf("expected diagnostics for syntax error")
		}
		first := diagnostics[0]
		if first.Stage != DiagnosticStageCompile {
			t.Fatalf("expected compile diagnostic stage, got %s", first.Stage)
		}
		if first.Line < 0 {
			t.Fatalf("expected non-negative diagnostic line number, got %d", first.Line)
		}
		if strings.TrimSpace(first.Message) == "" {
			t.Fatalf("expected diagnostic message")
		}
	}
}

func TestCompileModuleMissingMetadataProducesDiagnostic(t *testing.T) {
	dir := t.TempDir()
	source := `
module.exports = {
  create: function () {
    return {};
  }
};
`
	path := filepath.Join(dir, "missing.js")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write module: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat module: %v", err)
	}
	if _, err := compileModule(path, info); err == nil {
		t.Fatalf("expected metadata error when metadata export missing")
	} else {
		diagErr, ok := AsDiagnosticError(err)
		if !ok {
			t.Fatalf("expected diagnostic error, got %v", err)
		}
		if diagErr.Error() != "metadata export missing" {
			t.Fatalf("unexpected diagnostic message: %s", diagErr.Error())
		}
		if diags := diagErr.Diagnostics(); len(diags) == 0 {
			t.Fatalf("expected diagnostics for missing metadata export")
		}
	}
}

func TestCompileModuleMetadataValidationDiagnostics(t *testing.T) {
	dir := t.TempDir()
	source := `
module.exports = {
  metadata: {
    name: "validator",
    displayName: "",
    config: [],
    events: ["UnknownEvent"]
  },
  create: function () {
    return {};
  }
};
`
	path := filepath.Join(dir, "validator.js")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write module: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat module: %v", err)
	}
	if _, err := compileModule(path, info); err == nil {
		t.Fatalf("expected metadata validation error")
	} else {
		diagErr, ok := AsDiagnosticError(err)
		if !ok {
			t.Fatalf("expected diagnostic error, got %v", err)
		}
		if diagErr.Error() == "" {
			t.Fatalf("expected diagnostic error message")
		}
		diagnostics := diagErr.Diagnostics()
		if len(diagnostics) < 1 {
			t.Fatalf("expected validation diagnostics")
		}
		for _, diag := range diagnostics {
			if diag.Stage != DiagnosticStageValidation {
				t.Fatalf("expected validation stage, got %s", diag.Stage)
			}
			if strings.TrimSpace(diag.Message) == "" {
				t.Fatalf("expected diagnostic message")
			}
		}
	}
}

func TestCompileModuleInjectsDryRunField(t *testing.T) {
	dir := t.TempDir()
	source := `
module.exports = {
  metadata: {
    name: "threshold",
    displayName: "Threshold Strategy",
    config: [
      { name: "limit", type: "number", description: "Limit threshold", required: true }
    ],
    events: ["` + string(schema.EventTypeTrade) + `"]
  },
  create: function () {
    return {};
  }
};
`
	path := filepath.Join(dir, "threshold.js")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write module: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat module: %v", err)
	}
	module, err := compileModule(path, info)
	if err != nil {
		t.Fatalf("compileModule: %v", err)
	}
	foundDryRun := false
	for _, field := range module.Metadata.Config {
		if field.Name == "dry_run" {
			foundDryRun = true
			if field.Type != "bool" {
				t.Fatalf("expected dry_run type bool, got %s", field.Type)
			}
		}
		if strings.TrimSpace(field.Name) == "" {
			t.Fatalf("config field name should be trimmed")
		}
	}
	if !foundDryRun {
		t.Fatalf("expected dry_run field injected")
	}
}

func TestResolveReferenceVariants(t *testing.T) {
	dir := t.TempDir()
	modulePath := writeVersionedModule(t, dir, "noop", "v1.0.0", []byte(sampleModule))
	writeRegistry(t, dir, "noop", "v1.0.0", modulePath)

	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if err := loader.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	baseModule, err := loader.Get("noop")
	if err != nil {
		t.Fatalf("Get noop: %v", err)
	}

	t.Run("default name resolves latest", func(t *testing.T) {
		resolution, err := loader.ResolveReference("noop")
		if err != nil {
			t.Fatalf("ResolveReference noop: %v", err)
		}
		if resolution.Name != "noop" {
			t.Fatalf("expected name noop, got %s", resolution.Name)
		}
		if resolution.Hash != baseModule.Hash {
			t.Fatalf("hash mismatch: %s != %s", resolution.Hash, baseModule.Hash)
		}
		if resolution.Tag == "" {
			t.Fatalf("expected tag to be populated")
		}
		if resolution.Module == nil || resolution.Module.Hash != baseModule.Hash {
			t.Fatalf("expected module reference")
		}
	})

	t.Run("name and tag resolves specific revision", func(t *testing.T) {
		resolution, err := loader.ResolveReference("noop:v1.0.0")
		if err != nil {
			t.Fatalf("ResolveReference noop:v1.0.0: %v", err)
		}
		if resolution.Tag != "v1.0.0" {
			t.Fatalf("expected tag v1.0.0, got %s", resolution.Tag)
		}
		if resolution.Hash != baseModule.Hash {
			t.Fatalf("hash mismatch for tag resolution")
		}
	})

	t.Run("name and hash resolves revision", func(t *testing.T) {
		resolution, err := loader.ResolveReference("noop@" + baseModule.Hash)
		if err != nil {
			t.Fatalf("ResolveReference noop@hash: %v", err)
		}
		if resolution.Hash != baseModule.Hash {
			t.Fatalf("hash mismatch for hash resolution")
		}
		if resolution.Module == nil || resolution.Module.Hash != baseModule.Hash {
			t.Fatalf("expected module pointer on hash resolution")
		}
	})

	t.Run("canonical hash string resolves directly", func(t *testing.T) {
		resolution, err := loader.ResolveReference(baseModule.Hash)
		if err != nil {
			t.Fatalf("ResolveReference hash: %v", err)
		}
		if resolution.Hash != baseModule.Hash {
			t.Fatalf("expected hash %s, got %s", baseModule.Hash, resolution.Hash)
		}
	})

	t.Run("unknown tag failure", func(t *testing.T) {
		if _, err := loader.ResolveReference("noop:unknown"); err == nil {
			t.Fatalf("expected error resolving unknown tag")
		}
	})

	t.Run("Get supports tag selector", func(t *testing.T) {
		module, err := loader.Get("noop:v1.0.0")
		if err != nil {
			t.Fatalf("Get noop:v1.0.0: %v", err)
		}
		if module.Hash != baseModule.Hash {
			t.Fatalf("unexpected module hash %s", module.Hash)
		}
	})

	t.Run("Get supports canonical hash", func(t *testing.T) {
		module, err := loader.Get(baseModule.Hash)
		if err != nil {
			t.Fatalf("Get by hash: %v", err)
		}
		if module.Hash != baseModule.Hash {
			t.Fatalf("hash mismatch on hash lookup")
		}
		if !strings.EqualFold(module.Name, baseModule.Name) {
			t.Fatalf("expected module name %s, got %s", baseModule.Name, module.Name)
		}
	})

	t.Run("hash belongs to different strategy", func(t *testing.T) {
		modulePath := writeVersionedModule(t, dir, "alt", "v1.0.0", []byte(strings.ReplaceAll(sampleModule, "noop", "alt")))
		writeRegistry(t, dir, "alt", "v1.0.0", modulePath)
		if err := loader.Refresh(context.Background()); err != nil {
			t.Fatalf("Refresh with alt: %v", err)
		}
		alt, err := loader.Get("alt")
		if err != nil {
			t.Fatalf("Get alt: %v", err)
		}
		if _, err := loader.ResolveReference("noop@" + alt.Hash); err == nil {
			t.Fatalf("expected error when hash belongs to another strategy")
		}
	})
}

func TestWriteWithRegistryCreatesVersionedLayout(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if err := loader.Write("noop.js", []byte(sampleModule)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	hashSum := sha256.Sum256([]byte(sampleModule))
	digest := hex.EncodeToString(hashSum[:])
	expectedPath := filepath.Join(dir, "noop", digest, "noop.js")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected module written to %s: %v", expectedPath, err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var reg registry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("unmarshal registry: %v", err)
	}
	entry, ok := reg["noop"]
	if !ok {
		t.Fatalf("expected noop entry in registry")
	}
	hashes := entry.Hashes
	if len(hashes) != 1 {
		t.Fatalf("expected one hash entry, got %d", len(hashes))
	}
	for hash, loc := range hashes {
		if !strings.HasPrefix(hash, "sha256:") {
			t.Fatalf("unexpected hash %s", hash)
		}
		expectedRel := filepath.ToSlash(filepath.Join("noop", digest, "noop.js"))
		if loc.Path != expectedRel {
			t.Fatalf("unexpected path %s", loc.Path)
		}
		if entry.Tags["latest"] != hash {
			t.Fatalf("expected latest tag to point to %s, got %s", hash, entry.Tags["latest"])
		}
		if entry.Tags["v1.0.0"] != hash {
			t.Fatalf("expected version tag mapping, got %+v", entry.Tags)
		}
	}
}

func TestDeleteWithRegistrySelector(t *testing.T) {
	dir := t.TempDir()
	modulePath := writeVersionedModule(t, dir, "noop", "v1.0.0", []byte(sampleModule))
	writeRegistry(t, dir, "noop", "v1.0.0", modulePath)

	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if err := loader.Delete("noop:v1.0.0"); err != nil {
		t.Fatalf("Delete by tag: %v", err)
	}

	digestDir := filepath.Base(filepath.Dir(modulePath))
	expectedPath := filepath.Join(dir, "noop", digestDir, "noop.js")
	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		t.Fatalf("expected module file removed, got %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var reg registry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("unmarshal registry: %v", err)
	}
	if len(reg) != 0 {
		t.Fatalf("expected registry cleared, got %+v", reg)
	}
}

func TestStoreAllowsTagReassignment(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write registry stub: %v", err)
	}
	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	first, err := loader.Store([]byte(sampleModule), ModuleWriteOptions{PromoteLatest: true})
	if err != nil {
		t.Fatalf("Store v1: %v", err)
	}
	updatedSource := strings.Replace(sampleModule, "\"ok\"", "\"updated\"", 1)
	second, err := loader.Store([]byte(updatedSource), ModuleWriteOptions{PromoteLatest: true})
	if err != nil {
		t.Fatalf("Store updated: %v", err)
	}
	if first.Hash == second.Hash {
		t.Fatalf("expected distinct hashes for updated module")
	}
	data, err := os.ReadFile(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var reg registry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("decode registry: %v", err)
	}
	entry, ok := reg[first.Name]
	if !ok {
		t.Fatalf("expected registry entry for %s", first.Name)
	}
	if len(entry.Hashes) != 2 {
		t.Fatalf("expected both hashes preserved, got %d", len(entry.Hashes))
	}
	if entry.Tags[first.Tag] != second.Hash {
		t.Fatalf("expected tag %s to point to %s, got %s", first.Tag, second.Hash, entry.Tags[first.Tag])
	}
	if _, ok := entry.Hashes[first.Hash]; !ok {
		t.Fatalf("expected registry to retain first hash")
	}
	if _, ok := entry.Hashes[second.Hash]; !ok {
		t.Fatalf("expected registry to record second hash")
	}
}

func TestAssignTagUpdatesSummariesWithoutRefresh(t *testing.T) {
	dir := t.TempDir()
	modulePath := writeVersionedModule(t, dir, "noop", "v1.0.0", []byte(sampleModule))
	writeRegistry(t, dir, "noop", "v1.0.0", modulePath)
	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if err := loader.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	resolution, err := loader.ResolveReference("noop")
	if err != nil {
		t.Fatalf("ResolveReference noop: %v", err)
	}
	if _, err := loader.AssignTag("noop", "prod", resolution.Hash); err != nil {
		t.Fatalf("AssignTag prod: %v", err)
	}
	modules := loader.List()
	if len(modules) != 1 {
		t.Fatalf("expected one module summary, got %d", len(modules))
	}
	if modules[0].TagAliases["prod"] != resolution.Hash {
		t.Fatalf("expected prod alias to map to %s, got %s", resolution.Hash, modules[0].TagAliases["prod"])
	}
	data, err := os.ReadFile(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var reg registry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("decode registry: %v", err)
	}
	if reg["noop"].Tags["prod"] != resolution.Hash {
		t.Fatalf("expected registry alias prod -> %s", resolution.Hash)
	}
}

func TestDeleteTagGuardRails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write registry stub: %v", err)
	}
	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	first, err := loader.Store([]byte(sampleModule), ModuleWriteOptions{PromoteLatest: true})
	if err != nil {
		t.Fatalf("Store v1: %v", err)
	}
	v2Source := strings.Replace(sampleModule, "tag: \"v1.0.0\"", "tag: \"v2.0.0\"", 1)
	v2Source = strings.Replace(v2Source, "No operation strategy", "Updated release", 1)
	second, err := loader.Store([]byte(v2Source), ModuleWriteOptions{PromoteLatest: true})
	if err != nil {
		t.Fatalf("Store v2: %v", err)
	}
	if _, err := loader.AssignTag(first.Name, "prod", second.Hash); err != nil {
		t.Fatalf("AssignTag prod -> v2: %v", err)
	}
	if _, err := loader.DeleteTag(first.Name, "latest"); err == nil {
		t.Fatalf("expected error deleting latest tag")
	}
	if _, err := loader.DeleteTag(first.Name, first.Tag); err == nil {
		t.Fatalf("expected error deleting sole selector for %s", first.Hash)
	}
	if _, err := loader.DeleteTag(first.Name, "prod"); err != nil {
		t.Fatalf("DeleteTag prod: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var reg registry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("decode registry: %v", err)
	}
	if _, ok := reg[first.Name].Tags["prod"]; ok {
		t.Fatalf("expected prod alias removed")
	}
	if reg[first.Name].Tags["latest"] != second.Hash {
		t.Fatalf("expected latest to remain pointed at %s", second.Hash)
	}
}

func TestLoaderListWithUsage(t *testing.T) {
	dir := t.TempDir()
	modulePath := writeVersionedModule(t, dir, "noop", "v1.0.0", []byte(sampleModule))
	writeRegistry(t, dir, "noop", "v1.0.0", modulePath)

	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if err := loader.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	modules := loader.List()
	if len(modules) != 1 {
		t.Fatalf("expected module list of size 1, got %d", len(modules))
	}
	hash := modules[0].Hash
	now := time.Now().UTC()
	usageSnapshot := ModuleUsageSnapshot{
		Name:      modules[0].Name,
		Hash:      hash,
		Instances: []string{"alpha"},
		Count:     1,
		FirstSeen: now,
		LastSeen:  now,
	}
	withUsage := loader.ListWithUsage([]ModuleUsageSnapshot{usageSnapshot})
	if len(withUsage[0].Running) != 1 {
		t.Fatalf("expected running usage entry, got %+v", withUsage[0].Running)
	}
	if withUsage[0].Running[0].Hash != hash {
		t.Fatalf("unexpected running hash %s", withUsage[0].Running[0].Hash)
	}
	if withUsage[0].Revisions[0].Retired {
		t.Fatalf("expected revision not retired when count > 0")
	}

	zeroUsage := loader.ListWithUsage([]ModuleUsageSnapshot{{Name: modules[0].Name, Hash: hash, Count: 0}})
	if len(zeroUsage[0].Running) != 0 {
		t.Fatalf("expected no running entries for zero usage")
	}
	if zeroUsage[0].Revisions[0].Retired {
		t.Fatalf("expected revision not retired while a tag still references it")
	}

	moduleSummary, err := loader.ModuleWithUsage("noop", []ModuleUsageSnapshot{usageSnapshot})
	if err != nil {
		t.Fatalf("ModuleWithUsage: %v", err)
	}
	if len(moduleSummary.Running) != 1 || moduleSummary.Running[0].Hash != hash {
		t.Fatalf("unexpected module summary running payload: %+v", moduleSummary.Running)
	}
}

func TestLoaderRegistrySnapshot(t *testing.T) {
	dir := t.TempDir()
	modulePath := writeVersionedModule(t, dir, "noop", "v1.0.0", []byte(sampleModule))
	writeRegistry(t, dir, "noop", "v1.0.0", modulePath)

	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	snapshot, err := loader.RegistrySnapshot()
	if err != nil {
		t.Fatalf("RegistrySnapshot: %v", err)
	}
	if len(snapshot) != 1 {
		t.Fatalf("expected snapshot entry, got %d", len(snapshot))
	}
	entry, ok := snapshot["noop"]
	if !ok {
		t.Fatalf("expected noop entry in snapshot")
	}
	if len(entry.Hashes) != 1 {
		t.Fatalf("expected one hash in snapshot entry, got %d", len(entry.Hashes))
	}
}
