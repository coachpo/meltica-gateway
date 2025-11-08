package config

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LambdaStrategySpec defines the strategy identifier and associated configuration payload.
type LambdaStrategySpec struct {
	Identifier string         `yaml:"identifier" json:"identifier"`
	Config     map[string]any `yaml:"config" json:"config"`
	Selector   string         `yaml:"-" json:"selector,omitempty"`
	Tag        string         `yaml:"-" json:"tag,omitempty"`
	Hash       string         `yaml:"-" json:"hash,omitempty"`
	// LegacyVersion captures payloads that still send strategy.version so we can map it to Tag.
	LegacyVersion string `yaml:"-" json:"version,omitempty"`
}

func (s *LambdaStrategySpec) normalize() {
	if s == nil {
		return
	}
	s.Identifier = strings.TrimSpace(s.Identifier)
	if s.Config == nil {
		s.Config = make(map[string]any)
	}
	s.Selector = strings.TrimSpace(s.Selector)
	s.Tag = strings.TrimSpace(s.Tag)
	s.Hash = strings.TrimSpace(s.Hash)
	s.LegacyVersion = strings.TrimSpace(s.LegacyVersion)
	if s.Tag == "" {
		s.Tag = s.LegacyVersion
	}
	s.LegacyVersion = ""
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
		assignment.Normalize()
		if _, exists := assignments[name]; exists {
			return fmt.Errorf("scope[%s]: duplicate provider entry", name)
		}
		assignments[name] = assignment
		names = append(names, name)
	}

	s.ID = base.ID
	base.Strategy.Normalize()
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
	for provider, assignment := range s.ProviderSymbols {
		normalized := strings.TrimSpace(provider)
		if normalized == "" {
			continue
		}
		assignment.Normalize()
		s.ProviderSymbols[normalized] = assignment
		names = append(names, normalized)
	}
	s.Providers = normalizeProviderNames(names)
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
