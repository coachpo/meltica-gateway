package js

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/dop251/goja"
	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/app/lambda/strategies"
)

// ErrModuleNotFound reports missing strategy modules.
var ErrModuleNotFound = errors.New("strategy module not found")

// ErrRegistryUnavailable indicates registry-backed operations are unsupported.
var ErrRegistryUnavailable = errors.New("strategy registry unavailable")

type registry map[string]registryEntry

type registryEntry struct {
	Tags   map[string]string           `json:"tags"`
	Hashes map[string]registryLocation `json:"hashes"`
}

type registryLocation struct {
	Tag  string `json:"tag"`
	Path string `json:"path"`
}

// Loader manages JavaScript strategy modules sourced from an external directory.
type Loader struct {
	mu       sync.RWMutex
	root     string
	registry registry

	files         map[string]*Module            // keyed by file path
	byName        map[string]*Module            // default (latest) module per strategy name
	byHash        map[string]*Module            // hash -> module
	modulesByName map[string]map[string]*Module // name -> hash -> module
	tags          map[string]map[string]string  // name -> tag -> hash
}

// Module encapsulates the compiled program and metadata for a strategy.
type Module struct {
	Name     string
	Filename string
	Path     string
	Hash     string
	Version  string
	Tags     []string
	Metadata strategies.Metadata
	Program  *goja.Program
	Size     int64
}

// ModuleResolution captures the result of resolving a strategy reference.
type ModuleResolution struct {
	Name   string
	Hash   string
	Tag    string
	Module *Module
}

// ModuleWriteOptions configures how a module revision should be stored.
type ModuleWriteOptions struct {
	Filename      string
	Tag           string
	Aliases       []string
	PromoteLatest bool
}

// ModuleSummary exposes immutable module details for control APIs.
type ModuleSummary struct {
	Name       string              `json:"name"`
	File       string              `json:"file"`
	Path       string              `json:"path"`
	Hash       string              `json:"hash"`
	Version    string              `json:"version"`
	Tags       []string            `json:"tags"`
	TagAliases map[string]string   `json:"tagAliases,omitempty"`
	Revisions  []ModuleRevision    `json:"revisions,omitempty"`
	Size       int64               `json:"size"`
	Metadata   strategies.Metadata `json:"metadata"`
}

// ModuleRevision describes a specific strategy revision available to the loader.
type ModuleRevision struct {
	Hash    string `json:"hash"`
	Tag     string `json:"tag,omitempty"`
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
	Size    int64  `json:"size"`
}

// NewLoader constructs a Loader rooted at the provided directory.
func NewLoader(root string) (*Loader, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return nil, fmt.Errorf("strategy loader: root directory required")
	}
	clean := filepath.Clean(trimmed)
	if err := os.MkdirAll(clean, 0o750); err != nil {
		return nil, fmt.Errorf("strategy loader: ensure directory %q: %w", clean, err)
	}
	return &Loader{
		mu:            sync.RWMutex{},
		root:          clean,
		registry:      nil,
		files:         make(map[string]*Module),
		byName:        make(map[string]*Module),
		byHash:        make(map[string]*Module),
		modulesByName: make(map[string]map[string]*Module),
		tags:          make(map[string]map[string]string),
	}, nil
}

// Root returns the filesystem root used by the loader.
func (l *Loader) Root() string {
	if l == nil {
		return ""
	}
	return l.root
}

// Refresh clears in-memory modules and loads the latest JavaScript strategies from disk.
func (l *Loader) Refresh(ctx context.Context) error {
	if l == nil {
		return fmt.Errorf("strategy loader: nil receiver")
	}
	reg, err := loadRegistry(l.root)
	if err != nil {
		return fmt.Errorf("strategy loader: load registry: %w", err)
	}
	if reg != nil {
		return l.refreshFromRegistry(ctx, reg)
	}
	return l.refreshLegacy(ctx)
}

