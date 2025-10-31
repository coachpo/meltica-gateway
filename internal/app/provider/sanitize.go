package provider

import "strings"

var (
	providerSensitiveFragments = []string{
		"secret",
		"passphrase",
		"apikey",
		"wsapikey",
		"wssecret",
		"privatekey",
		"privkey",
		"token",
	}

	providerSettingReplacer = strings.NewReplacer("-", "", "_", "", " ", "")
)

// SanitizeProviderConfig returns a copy of the supplied configuration with sensitive keys removed.
func SanitizeProviderConfig(cfg map[string]any) map[string]any {
	if len(cfg) == 0 {
		return nil
	}
	clean := sanitizeProviderSettingsMap(cfg)
	if len(clean) == 0 {
		return nil
	}
	return clean
}

func sanitizeProviderSettingsMap(cfg map[string]any) map[string]any {
	if len(cfg) == 0 {
		return nil
	}
	clean := make(map[string]any)
	for key, value := range cfg {
		if shouldOmitProviderSettingKey(key) {
			continue
		}
		sanitized := sanitizeProviderSettingValue(value)
		if sanitized == nil {
			continue
		}
		clean[key] = sanitized
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

func sanitizeProviderSettingValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		clean := sanitizeProviderSettingsMap(v)
		if len(clean) == 0 {
			return nil
		}
		return clean
	case []any:
		filtered := make([]any, 0, len(v))
		for _, item := range v {
			if nested, ok := item.(map[string]any); ok {
				clean := sanitizeProviderSettingsMap(nested)
				if len(clean) == 0 {
					continue
				}
				filtered = append(filtered, clean)
				continue
			}
			filtered = append(filtered, item)
		}
		if len(filtered) == 0 {
			return nil
		}
		return filtered
	case []string:
		if len(v) == 0 {
			return []string{}
		}
		return append([]string(nil), v...)
	default:
		return value
	}
}

func shouldOmitProviderSettingKey(key string) bool {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return false
	}
	normalized := strings.ToLower(trimmed)
	normalized = providerSettingReplacer.Replace(normalized)
	for _, fragment := range providerSensitiveFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
