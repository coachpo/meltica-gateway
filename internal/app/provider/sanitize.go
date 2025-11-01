package provider

import (
	"strings"

	"github.com/coachpo/meltica/internal/infra/config"
)

var sensitiveFragments = []string{
	"api_key",
	"apikey",
	"api-secret",
	"api_secret",
	"secret",
	"passphrase",
	"password",
	"token",
	"wssecret",
	"privatekey",
	"access_key",
	"accesskey",
	"client_secret",
}

// SanitizeConfig returns a copy of the provider configuration with sensitive fields removed.
func SanitizeConfig(cfg map[string]any) map[string]any {
	if len(cfg) == 0 {
		return nil
	}
	out := make(map[string]any, len(cfg))
	for key, value := range cfg {
		if isSensitiveConfigKey(key) {
			continue
		}
		switch typed := value.(type) {
		case map[string]any:
			nested := SanitizeConfig(typed)
			if len(nested) > 0 {
				out[key] = nested
			}
		case []any:
			nested := sanitizeSlice(typed)
			if len(nested) > 0 {
				out[key] = nested
			}
		default:
			out[key] = typed
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SanitizeSpec returns a sanitised copy of the provider specification with sensitive fields removed.
func SanitizeSpec(spec config.ProviderSpec) config.ProviderSpec {
	return config.ProviderSpec{
		Name:    spec.Name,
		Adapter: spec.Adapter,
		Config:  SanitizeConfig(spec.Config),
	}
}

func sanitizeSlice(items []any) []any {
	if len(items) == 0 {
		return nil
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case map[string]any:
			nested := SanitizeConfig(typed)
			if len(nested) > 0 {
				out = append(out, nested)
			}
		case []any:
			nested := sanitizeSlice(typed)
			if len(nested) > 0 {
				out = append(out, nested)
			}
		default:
			out = append(out, typed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isSensitiveConfigKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return false
	}
	for _, fragment := range sensitiveFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}