func (l *Loader) refreshFromRegistry(ctx context.Context, reg registry) error {
	nextFiles := make(map[string]*Module)
	nextByName := make(map[string]*Module)
	nextByHash := make(map[string]*Module)
	modulesByName := make(map[string]map[string]*Module)
	tagsByName := make(map[string]map[string]string)

	for rawName, entry := range reg {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("strategy loader: refresh canceled: %w", err)
			}
			return fmt.Errorf("strategy loader: refresh canceled")
		default:
		}

		name := strings.ToLower(strings.TrimSpace(rawName))
		if name == "" {
			return fmt.Errorf("strategy loader: registry contains empty strategy name")
		}
		hashToModule := make(map[string]*Module)
		for hash, loc := range entry.Hashes {
			normalizedHash := strings.TrimSpace(hash)
			if normalizedHash == "" {
				return fmt.Errorf("strategy loader: registry[%s] hash key empty", rawName)
			}
			modulePath := filepath.Join(l.root, filepath.Clean(loc.Path))
			module, err := loadModule(modulePath)
			if err != nil {
				return fmt.Errorf("strategy loader: load module %q: %w", modulePath, err)
			}
			if module.Name == "" {
				module.Name = name
				module.Metadata.Name = name
			}
			if !strings.EqualFold(module.Name, name) {
				return fmt.Errorf("strategy loader: module name %q does not match registry entry %q", module.Name, rawName)
			}
			if module.Hash != normalizedHash {
				return fmt.Errorf("strategy loader: module hash mismatch for %s (%s != %s)", modulePath, module.Hash, normalizedHash)
			}
			module.Tags = collectTagsForHash(entry.Tags, loc.Tag, normalizedHash)
			if module.Metadata.Version == "" {
				module.Metadata.Version = loc.Tag
			}
			module.Version = module.Metadata.Version
			hashToModule[normalizedHash] = module
			nextFiles[module.Path] = module
			nextByHash[normalizedHash] = module
		}
		if len(hashToModule) == 0 {
			continue
		}
		modulesByName[name] = hashToModule
		tagsByName[name] = cloneStringMap(entry.Tags)

		defaultHash := strings.TrimSpace(entry.Tags["latest"])
		if defaultHash == "" {
			// If latest is not specified, take the first hash in deterministic order.
			hashes := make([]string, 0, len(hashToModule))
			for h := range hashToModule {
				hashes = append(hashes, h)
			}
			sort.Strings(hashes)
			defaultHash = hashes[0]
		}
		defaultModule, ok := hashToModule[defaultHash]
		if !ok {
			return fmt.Errorf("strategy loader: latest hash %q not found for %s", defaultHash, rawName)
		}
		nextByName[name] = defaultModule
	}

	l.mu.Lock()
	l.registry = reg
	l.files = nextFiles
	l.byName = nextByName
	l.byHash = nextByHash
	l.modulesByName = modulesByName
	l.tags = tagsByName
	l.mu.Unlock()
	return nil
}

func (l *Loader) refreshLegacy(ctx context.Context) error {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		return fmt.Errorf("strategy loader: read directory %q: %w", l.root, err)
	}
	nextFiles := make(map[string]*Module)
	nextByName := make(map[string]*Module)
	nextByHash := make(map[string]*Module)
	modulesByName := make(map[string]map[string]*Module)

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("strategy loader: refresh canceled: %w", err)
			}
			return fmt.Errorf("strategy loader: refresh canceled")
		default:
		}

		if entry.IsDir() || !isJavaScriptFile(entry.Name()) {
			continue
		}
		fullPath := filepath.Join(l.root, entry.Name())
		module, err := loadModule(fullPath)
		if err != nil {
			return fmt.Errorf("strategy loader: load module %q: %w", fullPath, err)
		}
		if module.Metadata.Version == "" {
			module.Metadata.Version = "0.0.0"
		}
		module.Version = module.Metadata.Version
		module.Tags = nil

		lowerName := strings.ToLower(module.Name)
		if lowerName == "" {
			return fmt.Errorf("strategy loader: module %q missing name", entry.Name())
		}
		if _, exists := nextByName[lowerName]; exists {
			return fmt.Errorf("strategy loader: duplicate strategy name %q", module.Name)
		}
		nextFiles[module.Path] = module
		nextByName[lowerName] = module
		nextByHash[module.Hash] = module
		modulesByName[lowerName] = map[string]*Module{module.Hash: module}
	}

	l.mu.Lock()
	l.registry = nil
	l.files = nextFiles
	l.byName = nextByName
	l.byHash = nextByHash
	l.modulesByName = modulesByName
	l.tags = make(map[string]map[string]string)
	l.mu.Unlock()
	return nil
}

