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

// LambdaStrategySpec defines the strategy identifier and associated configuration payload.
type LambdaStrategySpec struct {
	Identifier string         `yaml:"identifier" json:"identifier"`
	Config     map[string]any `yaml:"config" json:"config"`
}

func (s *LambdaStrategySpec) normalize() {
	if s == nil {
		return
	}
	s.Identifier = strings.TrimSpace(s.Identifier)
	if s.Config == nil {
		s.Config = make(map[string]any)
	}
}

// Normalize applies canonical formatting to the strategy definition.
func (s *LambdaStrategySpec) Normalize() {
	s.normalize()
}

// ProviderSymbols defines the symbol scope supplied by a provider.
type ProviderSymbols struct {
	Symbols []string `yaml:"symbols" json:"symbols"`
}

func (p *ProviderSymbols) normalize() {
	if p == nil {
		return
	}
	if len(p.Symbols) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(p.Symbols))
	out := make([]string, 0, len(p.Symbols))
	for _, symbol := range p.Symbols {
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
	p.Symbols = out
}

// Normalize applies canonical formatting to symbol entries.
func (p *ProviderSymbols) Normalize() {
	p.normalize()
}

// LambdaSpec defines a lambda instance configuration.
type LambdaSpec struct {
	ID              string                     `yaml:"id" json:"id"`
	Strategy        LambdaStrategySpec         `yaml:"strategy" json:"strategy"`
	ProviderSymbols map[string]ProviderSymbols `yaml:"scope" json:"scope"`
	Providers       []string                   `yaml:"-" json:"-"`
}

// UnmarshalYAML implements custom YAML decoding for LambdaSpec.
func (s *LambdaSpec) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}

	var base struct {
		ID       string             `yaml:"id"`
		Strategy LambdaStrategySpec `yaml:"strategy"`
	}
	if err := value.Decode(&base); err != nil {
		return fmt.Errorf("decode lambda spec: %w", err)
	}

	var providersNode *yaml.Node
	for i := 0; i < len(value.Content)-1; i += 2 {
		keyNode := value.Content[i]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(keyNode.Value), "scope") {
			providersNode = value.Content[i+1]
			break
		}
	}
	if providersNode == nil {
		return fmt.Errorf("scope: mapping required")
	}
	if providersNode.Kind != yaml.MappingNode {
		return fmt.Errorf("scope must be a mapping")
	}

	assignments := make(map[string]ProviderSymbols, len(providersNode.Content)/2)
	names := make([]string, 0, len(providersNode.Content)/2)
	for i := 0; i < len(providersNode.Content)-1; i += 2 {
		keyNode := providersNode.Content[i]
		valNode := providersNode.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("scope[%d]: provider name must be a scalar", i/2)
		}
		name := strings.TrimSpace(keyNode.Value)
		if name == "" {
			return fmt.Errorf("scope[%d]: provider name required", i/2)
		}
		var assignment ProviderSymbols
		if err := valNode.Decode(&assignment); err != nil {
			return fmt.Errorf("scope[%s]: %w", name, err)
		}
		assignment.normalize()
		if _, exists := assignments[name]; exists {
			return fmt.Errorf("scope[%s]: duplicate provider entry", name)
		}
		assignments[name] = assignment
		names = append(names, name)
	}

	s.ID = base.ID
	base.Strategy.normalize()
	s.Strategy = base.Strategy
	s.ProviderSymbols = assignments
	s.Providers = normalizeProviderNames(names)
	return nil
}

// refreshProviders re-derives the provider list from assignments.
func (s *LambdaSpec) refreshProviders() {
	if s == nil {
		return
	}
	s.Strategy.normalize()
	if len(s.ProviderSymbols) == 0 {
		s.Providers = normalizeProviderNames(s.Providers)
		return
	}
	names := make([]string, 0, len(s.ProviderSymbols))
	for name, assignment := range s.ProviderSymbols {
		assignment.normalize()
		s.ProviderSymbols[name] = assignment
		names = append(names, name)
	}
	s.Providers = normalizeProviderNames(names)
}

// SymbolsForProvider returns the symbol assignments for a provider, if any.
func (s LambdaSpec) SymbolsForProvider(provider string) []string {
	name := strings.TrimSpace(provider)
	if name == "" {
		return nil
	}
	assignment, ok := s.ProviderSymbols[name]
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
	out := make(map[string][]string, len(s.ProviderSymbols))
	for name, assignment := range s.ProviderSymbols {
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
		return nil
	}
	for i := range m.Lambdas {
		spec := &m.Lambdas[i]
		spec.refreshProviders()
		if strings.TrimSpace(spec.ID) == "" {
			return fmt.Errorf("lambdas[%d]: id required", i)
		}
		if strings.TrimSpace(spec.Strategy.Identifier) == "" {
			return fmt.Errorf("lambdas[%d]: strategy required", i)
		}
		if len(spec.Providers) == 0 {
			return fmt.Errorf("lambdas[%d]: providers required", i)
		}
		if len(spec.ProviderSymbols) == 0 {
			return fmt.Errorf("lambdas[%d]: scope mapping required", i)
		}
		for j, provider := range spec.Providers {
			name := strings.TrimSpace(provider)
			if name == "" {
				return fmt.Errorf("lambdas[%d].providers[%d]: provider name required", i, j)
			}
			assignment, ok := spec.ProviderSymbols[name]
			if !ok {
				return fmt.Errorf("lambdas[%d].providers[%q]: scope entry missing", i, name)
			}
			if len(assignment.Symbols) == 0 {
				return fmt.Errorf("lambdas[%d].providers[%q]: at least one symbol required", i, name)
			}
		}
		if len(spec.AllSymbols()) == 0 {
			return fmt.Errorf("lambdas[%d]: at least one symbol required", i)
		}
	}
	return nil
}
