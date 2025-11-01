package config

import (
	"fmt"
	"strings"
)

// ProviderSpec describes a single provider instance and its configuration payload.
type ProviderSpec struct {
	Name    string
	Adapter string
	Config  map[string]any
}

// BuildProviderSpecs converts provider entries from the application configuration into provider specifications.
func BuildProviderSpecs(providers map[Provider]map[string]any) ([]ProviderSpec, error) {
	if len(providers) == 0 {
		return nil, nil
	}

	specs := make([]ProviderSpec, 0, len(providers))
	for key, data := range providers {
		name := strings.TrimSpace(string(key))
		if name == "" {
			return nil, fmt.Errorf("provider name required")
		}
		if data == nil {
			return nil, fmt.Errorf("provider %q configuration required", name)
		}

		rawAdapter, ok := data["adapter"]
		if !ok {
			return nil, fmt.Errorf("provider %q missing adapter block", name)
		}

		adapterConfig, ok := rawAdapter.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("provider %q adapter block must be a map", name)
		}

		rawIdentifier, ok := adapterConfig["identifier"]
		if !ok {
			return nil, fmt.Errorf("provider %q adapter.identifier required", name)
		}
		identifierStr, ok := rawIdentifier.(string)
		if !ok || strings.TrimSpace(identifierStr) == "" {
			return nil, fmt.Errorf("provider %q adapter.identifier must be non-empty string", name)
		}

		canonical := normalizeAdapterIdentifier(identifierStr)

		config := make(map[string]any, len(adapterConfig)+1)
		for k, v := range adapterConfig {
			config[k] = v
		}
		config["provider_name"] = name

		specs = append(specs, ProviderSpec{
			Name:    name,
			Adapter: canonical,
			Config:  config,
		})
	}
	return specs, nil
}

// ProviderSpecsToConfigMap converts provider specifications back into configuration map form.
func ProviderSpecsToConfigMap(specs []ProviderSpec) map[Provider]map[string]any {
	if len(specs) == 0 {
		return nil
	}

	out := make(map[Provider]map[string]any, len(specs))
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}

		adapter := make(map[string]any)
		if strings.TrimSpace(spec.Adapter) != "" {
			adapter["identifier"] = spec.Adapter
		}

		for key, value := range spec.Config {
			trimmed := strings.TrimSpace(key)
			lower := strings.ToLower(trimmed)
			switch lower {
			case "provider_name":
				continue
			case "identifier":
				adapter["identifier"] = cloneAny(value)
			default:
				adapter[trimmed] = cloneAny(value)
			}
		}

		out[Provider(name)] = map[string]any{
			"adapter": adapter,
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