// List returns the loaded module catalog.
func (l *Loader) List() []ModuleSummary {
	l.mu.RLock()
	defer l.mu.RUnlock()

	out := make([]ModuleSummary, 0, len(l.byName))
	for name, module := range l.byName {
		summary := module.toSummary(name)
		if aliases, ok := l.tags[name]; ok {
			summary.TagAliases = cloneStringMap(aliases)
		}
		if revisions, ok := l.modulesByName[name]; ok {
			list := make([]ModuleRevision, 0, len(revisions))
			for hash, revModule := range revisions {
				list = append(list, ModuleRevision{
					Hash:    hash,
					Tag:     primaryTagForHash(l.tags[name], hash),
					Path:    revModule.Path,
					Version: revModule.Version,
					Size:    revModule.Size,
				})
			}
			sort.Slice(list, func(i, j int) bool {
				if list[i].Tag != "" && list[j].Tag != "" && list[i].Tag != list[j].Tag {
					return list[i].Tag < list[j].Tag
				}
				return list[i].Hash < list[j].Hash
			})
			summary.Revisions = list
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// Module returns the module metadata for the named strategy.
func (l *Loader) Module(name string) (ModuleSummary, error) {
	module, err := l.Get(name)
	if err != nil {
		var empty ModuleSummary
		return empty, err
	}
	return module.toSummary(strings.ToLower(strings.TrimSpace(module.Name))), nil
}

// ResolveReference resolves a strategy identifier of the form
//
//	name
//	name:tag
//	name@hash
//
// and returns the associated module along with the resolved hash and tag alias.
func (l *Loader) ResolveReference(identifier string) (ModuleResolution, error) {
	var empty ModuleResolution
	if l == nil {
		return empty, fmt.Errorf("strategy loader: nil receiver")
	}

	raw := strings.TrimSpace(identifier)
	if raw == "" {
		return empty, fmt.Errorf("strategy loader: identifier required")
	}

	if isHashIdentifier(raw) {
		hash := normalizeHash(raw)
		l.mu.RLock()
		module := l.byHash[hash]
		l.mu.RUnlock()
		if module == nil {
			return empty, fmt.Errorf("strategy loader: hash %q not found", hash)
		}
		return ModuleResolution{
			Name:   module.Name,
			Hash:   hash,
			Tag:    pickDefaultTag(module),
			Module: module,
		}, nil
	}

	var (
		namePart = raw
		tagPart  string
		hashPart string
	)

	if at := strings.Index(raw, "@"); at >= 0 {
		namePart = raw[:at]
		hashPart = raw[at+1:]
	} else if colon := strings.LastIndex(raw, ":"); colon >= 0 {
		namePart = raw[:colon]
		tagPart = raw[colon+1:]
	}

	name := strings.ToLower(strings.TrimSpace(namePart))
	tag := strings.TrimSpace(tagPart)
	hash := strings.TrimSpace(hashPart)
	if name == "" {
		return empty, fmt.Errorf("strategy loader: strategy name required")
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	var module *Module
	var resolvedHash string
	resolvedTag := tag

	switch {
	case hash != "":
		normalized := normalizeHash(hash)
		module = l.byHash[normalized]
		if module == nil {
			return empty, fmt.Errorf("strategy loader: hash %q not found for %s", normalized, name)
		}
		if !strings.EqualFold(module.Name, name) {
			return empty, fmt.Errorf("strategy loader: hash %q belongs to %s", normalized, module.Name)
		}
		resolvedHash = normalized
		if resolvedTag == "" {
			resolvedTag = pickDefaultTag(module)
		}
	case tag != "":
		var err error
		module, resolvedHash, err = l.resolveTagLocked(name, tag)
		if err != nil {
			return empty, err
		}
		resolvedTag = tag
	default:
		module = l.byName[name]
		if module == nil {
			return empty, ErrModuleNotFound
		}
		resolvedHash = module.Hash
		resolvedTag = pickDefaultTag(module)
	}

	if module == nil {
		return empty, ErrModuleNotFound
	}

	return ModuleResolution{
		Name:   module.Name,
		Hash:   resolvedHash,
		Tag:    resolvedTag,
		Module: module,
	}, nil
}

// Get returns the in-memory module definition for instantiation.
func (l *Loader) Get(name string) (*Module, error) {
	identifier := strings.TrimSpace(name)
	if identifier == "" {
		return nil, ErrModuleNotFound
	}

	if isHashIdentifier(identifier) {
		normalized := normalizeHash(identifier)
		l.mu.RLock()
		module := l.byHash[normalized]
		l.mu.RUnlock()
		if module != nil {
			return module, nil
		}
		return nil, ErrModuleNotFound
	}

	if strings.ContainsAny(identifier, "@:") {
		if res, err := l.ResolveReference(identifier); err == nil && res.Module != nil {
			return res.Module, nil
		} else if err != nil {
			return nil, err
		}
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	lower := strings.ToLower(identifier)
	if module, ok := l.byName[lower]; ok {
		return module, nil
	}
	if module, ok := l.byHash[identifier]; ok {
		return module, nil
	}
	if module, ok := l.byHash[lower]; ok {
		return module, nil
	}
	return nil, ErrModuleNotFound
}

// Read returns the raw JavaScript source for the named strategy.
func (l *Loader) Read(name string) ([]byte, error) {
	module, err := l.Get(name)
	if err == nil {
		// #nosec G304
		source, readErr := os.ReadFile(module.Path)
		if readErr != nil {
			return nil, fmt.Errorf("strategy loader: read %q: %w", module.Path, readErr)
		}
		return source, nil
	}

	filename := strings.TrimSpace(name)
	if filename == "" {
		return nil, err
	}
	base := filepath.Base(filename)
	if !isJavaScriptFile(base) {
		return nil, err
	}
	target := filepath.Join(l.root, base)
	if !strings.HasPrefix(target, l.root+string(os.PathSeparator)) && target != l.root {
		return nil, fmt.Errorf("strategy loader: read %q outside root", filename)
	}
	// #nosec G304
	source, readErr := os.ReadFile(target)
	if readErr != nil {
		if errors.Is(readErr, fs.ErrNotExist) {
			return nil, ErrModuleNotFound
		}
		return nil, fmt.Errorf("strategy loader: read %q: %w", target, readErr)
	}
	return source, nil
}

// Delete removes the JavaScript source for the named strategy.
func (l *Loader) Delete(name string) error {
	if l == nil {
		return fmt.Errorf("strategy loader: nil receiver")
	}

	reg, err := loadRegistry(l.root)
	if err != nil {
		return fmt.Errorf("strategy loader: load registry: %w", err)
	}
	if reg != nil {
		return l.deleteWithRegistry(name, reg)
	}

	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return fmt.Errorf("strategy loader: strategy name required")
	}
	l.mu.RLock()
	module, ok := l.byName[lower]
	l.mu.RUnlock()
	if ok {
		if err := os.Remove(module.Path); err != nil {
			return fmt.Errorf("strategy loader: delete %q: %w", module.Path, err)
		}
		return nil
	}

	filename := strings.TrimSpace(name)
	if filename == "" {
		return ErrModuleNotFound
	}
	base := filepath.Base(filename)
	if !isJavaScriptFile(base) {
		return ErrModuleNotFound
	}
	target := filepath.Join(l.root, base)
	if !strings.HasPrefix(target, l.root+string(os.PathSeparator)) && target != l.root {
		return fmt.Errorf("strategy loader: delete %q outside root", filename)
	}
	if err := os.Remove(target); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrModuleNotFound
		}
		return fmt.Errorf("strategy loader: delete %q: %w", target, err)
	}
	return nil
}

// Write persists the provided JavaScript source to disk and validates compilation.
// If a module with the same strategy name exists it will be replaced once Refresh is called.
func (l *Loader) Write(filename string, source []byte) error {
	if l == nil {
		return fmt.Errorf("strategy loader: nil receiver")
	}

	reg, err := loadRegistry(l.root)
	if err != nil {
		return fmt.Errorf("strategy loader: load registry: %w", err)
	}
	if reg != nil {
		_, writeErr := l.writeModuleWithRegistry(source, ModuleWriteOptions{Filename: "", Tag: "", Aliases: nil, PromoteLatest: true}, reg)
		return writeErr
	}

	trimmed := strings.TrimSpace(filename)
	if trimmed == "" {
		return fmt.Errorf("strategy loader: filename required")
	}
	if !isJavaScriptFile(trimmed) {
		return fmt.Errorf("strategy loader: file %q must use .js extension", trimmed)
	}

	tempFile, err := os.CreateTemp(l.root, "strategy-*.js")
	if err != nil {
		return fmt.Errorf("strategy loader: create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	if _, err := tempFile.Write(source); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("strategy loader: write temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("strategy loader: close temp file: %w", err)
	}

	entry, err := os.Stat(tempPath)
	if err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("strategy loader: stat temp file: %w", err)
	}
	if _, err := compileModule(tempPath, entry); err != nil {
		_ = os.Remove(tempPath)
		return err
	}

	destPath := filepath.Join(l.root, filepath.Base(trimmed))
	if err := os.Rename(tempPath, destPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("strategy loader: persist %q: %w", destPath, err)
	}
	return nil
}

// Store persists a module revision using registry semantics and returns the resulting resolution.
func (l *Loader) Store(source []byte, opts ModuleWriteOptions) (ModuleResolution, error) {
	if l == nil {
		return ModuleResolution{}, fmt.Errorf("strategy loader: nil receiver")
	}
	reg, err := loadRegistry(l.root)
	if err != nil {
		return ModuleResolution{}, fmt.Errorf("strategy loader: load registry: %w", err)
	}
	if reg == nil {
		return ModuleResolution{}, ErrRegistryUnavailable
	}
	resolution, err := l.writeModuleWithRegistry(source, opts, reg)
	if err != nil {
		return ModuleResolution{}, err
	}
	return resolution, nil
}

func loadRegistry(root string) (registry, error) {
	path := filepath.Join(root, "registry.json")
	// #nosec G304 -- path is derived from controlled loader root and fixed filename
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("strategy loader: read registry %q: %w", path, err)
	}
	var reg registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("strategy loader: decode registry %q: %w", path, err)
	}
	return reg, nil
}

