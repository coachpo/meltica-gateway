package config

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LambdaManifest declares the lambda instances Meltica should materialise at startup.
type LambdaManifest struct {
	Lambdas []LambdaSpec `yaml:"lambdas"`
}

// ProviderAssignment defines the symbol scope supplied by a provider.
type ProviderAssignment struct {
	Symbols []string `yaml:"symbols" json:"symbols"`
}

func (a *ProviderAssignment) normalize() {
	if a == nil {
		return
	}
	if len(a.Symbols) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(a.Symbols))
	out := make([]string, 0, len(a.Symbols))
	for _, symbol := range a.Symbols {
		normalized := strings.ToUpper(strings.TrimSpace(symbol))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	a.Symbols = out
}

func (a ProviderAssignment) includes(symbol string) bool {
	if len(a.Symbols) == 0 {
		return true
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return false
	}
	for _, candidate := range a.Symbols {
		if strings.EqualFold(candidate, symbol) {
			return true
		}
	}
	return false
}

// LambdaSpec defines a lambda instance configuration.
type LambdaSpec struct {
	ID                  string                        `yaml:"id" json:"id"`
	Providers           []string                      `yaml:"-" json:"providers"`
	ProviderAssignments map[string]ProviderAssignment `yaml:"-" json:"provider_assignments,omitempty"`
	Symbol              string                        `yaml:"symbol" json:"symbol"`
	Strategy            string                        `yaml:"strategy" json:"strategy"`
	Config              map[string]any                `yaml:"config" json:"config"`
	AutoStart           bool                          `yaml:"auto_start" json:"auto_start"`
}

type lambdaSpecAlias struct {
	ID        string         `yaml:"id"`
	Symbol    string         `yaml:"symbol"`
	Strategy  string         `yaml:"strategy"`
	Config    map[string]any `yaml:"config"`
	AutoStart bool           `yaml:"auto_start"`
}

// UnmarshalYAML implements custom YAML decoding for LambdaSpec.
func (s *LambdaSpec) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}

	var aux lambdaSpecAlias
	if err := value.Decode(&aux); err != nil {
		return fmt.Errorf("decode lambda spec: %w", err)
	}

	var providersNode yaml.Node
	for i := 0; i < len(value.Content)-1; i += 2 {
		keyNode := value.Content[i]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(keyNode.Value), "providers") {
			providersNode = *value.Content[i+1]
			break
		}
	}

	var providerNames []string
	providerAssignments := make(map[string]ProviderAssignment)

	switch providersNode.Kind {
	case 0:
		// No providers field supplied. Leave empty and let validation catch it.
		providerNames = nil
	case yaml.SequenceNode:
		var entries []string
		if err := providersNode.Decode(&entries); err != nil {
			return fmt.Errorf("providers: %w", err)
		}
		providerNames = normalizeProviderNames(entries)
	case yaml.MappingNode:
		raw := make(map[string]ProviderAssignment)
		if err := providersNode.Decode(&raw); err != nil {
			return fmt.Errorf("providers: %w", err)
		}
		for name, assignment := range raw {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" {
				continue
			}
			assignment.normalize()
			providerAssignments[trimmed] = assignment
		}
		for name := range providerAssignments {
			providerNames = append(providerNames, name)
		}
		providerNames = normalizeProviderNames(providerNames)
	case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode:
		return fmt.Errorf("providers must be a sequence or mapping")
	}

	s.ID = aux.ID
	s.Symbol = aux.Symbol
	s.Strategy = aux.Strategy
	s.Config = aux.Config
	s.AutoStart = aux.AutoStart
	s.ProviderAssignments = providerAssignments
	s.Providers = providerNames
	s.refreshProviders()
	return nil
}

// refreshProviders re-derives the provider list from assignments and symbol scope.
func (s *LambdaSpec) refreshProviders() {
	if s == nil {
		return
	}
	if len(s.ProviderAssignments) == 0 {
		s.Providers = normalizeProviderNames(s.Providers)
		return
	}
	symbol := strings.ToUpper(strings.TrimSpace(s.Symbol))
	providerNames := make([]string, 0, len(s.ProviderAssignments))
	for name, assignment := range s.ProviderAssignments {
		assignment.normalize()
		s.ProviderAssignments[name] = assignment
		if symbol == "" || assignment.includes(symbol) {
			providerNames = append(providerNames, name)
		}
	}
	s.Providers = normalizeProviderNames(providerNames)
}

