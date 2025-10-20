package config

import (
	"fmt"
	"strings"
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
