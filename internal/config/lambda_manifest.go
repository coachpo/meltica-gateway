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

// LambdaManifest declares the lambda instances Meltica should materialise at startup.
type LambdaManifest struct {
	Lambdas []LambdaSpec `yaml:"lambdas"`
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

// LoadLambdaManifest loads the lambda manifest from disk.
func LoadLambdaManifest(ctx context.Context, path string) (LambdaManifest, error) {
	_ = ctx

	reader, closer, err := openLambdaManifest(path)
	if err != nil {
		return LambdaManifest{}, err
	}
	defer closer()

	bytes, err := io.ReadAll(reader)
	if err != nil {
		return LambdaManifest{}, fmt.Errorf("read lambda manifest: %w", err)
	}

	var manifest LambdaManifest
	if err := yaml.Unmarshal(bytes, &manifest); err != nil {
		return LambdaManifest{}, fmt.Errorf("unmarshal lambda manifest: %w", err)
	}

	if err := manifest.Validate(); err != nil {
		return LambdaManifest{}, err
	}
	return manifest, nil
}

// Validate performs semantic validation of the manifest definition.
func (m LambdaManifest) Validate() error {
	if len(m.Lambdas) == 0 {
		return fmt.Errorf("lambda manifest requires at least one lambda")
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
	}
	return nil
}

func openLambdaManifest(path string) (io.Reader, func(), error) {
	candidate := strings.TrimSpace(path)
	if candidate == "" {
		candidate = "config/lambda-manifest.yaml"
	}
	candidate = filepath.Clean(candidate)

	file, err := os.Open(candidate) // #nosec G304 -- path controlled by operator.
	if err != nil {
		return nil, nil, fmt.Errorf("open lambda manifest: %w", err)
	}
	return file, func() { _ = file.Close() }, nil
}