func collectTagsForHash(tags map[string]string, defaultTag string, hash string) []string {
	if len(tags) == 0 {
		if defaultTag == "" {
			return nil
		}
		return []string{defaultTag}
	}
	set := make(map[string]struct{}, len(tags)+1)
	for tag, candidate := range tags {
		if strings.TrimSpace(candidate) == hash {
			set[tag] = struct{}{}
		}
	}
	if defaultTag != "" {
		set[defaultTag] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for tag := range set {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func primaryTagForHash(tags map[string]string, hash string) string {
	if len(tags) == 0 {
		return ""
	}
	for tag, candidate := range tags {
		if strings.TrimSpace(candidate) == hash && !strings.EqualFold(tag, "latest") {
			return tag
		}
	}
	if latest, ok := tags["latest"]; ok && latest == hash {
		return "latest"
	}
	return ""
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return make(map[string]string)
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func loadModule(path string) (*Module, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("strategy loader: stat %q: %w", path, err)
	}
	return compileModule(path, info)
}

func (m *Module) toSummary(name string) ModuleSummary {
	clone := strategies.CloneMetadata(m.Metadata)
	return ModuleSummary{
		Name:       name,
		File:       m.Filename,
		Path:       m.Path,
		Hash:       m.Hash,
		Version:    m.Version,
		Tags:       append([]string(nil), m.Tags...),
		TagAliases: nil,
		Revisions:  nil,
		Size:       m.Size,
		Metadata:   clone,
	}
}

func isJavaScriptFile(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(lower, ".js") || strings.HasSuffix(lower, ".mjs")
}

func compileModule(fullPath string, info fs.FileInfo) (*Module, error) {
	// #nosec G304
	source, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("strategy loader: read %q: %w", fullPath, err)
	}
	program, err := goja.Compile(fullPath, string(source), true)
	if err != nil {
		return nil, fmt.Errorf("strategy loader: compile %q: %w", fullPath, err)
	}

	meta, err := extractMetadata(program)
	if err != nil {
		return nil, fmt.Errorf("strategy loader: %s: %w", fullPath, err)
	}
	sum := sha256.Sum256(source)
	hash := hex.EncodeToString(sum[:])

	module := &Module{
		Name:     strings.ToLower(strings.TrimSpace(meta.Name)),
		Filename: filepath.Base(fullPath),
		Path:     fullPath,
		Hash:     fmt.Sprintf("sha256:%s", hash),
		Version:  "",
		Tags:     nil,
		Metadata: meta,
		Program:  program,
		Size:     info.Size(),
	}
	module.Metadata.Name = module.Name
	if module.Metadata.Version != "" {
		module.Version = module.Metadata.Version
	}
	return module, nil
}

func extractMetadata(program *goja.Program) (strategies.Metadata, error) {
	rt := goja.New()
	rt.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	exports, err := runModule(rt, program)
	if err != nil {
		return strategies.Metadata{}, err
	}
	raw := exports.Get("metadata")
	if goja.IsUndefined(raw) || goja.IsNull(raw) {
		return strategies.Metadata{}, fmt.Errorf("metadata export missing")
	}

	var meta strategies.Metadata
	if err := rt.ExportTo(raw, &meta); err != nil {
		return strategies.Metadata{}, fmt.Errorf("metadata export invalid: %w", err)
	}
	meta.Name = strings.ToLower(strings.TrimSpace(meta.Name))
	if meta.Name == "" {
		return strategies.Metadata{}, fmt.Errorf("metadata name required")
	}
	return strategies.CloneMetadata(meta), nil
}

func runModule(rt *goja.Runtime, program *goja.Program) (*goja.Object, error) {
	module := rt.NewObject()
	exports := rt.NewObject()
	if err := module.Set("exports", exports); err != nil {
		return nil, fmt.Errorf("module init: %w", err)
	}
	if err := rt.Set("exports", exports); err != nil {
		return nil, fmt.Errorf("module init: %w", err)
	}
	if err := rt.Set("module", module); err != nil {
		return nil, fmt.Errorf("module init: %w", err)
	}

	if err := rt.Set("console", buildConsole(rt)); err != nil {
		return nil, fmt.Errorf("module init: %w", err)
	}

	if _, err := rt.RunProgram(program); err != nil {
		return nil, fmt.Errorf("strategy loader: execute module: %w", err)
	}

	value := module.Get("exports")
	object := value.ToObject(rt)
	if object == nil {
		return nil, fmt.Errorf("module exports must be an object")
	}
	return object, nil
}

func buildConsole(rt *goja.Runtime) *goja.Object {
	console := rt.NewObject()
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	_ = console.Set("log", noop)
	_ = console.Set("error", noop)
	_ = console.Set("warn", noop)
	_ = console.Set("info", noop)
	return console
}

func (l *Loader) writeModuleWithRegistry(source []byte, opts ModuleWriteOptions, reg registry) (ModuleResolution, error) {
	var empty ModuleResolution
	if reg == nil {
		reg = make(registry)
	}

	tempFile, err := os.CreateTemp(l.root, "strategy-*.js")
	if err != nil {
		return empty, fmt.Errorf("strategy loader: create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	if _, err := tempFile.Write(source); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return empty, fmt.Errorf("strategy loader: write temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return empty, fmt.Errorf("strategy loader: close temp file: %w", err)
	}

	info, err := os.Stat(tempPath)
	if err != nil {
		_ = os.Remove(tempPath)
		return empty, fmt.Errorf("strategy loader: stat temp file: %w", err)
	}

	module, err := compileModule(tempPath, info)
	if err != nil {
		_ = os.Remove(tempPath)
		return empty, err
	}

	name := strings.ToLower(strings.TrimSpace(module.Metadata.Name))
	if name == "" {
		_ = os.Remove(tempPath)
		return empty, fmt.Errorf("strategy loader: metadata name required")
	}
	if err := validatePathSegment(name); err != nil {
		_ = os.Remove(tempPath)
		return empty, fmt.Errorf("strategy loader: %w", err)
	}

	tag := strings.TrimSpace(opts.Tag)
	if tag == "" {
		tag = strings.TrimSpace(module.Metadata.Version)
	}
	if tag == "" {
		_ = os.Remove(tempPath)
		return empty, fmt.Errorf("strategy loader: metadata version required for registry writes")
	}
	if err := validatePathSegment(tag); err != nil {
		_ = os.Remove(tempPath)
		return empty, fmt.Errorf("strategy loader: %w", err)
	}

	hash := module.Hash
	if hash == "" {
		_ = os.Remove(tempPath)
		return empty, fmt.Errorf("strategy loader: hash missing for module %s", name)
	}

	entry := reg[name]
	if entry.Tags == nil {
		entry.Tags = make(map[string]string)
	}
	if entry.Hashes == nil {
		entry.Hashes = make(map[string]registryLocation)
	}

	if existingHash, ok := entry.Tags[tag]; ok && existingHash != hash {
		_ = os.Remove(tempPath)
		return empty, fmt.Errorf("strategy loader: tag %q already exists for %s (hash %s)", tag, name, existingHash)
	}

	dir := filepath.Join(l.root, name, tag)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		_ = os.Remove(tempPath)
		return empty, fmt.Errorf("strategy loader: ensure directory %q: %w", dir, err)
	}
	destPath := filepath.Join(dir, fmt.Sprintf("%s.js", name))

	if _, err := os.Stat(destPath); err == nil {
		if err := os.Remove(destPath); err != nil {
			_ = os.Remove(tempPath)
			return empty, fmt.Errorf("strategy loader: replace %q: %w", destPath, err)
		}
	}
	if err := os.Rename(tempPath, destPath); err != nil {
		_ = os.Remove(tempPath)
		return empty, fmt.Errorf("strategy loader: persist %q: %w", destPath, err)
	}

	relPath, err := filepath.Rel(l.root, destPath)
	if err != nil {
		return empty, fmt.Errorf("strategy loader: relative path: %w", err)
	}

	entry.Tags[tag] = hash
	entry.Hashes[hash] = registryLocation{
		Tag:  tag,
		Path: filepath.ToSlash(relPath),
	}

	for _, alias := range opts.Aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" || alias == tag {
			continue
		}
		if err := validatePathSegment(alias); err != nil {
			continue
		}
		if existingHash, ok := entry.Tags[alias]; ok && existingHash != hash {
			continue
		}
		entry.Tags[alias] = hash
	}

	if opts.PromoteLatest || entry.Tags["latest"] == "" {
		entry.Tags["latest"] = hash
	}

	reg[name] = entry

	if err := writeRegistryFile(l.root, reg); err != nil {
		return empty, err
	}

	module.Path = destPath
	module.Filename = fmt.Sprintf("%s.js", name)
	module.Metadata.Version = tag
	module.Version = tag

	return ModuleResolution{
		Name:   name,
		Hash:   hash,
		Tag:    tag,
		Module: module,
	}, nil
}

