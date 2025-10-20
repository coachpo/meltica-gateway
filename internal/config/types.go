package config

import "strings"

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

func normalizeExchangeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
