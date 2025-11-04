// Command bootstrap_strategies normalizes strategy modules and emits a registry manifest.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"
	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/app/lambda/strategies"
)

type registryEntry struct {
	Tags   map[string]string           `json:"tags"`
	Hashes map[string]registryLocation `json:"hashes"`
}

type registryLocation struct {
	Tag  string `json:"tag"`
	Path string `json:"path"`
}

type usageExport struct {
	Usage []revisionUsage `json:"usage"`
}

type revisionUsage struct {
	Strategy  string    `json:"strategy"`
	Hash      string    `json:"hash"`
	Instances []string  `json:"instances"`
	Count     int       `json:"count"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

func main() {
	root := flag.String("root", "strategies", "Path to the strategies directory")
	write := flag.Bool("write", false, "Apply filesystem moves in addition to emitting registry.json")
	usageFile := flag.String("usage", "", "Path to usage export JSON for highlighting unused revisions")
	flag.Parse()

	cleanRoot, err := ensureDir(*root)
	if err != nil {
		fatal(err)
	}

	modules, err := discoverModules(cleanRoot)
	if err != nil {
		fatal(err)
	}
	if len(modules) == 0 {
		fatal(fmt.Errorf("no JavaScript strategies found under %s", cleanRoot))
	}

	reg := make(map[string]registryEntry)
	for _, module := range modules {
		entry := reg[module.name]
		if entry.Tags == nil {
			entry.Tags = make(map[string]string)
		}
		if entry.Hashes == nil {
			entry.Hashes = make(map[string]registryLocation)
		}

		targetPath := module.path
		if *write {
			target, err := materializeModule(cleanRoot, module)
			if err != nil {
				fatal(err)
			}
			targetPath = target
		}

		rel, err := filepath.Rel(cleanRoot, targetPath)
		if err != nil {
			fatal(err)
		}
		entry.Tags[module.version] = module.hash
		entry.Hashes[module.hash] = registryLocation{
			Tag:  module.version,
			Path: filepath.ToSlash(rel),
		}
		reg[module.name] = entry
	}

	for name, entry := range reg {
		entry.Tags["latest"] = pickLatestTag(entry.Tags)
		reg[name] = entry
	}

	if err := writeRegistry(cleanRoot, reg); err != nil {
		fatal(err)
	}
	fmt.Printf("registry.json generated for %d strategies under %s\n", len(reg), cleanRoot)
	if !*write {
		fmt.Println("filesystem left untouched (pass -write to reorganize)")
	}
	if usagePath := strings.TrimSpace(*usageFile); usagePath != "" {
		reportUsage(usagePath, reg)
	}
}

type moduleInfo struct {
	name        string
	version     string
	path        string
	source      []byte
	metadata    strategies.Metadata
	hash        string
	isVersioned bool
}

func discoverModules(root string) ([]moduleInfo, error) {
	var modules []moduleInfo
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk %s: %w", path, walkErr)
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".js") {
			return nil
		}
		info, err := loadModule(path)
		if err != nil {
			return fmt.Errorf("load module %s: %w", path, err)
		}
		modules = append(modules, info)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover modules under %s: %w", root, err)
	}
	return modules, nil
}

func loadModule(path string) (moduleInfo, error) {
	// #nosec G304 -- path originates from filesystem walk scoped to the provided root
	source, err := os.ReadFile(path)
	if err != nil {
		return moduleInfo{}, fmt.Errorf("read %q: %w", path, err)
	}
	meta, err := extractMetadata(path, source)
	if err != nil {
		return moduleInfo{}, fmt.Errorf("extract metadata %s: %w", path, err)
	}
	normalizedName := strings.ToLower(strings.TrimSpace(meta.Name))
	if normalizedName == "" {
		return moduleInfo{}, fmt.Errorf("%s: metadata.name required", path)
	}
	version := strings.TrimSpace(meta.Version)
	if version == "" {
		version = "v1.0.0"
	}
	sum := sha256.Sum256(source)
	hash := "sha256:" + hex.EncodeToString(sum[:])

	rel, err := filepath.Rel(filepath.Dir(path), path)
	if err != nil {
		return moduleInfo{}, fmt.Errorf("relative path for %s: %w", path, err)
	}
	isVersioned := strings.Count(filepath.ToSlash(rel), "/") >= 1

	return moduleInfo{
		name:        normalizedName,
		version:     version,
		path:        path,
		source:      source,
		metadata:    meta,
		hash:        hash,
		isVersioned: isVersioned,
	}, nil
}

func extractMetadata(filename string, source []byte) (strategies.Metadata, error) {
	rt := goja.New()
	rt.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	program, err := goja.Compile(filename, string(source), true)
	if err != nil {
		return strategies.Metadata{}, fmt.Errorf("compile %s: %w", filename, err)
	}
	module := rt.NewObject()
	exports := rt.NewObject()
	if err := module.Set("exports", exports); err != nil {
		return strategies.Metadata{}, fmt.Errorf("module init: %w", err)
	}
	if err := rt.Set("module", module); err != nil {
		return strategies.Metadata{}, fmt.Errorf("module init: %w", err)
	}
	if _, err := rt.RunProgram(program); err != nil {
		return strategies.Metadata{}, fmt.Errorf("execute %s: %w", filename, err)
	}
	value := module.Get("exports")
	obj := value.ToObject(rt)
	if obj == nil {
		return strategies.Metadata{}, errors.New("module exports must be an object")
	}
	raw := obj.Get("metadata")
	if goja.IsUndefined(raw) || goja.IsNull(raw) {
		return strategies.Metadata{}, errors.New("metadata export missing")
	}

	var meta strategies.Metadata
	if err := rt.ExportTo(raw, &meta); err != nil {
		return strategies.Metadata{}, fmt.Errorf("metadata export invalid: %w", err)
	}
	meta.Name = strings.ToLower(strings.TrimSpace(meta.Name))
	return meta, nil
}

func materializeModule(root string, module moduleInfo) (string, error) {
	if module.isVersioned {
		return module.path, nil
	}
	targetDir := filepath.Join(root, module.name, module.version)
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		return "", fmt.Errorf("create directory %s: %w", targetDir, err)
	}
	target := filepath.Join(targetDir, fmt.Sprintf("%s.js", module.name))
	if err := os.WriteFile(target, module.source, 0o600); err != nil {
		return "", fmt.Errorf("write module %s: %w", target, err)
	}
	if err := os.Remove(module.path); err != nil {
		return "", fmt.Errorf("remove original %s: %w", module.path, err)
	}
	return target, nil
}

func pickLatestTag(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	candidates := make([]string, 0, len(tags))
	for tag := range tags {
		if tag == "latest" {
			continue
		}
		candidates = append(candidates, tag)
	}
	if len(candidates) == 0 {
		for _, hash := range tags {
			return hash
		}
		return ""
	}
	sort.Strings(candidates)
	return tags[candidates[len(candidates)-1]]
}

func writeRegistry(root string, reg map[string]registryEntry) error {
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	tmp := filepath.Join(root, "registry.json.tmp")
	target := filepath.Join(root, "registry.json")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp registry %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("rename registry %s: %w", target, err)
	}
	return nil
}

func reportUsage(path string, reg map[string]registryEntry) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: read usage export %s: %v\n", path, err)
		return
	}
	var payload usageExport
	if err := json.Unmarshal(data, &payload); err != nil {
		var direct []revisionUsage
		if err2 := json.Unmarshal(data, &direct); err2 != nil {
			fmt.Fprintf(os.Stderr, "bootstrap: decode usage export %s: %v\n", path, err)
			return
		}
		payload.Usage = direct
	}
	usageIndex := make(map[string]map[string]revisionUsage)
	for _, usage := range payload.Usage {
		name := strings.ToLower(strings.TrimSpace(usage.Strategy))
		hash := strings.TrimSpace(usage.Hash)
		if name == "" || hash == "" {
			continue
		}
		if _, ok := usageIndex[name]; !ok {
			usageIndex[name] = make(map[string]revisionUsage)
		}
		usageIndex[name][hash] = usage
	}
	var unused []string
	for name, entry := range reg {
		normalized := strings.ToLower(strings.TrimSpace(name))
		for hash, loc := range entry.Hashes {
			usage := usageIndex[normalized][hash]
			if usage.Count == 0 {
				tag := loc.Tag
				if tag == "" {
					tag = "(untagged)"
				}
				unused = append(unused, fmt.Sprintf("%s %s [%s]", name, tag, hash))
			}
		}
	}
	sort.Strings(unused)
	fmt.Println()
	if len(unused) == 0 {
		fmt.Println("usage report: no unused revisions detected (all tracked hashes have usage).")
		return
	}
	fmt.Println("usage report: revisions with zero running instances:")
	for _, line := range unused {
		fmt.Printf("  - %s\n", line)
	}
}

func ensureDir(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("directory required")
	}
	clean := filepath.Clean(trimmed)
	if err := os.MkdirAll(clean, 0o750); err != nil {
		return "", fmt.Errorf("ensure directory %s: %w", clean, err)
	}
	return clean, nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "bootstrap: %v\n", err)
	os.Exit(1)
}
