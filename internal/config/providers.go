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
		name := string(key)
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("exchange name required")
		}

		if data == nil {
			return nil, fmt.Errorf("exchange %q configuration required", name)
		}

		exchangeValue, ok := data["exchange"]
		if !ok {
			return nil, fmt.Errorf("exchange %q missing exchange field", name)
		}
		exchangeName, ok := exchangeValue.(string)
		if !ok || strings.TrimSpace(exchangeName) == "" {
			return nil, fmt.Errorf("exchange %q exchange must be non-empty string", name)
		}

        canonicalExchange := normalizeExchangeName(exchangeName)

		config := make(map[string]any, len(data)-1)
		for k, v := range data {
			if k == "exchange" {
				continue
			}
			config[k] = v
		}

		if _, ok := config["name"]; !ok {
			config["name"] = name
		}

		specs = append(specs, ProviderSpec{
			Name:     name,
			Exchange: canonicalExchange,
			Config:   config,
		})
	}
	return specs, nil
}
