package config

import (
	"fmt"
	"strings"
)

// ProviderSpec describes a single provider instance and its configuration payload.
type ProviderSpec struct {
	Name     string
	Exchange string
	Config   map[string]any
}

// BuildProviderSpecs converts provider entries from the application configuration into provider specifications.
func BuildProviderSpecs(providers map[Provider]map[string]any) ([]ProviderSpec, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers defined in config")
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

		rawExchange, ok := data["exchange"]
		if !ok {
			return nil, fmt.Errorf("provider %q missing exchange block", name)
		}

		exchangeConfig, ok := rawExchange.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("provider %q exchange block must be a map", name)
		}

		rawIdentifier, ok := exchangeConfig["identifier"]
		if !ok {
			return nil, fmt.Errorf("provider %q exchange.identifier required", name)
		}
		identifierStr, ok := rawIdentifier.(string)
		if !ok || strings.TrimSpace(identifierStr) == "" {
			return nil, fmt.Errorf("provider %q exchange.identifier must be non-empty string", name)
		}

		canonical := normalizeExchangeIdentifier(identifierStr)

		config := make(map[string]any, len(exchangeConfig)+1)
		for k, v := range exchangeConfig {
			config[k] = v
		}
		config["provider_name"] = name

		specs = append(specs, ProviderSpec{
			Name:     name,
			Exchange: canonical,
			Config:   config,
		})
	}
	return specs, nil
}
