// Package strategies defines metadata structures for lambda strategies.
package strategies

import "github.com/coachpo/meltica/internal/domain/schema"

// ConfigField describes a configurable parameter for a strategy.
type ConfigField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Default     any    `json:"default,omitempty"`
	Required    bool   `json:"required"`
}

// Metadata captures descriptive information about a strategy.
type Metadata struct {
	Name        string             `json:"name"`
	Version     string             `json:"version,omitempty"`
	DisplayName string             `json:"displayName"`
	Description string             `json:"description,omitempty"`
	Config      []ConfigField      `json:"config"`
	Events      []schema.EventType `json:"events"`
}

var dryRunConfigField = ConfigField{
	Name:        "dry_run",
	Type:        "bool",
	Description: "When true, strategy logs intended orders without submitting them",
	Default:     true,
	Required:    false,
}

// WithDryRunField returns a new slice containing the provided fields and the dry_run field appended when absent.
func WithDryRunField(fields []ConfigField) []ConfigField {
	out := CloneConfigFields(fields)
	for _, field := range out {
		if field.Name == dryRunConfigField.Name {
			return out
		}
	}
	return append(out, dryRunConfigField)
}

// CloneConfigFields returns a shallow copy of the provided configuration fields.
func CloneConfigFields(fields []ConfigField) []ConfigField {
	if len(fields) == 0 {
		return nil
	}
	out := make([]ConfigField, len(fields))
	copy(out, fields)
	return out
}

// CloneMetadata returns a copy of the metadata with cloned slices.
func CloneMetadata(meta Metadata) Metadata {
	clone := meta
	clone.Config = CloneConfigFields(meta.Config)
	clone.Events = append([]schema.EventType(nil), meta.Events...)
	return clone
}
