package config

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// RuntimeManifest declares the runtime components Meltica should materialise at startup.
type RuntimeManifest struct {
	Providers []ProviderSpec `yaml:"providers"`
	Lambdas   []LambdaSpec   `yaml:"lambdas"`
}

// ProviderSpec describes a single provider instance and its configuration payload.
type ProviderSpec struct {
	Name   string         `yaml:"name"`
	Type   string         `yaml:"type"`
	Config map[string]any `yaml:"config"`
}

// LambdaSpec defines a lambda instance configuration.
type LambdaSpec struct {
	ID        string         `yaml:"id"`
	Provider  string         `yaml:"provider"`
	Symbol    string         `yaml:"symbol"`
	Strategy  string         `yaml:"strategy"`
	Config    map[string]any `yaml:"config"`
	AutoStart bool           `yaml:"auto_start"`
}

// LoadRuntimeManifest loads the runtime manifest from disk.
func LoadRuntimeManifest(ctx context.Context, path string) (RuntimeManifest, error) {
	_ = ctx
	path = strings.TrimSpace(path)
	if path == "" {
		path = os.Getenv("MELTICA_MANIFEST")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		path = "config/runtime.yaml"
	}

	reader, closer, err := openManifestFile(path)
	if err != nil {
		return RuntimeManifest{}, err
	}
	defer closer()

	bytes, err := io.ReadAll(reader)
	if err != nil {
		return RuntimeManifest{}, fmt.Errorf("read runtime manifest: %w", err)
	}

	var manifest RuntimeManifest
	if err := yaml.Unmarshal(bytes, &manifest); err != nil {
		return RuntimeManifest{}, fmt.Errorf("unmarshal runtime manifest: %w", err)
	}

	if err := manifest.Validate(); err != nil {
		return RuntimeManifest{}, err
	}
	return manifest, nil
}

// Validate performs semantic validation of the manifest definition.
func (m RuntimeManifest) Validate() error {
	if len(m.Providers) == 0 {
		return fmt.Errorf("manifest requires at least one provider")
	}
	providerNames := make(map[string]struct{}, len(m.Providers))
	for i, spec := range m.Providers {
		if strings.TrimSpace(spec.Name) == "" {
			return fmt.Errorf("providers[%d]: name required", i)
		}
		if strings.TrimSpace(spec.Type) == "" {
			return fmt.Errorf("providers[%d]: type required", i)
		}
		if _, exists := providerNames[spec.Name]; exists {
			return fmt.Errorf("providers[%d]: duplicate provider name %q", i, spec.Name)
		}
		providerNames[spec.Name] = struct{}{}
	}

	for i, spec := range m.Lambdas {
		if strings.TrimSpace(spec.ID) == "" {
			return fmt.Errorf("lambdas[%d]: id required", i)
		}
		if strings.TrimSpace(spec.Provider) == "" {
			return fmt.Errorf("lambdas[%d]: provider required", i)
		}
		if strings.TrimSpace(spec.Symbol) == "" {
			return fmt.Errorf("lambdas[%d]: symbol required", i)
		}
		if strings.TrimSpace(spec.Strategy) == "" {
			return fmt.Errorf("lambdas[%d]: strategy required", i)
		}
		if _, ok := providerNames[spec.Provider]; !ok {
			return fmt.Errorf("lambdas[%d]: unknown provider %q", i, spec.Provider)
		}
	}
	return nil
}

func openManifestFile(path string) (io.Reader, func(), error) {
	candidates := []string{}
	seen := make(map[string]struct{})
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	add(path)
	for _, fallback := range []string{
		"config/runtime.yaml",
		"internal/config/runtime.yaml",
	} {
		add(fallback)
	}

	var lastErr error
	for _, candidate := range candidates {
		file, err := os.Open(candidate)
		if err == nil {
			return file, func() { _ = file.Close() }, nil
		}
		if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("open runtime manifest: %w", err)
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = os.ErrNotExist
	}
	return nil, nil, fmt.Errorf("open runtime manifest: %w", lastErr)
}