// ProviderSymbols returns the symbol assignments for a provider, if any.
func (s LambdaSpec) ProviderSymbols(provider string) []string {
	name := strings.TrimSpace(provider)
	if name == "" {
		return nil
	}
	if len(s.Providers) > 0 {
		found := false
		for _, entry := range s.Providers {
			if strings.EqualFold(entry, name) {
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	assignment, ok := s.ProviderAssignments[name]
	if !ok {
		return nil
	}
	cloned := make([]string, len(assignment.Symbols))
	copy(cloned, assignment.Symbols)
	return cloned
}

// RefreshProviders re-evaluates provider membership based on assignments and symbol scope.
func (s *LambdaSpec) RefreshProviders() {
	s.refreshProviders()
}

// ProviderSymbolMap returns the provider-to-symbol mapping for this spec.
func (s LambdaSpec) ProviderSymbolMap() map[string][]string {
	out := make(map[string][]string, len(s.ProviderAssignments))
	for name, assignment := range s.ProviderAssignments {
		normalizedName := strings.TrimSpace(name)
		if normalizedName == "" {
			continue
		}
		if len(assignment.Symbols) == 0 {
			out[normalizedName] = nil
			continue
		}
		symbols := make([]string, 0, len(assignment.Symbols))
		for _, symbol := range assignment.Symbols {
			normalized := strings.ToUpper(strings.TrimSpace(symbol))
			if normalized == "" {
				continue
			}
			symbols = append(symbols, normalized)
		}
		if len(symbols) == 0 {
			out[normalizedName] = nil
			continue
		}
		out[normalizedName] = symbols
	}
	if trimmed := strings.ToUpper(strings.TrimSpace(s.Symbol)); trimmed != "" {
		for _, provider := range s.Providers {
			name := strings.TrimSpace(provider)
			if name == "" {
				continue
			}
			if _, ok := out[name]; !ok {
				out[name] = []string{trimmed}
			}
		}
	}
	return out
}

// AllSymbols returns the unique set of symbols referenced by the spec.
func (s LambdaSpec) AllSymbols() []string {
	unique := make(map[string]struct{})
	for _, symbols := range s.ProviderSymbolMap() {
		for _, symbol := range symbols {
			if symbol == "" {
				continue
			}
			unique[symbol] = struct{}{}
		}
	}
	if trimmed := strings.ToUpper(strings.TrimSpace(s.Symbol)); trimmed != "" {
		unique[trimmed] = struct{}{}
	}
	out := make([]string, 0, len(unique))
	for symbol := range unique {
		out = append(out, symbol)
	}
	sort.Strings(out)
	return out
}

func normalizeProviderNames(providers []string) []string {
	if len(providers) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(providers))
	out := make([]string, 0, len(providers))
	for _, provider := range providers {
		candidate := strings.TrimSpace(provider)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	sort.Strings(out)
	return out
}

// Validate performs semantic validation of the manifest definition.
func (m LambdaManifest) Validate() error {
	if len(m.Lambdas) == 0 {
		return fmt.Errorf("lambda manifest requires at least one lambda")
	}
	for i := range m.Lambdas {
		spec := &m.Lambdas[i]
		spec.refreshProviders()
		if strings.TrimSpace(spec.ID) == "" {
			return fmt.Errorf("lambdas[%d]: id required", i)
		}
		if strings.TrimSpace(spec.Symbol) == "" {
			if len(spec.ProviderAssignments) == 0 {
				return fmt.Errorf("lambdas[%d]: symbol required when provider assignments absent", i)
			}
			hasSymbols := false
			for _, assignment := range spec.ProviderAssignments {
				if len(assignment.Symbols) > 0 {
					hasSymbols = true
					break
				}
			}
			if !hasSymbols {
				return fmt.Errorf("lambdas[%d]: at least one provider symbol required", i)
			}
		}
		if strings.TrimSpace(spec.Strategy) == "" {
			return fmt.Errorf("lambdas[%d]: strategy required", i)
		}
		if len(spec.Providers) == 0 {
			return fmt.Errorf("lambdas[%d]: providers required", i)
		}
		for j, provider := range spec.Providers {
			name := strings.TrimSpace(provider)
			if name == "" {
				return fmt.Errorf("lambdas[%d].providers[%d]: provider name required", i, j)
			}
			if strings.TrimSpace(spec.Symbol) != "" && len(spec.ProviderAssignments) > 0 {
				if assignment, ok := spec.ProviderAssignments[name]; ok {
					if len(assignment.Symbols) > 0 && !assignment.includes(spec.Symbol) {
						return fmt.Errorf("lambdas[%d].providers[%q]: symbol %q not declared for provider", i, name, spec.Symbol)
					}
				}
			}
		}
	}
	return nil
}
