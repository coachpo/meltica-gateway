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

	"github.com/coachpo/meltica/internal/app/lambda/strategies"
)

// ErrModuleNotFound reports missing strategy modules.
var ErrModuleNotFound = errors.New("strategy module not found")

// Loader manages JavaScript strategy modules sourced from an external directory.
type Loader struct {
	mu     sync.RWMutex
	root   string
	files  map[string]*Module
	byName map[string]*Module
}

// Module encapsulates the compiled program and metadata for a strategy.
type Module struct {
	Name     string
	Filename string
	Path     string
	Hash     string
	Metadata strategies.Metadata
	Program  *goja.Program
	Size     int64
}

// ModuleSummary exposes immutable module details for control APIs.
type ModuleSummary struct {
	Name     string              `json:"name"`
	File     string              `json:"file"`
	Path     string              `json:"path"`
	Hash     string              `json:"hash"`
	Size     int64               `json:"size"`
	Metadata strategies.Metadata `json:"metadata"`
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
		mu:     sync.RWMutex{},
		root:   clean,
		files:  make(map[string]*Module),
		byName: make(map[string]*Module),
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
	select {
	case <-ctx.Done():
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("strategy loader: refresh canceled: %w", err)
		}
		return fmt.Errorf("strategy loader: refresh canceled")
	default:
	}

	entries, err := os.ReadDir(l.root)
	if err != nil {
		return fmt.Errorf("strategy loader: read directory %q: %w", l.root, err)
	}

	nextFiles := make(map[string]*Module)
	nextByName := make(map[string]*Module)

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("strategy loader: refresh canceled: %w", err)
			}
			return fmt.Errorf("strategy loader: refresh canceled")
		default:
		}

		if entry.IsDir() {
			continue
		}
		if !isJavaScriptFile(entry.Name()) {
			continue
		}
		fullPath := filepath.Join(l.root, entry.Name())
		module, err := compileModule(fullPath, entry)
		if err != nil {
			return fmt.Errorf("strategy loader: compile module %q: %w", fullPath, err)
		}
		lowerName := strings.ToLower(module.Name)
		if _, exists := nextByName[lowerName]; exists {
			return fmt.Errorf("strategy loader: duplicate strategy name %q", module.Name)
		}
		nextFiles[strings.ToLower(entry.Name())] = module
		nextByName[lowerName] = module
	}

	l.mu.Lock()
	l.files = nextFiles
	l.byName = nextByName
	l.mu.Unlock()

	return nil
}

// List returns the loaded module catalog.
func (l *Loader) List() []ModuleSummary {
	l.mu.RLock()
	defer l.mu.RUnlock()

	out := make([]ModuleSummary, 0, len(l.byName))
	for _, module := range l.byName {
		out = append(out, ModuleSummary{
			Name:     module.Name,
			File:     module.Filename,
			Path:     module.Path,
			Hash:     module.Hash,
			Size:     module.Size,
			Metadata: strategies.CloneMetadata(module.Metadata),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// Module returns the module metadata for the named strategy.
func (l *Loader) Module(name string) (ModuleSummary, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	module, ok := l.byName[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		var empty ModuleSummary
		return empty, ErrModuleNotFound
	}
	return ModuleSummary{
		Name:     module.Name,
		File:     module.Filename,
		Path:     module.Path,
		Hash:     module.Hash,
		Size:     module.Size,
		Metadata: strategies.CloneMetadata(module.Metadata),
	}, nil
}

// Get returns the in-memory module definition for instantiation.
func (l *Loader) Get(name string) (*Module, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	module, ok := l.byName[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, ErrModuleNotFound
	}
	return module, nil
}

// Read returns the raw JavaScript source for the named strategy.
func (l *Loader) Read(name string) ([]byte, error) {
	module, err := l.Get(name)
	if err == nil {
		// #nosec G304 -- module.Path is produced by compileModule which constrains it to files within loader root.
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
	// #nosec G304 -- target is constructed via filepath.Join using sanitized filename within loader root.
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
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return fmt.Errorf("strategy loader: strategy name required")
	}
	l.mu.RLock()
	module, ok := l.byName[lower]
	l.mu.RUnlock()
	if !ok {
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
	if err := os.Remove(module.Path); err != nil {
		return fmt.Errorf("strategy loader: delete %q: %w", module.Path, err)
	}
	return nil
}

// Write persists the provided JavaScript source to disk and validates compilation.
// If a module with the same strategy name exists it will be replaced once Refresh is called.
func (l *Loader) Write(filename string, source []byte) error {
	if l == nil {
		return fmt.Errorf("strategy loader: nil receiver")
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
	if _, err := compileModule(tempPath, fs.FileInfoToDirEntry(entry)); err != nil {
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

func isJavaScriptFile(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(lower, ".js") || strings.HasSuffix(lower, ".mjs")
}

func compileModule(fullPath string, entry fs.DirEntry) (*Module, error) {
	// #nosec G304 -- fullPath originates from os.ReadDir and filepath.Join within loader root.
	source, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("strategy loader: read %q: %w", fullPath, err)
	}
	prog, err := goja.Compile(fullPath, string(source), true)
	if err != nil {
		return nil, fmt.Errorf("strategy loader: compile %q: %w", fullPath, err)
	}

	meta, err := extractMetadata(prog)
	if err != nil {
		return nil, fmt.Errorf("strategy loader: %s: %w", fullPath, err)
	}

	sum := sha256.Sum256(source)
	hash := hex.EncodeToString(sum[:])

	info := Module{
		Name:     meta.Name,
		Filename: entry.Name(),
		Path:     fullPath,
		Hash:     hash,
		Metadata: meta,
		Program:  prog,
		Size:     fileSize(entry),
	}
	return &info, nil
}

func extractMetadata(program *goja.Program) (strategies.Metadata, error) {
	rt := goja.New()
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
	rt.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
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
		return nil, fmt.Errorf("module run: %w", err)
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

func fileSize(entry fs.DirEntry) int64 {
	info, err := entry.Info()
	if err != nil {
		return 0
	}
	return info.Size()
}
