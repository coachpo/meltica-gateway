package config

import (
	"strings"
	"unicode"
)

// Environment identifies the runtime environment where Meltica operates.
type Environment string

// Provider identifies a configured provider name in the application config.
type Provider string

const (
	// EnvDev marks the development environment.
	EnvDev Environment = "dev"
	// EnvStaging marks the staging environment.
	EnvStaging Environment = "staging"
	// EnvProd marks the production environment.
	EnvProd Environment = "prod"
)

func normalizeExchangeIdentifier(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeProviderName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}

	if strings.ContainsAny(trimmed, "-_/") {
		return strings.ToLower(trimmed)
	}

	if trimmed == strings.ToUpper(trimmed) {
		return trimmed
	}

	runes := []rune(trimmed)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}