func writeRegistryFile(root string, reg registry) error {
	path := filepath.Join(root, "registry.json")
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("strategy loader: marshal registry: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("strategy loader: write registry: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("strategy loader: replace registry: %w", err)
	}
	return nil
}

func validatePathSegment(segment string) error {
	if segment == "" {
		return fmt.Errorf("invalid path segment")
	}
	if strings.Contains(segment, "..") {
		return fmt.Errorf("invalid path segment %q", segment)
	}
	if strings.ContainsAny(segment, `/\`) {
		return fmt.Errorf("invalid path segment %q", segment)
	}
	return nil
}

func (l *Loader) deleteWithRegistry(selector string, reg registry) error {
	sel, err := parseModuleSelector(selector)
	if err != nil {
		return err
	}
	if sel.Name == "" {
		return fmt.Errorf("strategy loader: selector name required")
	}

	entry, ok := reg[sel.Name]
	if !ok {
		return ErrModuleNotFound
	}

	removeHash := ""
	if sel.Hash != "" {
		removeHash = normalizeHash(sel.Hash)
		if _, ok := entry.Hashes[removeHash]; !ok {
			return fmt.Errorf("strategy loader: hash %q not found for %s", removeHash, sel.Name)
		}
	} else if sel.Tag != "" {
		hash, ok := entry.Tags[sel.Tag]
		if !ok {
			return fmt.Errorf("strategy loader: tag %q not found for %s", sel.Tag, sel.Name)
		}
		removeHash = hash
		delete(entry.Tags, sel.Tag)
	} else {
		// Remove entire strategy.
		for hash := range entry.Hashes {
			if err := l.removeModuleFiles(entry.Hashes[hash]); err != nil {
				return err
			}
		}
		delete(reg, sel.Name)
		return writeRegistryFile(l.root, reg)
	}

	loc, ok := entry.Hashes[removeHash]
	if !ok {
		return fmt.Errorf("strategy loader: hash %q not found for %s", removeHash, sel.Name)
	}

	// Drop any tags that point to this hash.
	for tag, hash := range entry.Tags {
		if hash == removeHash {
			delete(entry.Tags, tag)
		}
	}

	if err := l.removeModuleFiles(loc); err != nil {
		return err
	}
	delete(entry.Hashes, removeHash)

	if len(entry.Hashes) == 0 {
		delete(reg, sel.Name)
	} else {
		// Reassign latest if necessary.
		if entry.Tags["latest"] == removeHash {
			entry.Tags["latest"] = pickReplacementLatest(entry.Tags)
		}
		reg[sel.Name] = entry
	}
	return writeRegistryFile(l.root, reg)
}

func (l *Loader) removeModuleFiles(loc registryLocation) error {
	if loc.Path == "" {
		return nil
	}
	fullPath := filepath.Join(l.root, filepath.Clean(loc.Path))
	if !strings.HasPrefix(fullPath, l.root+string(os.PathSeparator)) && fullPath != l.root {
		return fmt.Errorf("strategy loader: delete %q outside root", loc.Path)
	}
	if err := os.Remove(fullPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("strategy loader: delete module %q: %w", fullPath, err)
	}
	dir := filepath.Dir(fullPath)
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
	return nil
}

type moduleSelector struct {
	Name string
	Tag  string
	Hash string
}

func parseModuleSelector(selector string) (moduleSelector, error) {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		return moduleSelector{Name: "", Tag: "", Hash: ""}, fmt.Errorf("strategy loader: selector required")
	}

	if at := strings.Index(trimmed, "@"); at >= 0 {
		return moduleSelector{
			Name: strings.ToLower(strings.TrimSpace(trimmed[:at])),
			Tag:  "",
			Hash: strings.TrimSpace(trimmed[at+1:]),
		}, nil
	}

	if colon := strings.LastIndex(trimmed, ":"); colon >= 0 {
		return moduleSelector{
			Name: strings.ToLower(strings.TrimSpace(trimmed[:colon])),
			Tag:  strings.TrimSpace(trimmed[colon+1:]),
			Hash: "",
		}, nil
	}

	return moduleSelector{
		Name: strings.ToLower(trimmed),
		Tag:  "",
		Hash: "",
	}, nil
}

func pickReplacementLatest(tags map[string]string) string {
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
		return ""
	}
	sort.Strings(candidates)
	return tags[candidates[0]]
}

func (l *Loader) resolveTagLocked(name, tag string) (*Module, string, error) {
	if name == "" {
		return nil, "", fmt.Errorf("strategy loader: strategy name required")
	}
	if strings.TrimSpace(tag) == "" {
		return nil, "", fmt.Errorf("strategy loader: tag required for %s", name)
	}

	if tagsByName, ok := l.tags[name]; ok {
		if rawHash, ok := tagsByName[tag]; ok {
			hash := normalizeHash(rawHash)
			if module, ok := l.byHash[hash]; ok {
				return module, hash, nil
			}
			return nil, "", fmt.Errorf("strategy loader: tag %q for %s references unknown hash %q", tag, name, rawHash)
		}
	}

	if strings.EqualFold(tag, "latest") {
		if module, ok := l.byName[name]; ok {
			return module, module.Hash, nil
		}
	}
	return nil, "", fmt.Errorf("strategy loader: tag %q not found for %s", tag, name)
}

func normalizeHash(hash string) string {
	trimmed := strings.TrimSpace(strings.ToLower(hash))
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, ":") {
		return trimmed
	}
	if len(trimmed) == 64 && isHex(trimmed) {
		return "sha256:" + trimmed
	}
	return trimmed
}

func isHashIdentifier(identifier string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(identifier))
	if strings.HasPrefix(trimmed, "sha256:") {
		digest := strings.TrimPrefix(trimmed, "sha256:")
		return len(digest) == 64 && isHex(digest)
	}
	if len(trimmed) == 64 && isHex(trimmed) {
		return true
	}
	return false
}

func isHex(text string) bool {
	for _, r := range text {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

func pickDefaultTag(module *Module) string {
	if module == nil {
		return ""
	}
	for _, tag := range module.Tags {
		if strings.EqualFold(tag, "latest") {
			return tag
		}
	}
	if module.Version != "" {
		return module.Version
	}
	if len(module.Tags) > 0 {
		return module.Tags[0]
	}
	return ""
}
