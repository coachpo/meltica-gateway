// Package config centralises runtime configuration helpers for Meltica services.
package config

import (
	"strings"
	"time"
)

// Environment identifies the runtime environment where Meltica operates.
type Environment string

// Exchange names a supported exchange integration.
type Exchange string

const (
	// EnvDev marks the development environment.
	EnvDev Environment = "dev"
	// EnvStaging marks the staging environment.
	EnvStaging Environment = "staging"
	// EnvProd marks the production environment.
	EnvProd Environment = "prod"
)

// Credentials captures API credentials used for authenticated requests.
type Credentials struct {
	APIKey    string
	APISecret string
}

// WebsocketSettings configures websocket endpoints per exchange.
type WebsocketSettings struct {
	PublicURL  string
	PrivateURL string
}

// ExchangeSettings aggregates transport and credential configuration.
type ExchangeSettings struct {
	REST                  map[string]string
	Websocket             WebsocketSettings
	Credentials           Credentials
	HTTPTimeout           time.Duration
	HandshakeTimeout      time.Duration
	SymbolRefreshInterval time.Duration
}

// NewExchangeSettings constructs an empty exchange configuration with safe defaults.
func NewExchangeSettings() ExchangeSettings {
	return ExchangeSettings{
		REST: make(map[string]string),
		Websocket: WebsocketSettings{
			PublicURL:  "",
			PrivateURL: "",
		},
		Credentials: Credentials{
			APIKey:    "",
			APISecret: "",
		},
		HTTPTimeout:           0,
		HandshakeTimeout:      0,
		SymbolRefreshInterval: 0,
	}
}

// CloneExchangeSettings performs a deep copy of the exchange configuration.
func CloneExchangeSettings(src ExchangeSettings) ExchangeSettings {
	clone := src
	if src.REST != nil {
		clone.REST = make(map[string]string, len(src.REST))
		for k, v := range src.REST {
			clone.REST[k] = v
		}
	} else {
		clone.REST = make(map[string]string)
	}
	return clone
}

func normalizeExchangeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
