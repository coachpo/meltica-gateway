package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dop251/goja"

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

func main() {
	root := flag.String("root", "strategies", "Path to the strategies directory")
	write := flag.Bool("write", false, "Apply filesystem moves in addition to emitting registry.json")
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
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
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
	return modules, err
}

func loadModule(path string) (moduleInfo, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return moduleInfo{}, err
	}
	meta, err := extractMetadata(path, source)
	if err != nil {
		return moduleInfo{}, err
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
		return moduleInfo{}, err
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
		return strategies.Metadata{}, err
	}
	module := rt.NewObject()
	exports := rt.NewObject()
	if err := module.Set("exports", exports); err != nil {
		return strategies.Metadata{}, err
	}
	if err := rt.Set("module", module); err != nil {
		return strategies.Metadata{}, err
	}
	if _, err := rt.RunProgram(program); err != nil {
		return strategies.Metadata{}, err
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
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(targetDir, fmt.Sprintf("%s.js", module.name))
	if err := os.WriteFile(target, module.source, 0o600); err != nil {
		return "", err
	}
	if err := os.Remove(module.path); err != nil {
		return "", err
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
		return err
	}
	tmp := filepath.Join(root, "registry.json.tmp")
	target := filepath.Join(root, "registry.json")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

func ensureDir(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("directory required")
	}
	clean := filepath.Clean(trimmed)
	if err := os.MkdirAll(clean, 0o755); err != nil {
		return "", err
	}
	return clean, nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "bootstrap: %v\n", err)
	os.Exit(1)
}
