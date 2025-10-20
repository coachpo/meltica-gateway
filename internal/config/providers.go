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

// BuildProviderSpecs converts exchange entries from the application configuration into provider specifications.
func BuildProviderSpecs(exchanges map[Exchange]map[string]any) ([]ProviderSpec, error) {
	if len(exchanges) == 0 {
		return nil, fmt.Errorf("no exchanges defined in config")
	}

	specs := make([]ProviderSpec, 0, len(exchanges))
	for key, data := range exchanges {
		alias := strings.TrimSpace(string(key))
		if alias == "" {
			return nil, fmt.Errorf("exchange alias required")
		}
		if data == nil {
			return nil, fmt.Errorf("exchange %q configuration required", alias)
		}

		rawExchange, ok := data["exchange"]
		if !ok {
			return nil, fmt.Errorf("exchange %q missing exchange block", alias)
		}

		exchangeConfig, ok := rawExchange.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("exchange %q exchange block must be a map", alias)
		}

		rawName, ok := exchangeConfig["name"]
		if !ok {
			return nil, fmt.Errorf("exchange %q exchange.name required", alias)
		}
		nameStr, ok := rawName.(string)
		if !ok || strings.TrimSpace(nameStr) == "" {
			return nil, fmt.Errorf("exchange %q exchange.name must be non-empty string", alias)
		}

		canonical := normalizeExchangeName(nameStr)

		config := make(map[string]any, len(exchangeConfig))
		for k, v := range exchangeConfig {
			config[k] = v
		}

		specs = append(specs, ProviderSpec{
			Name:     alias,
			Exchange: canonical,
			Config:   config,
		})
	}
	return specs, nil
}
