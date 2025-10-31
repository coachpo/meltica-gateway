package provider

import (
	"sort"

	"github.com/coachpo/meltica/internal/domain/schema"
)

// AdapterMetadata describes static metadata about a provider adapter.
type AdapterMetadata struct {
	Identifier     string           `json:"identifier"`
	DisplayName    string           `json:"displayName,omitempty"`
	Venue          string           `json:"venue,omitempty"`
	Description    string           `json:"description,omitempty"`
	Capabilities   []string         `json:"capabilities,omitempty"`
	SettingsSchema []AdapterSetting `json:"settingsSchema"`
}

// AdapterSetting details a user-configurable adapter parameter.
type AdapterSetting struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Default     any    `json:"default,omitempty"`
	Required    bool   `json:"required"`
}

// Clone returns a deep copy of the adapter metadata.
func (m AdapterMetadata) Clone() AdapterMetadata {
	clone := m
	clone.Capabilities = append([]string(nil), m.Capabilities...)
	clone.SettingsSchema = CloneAdapterSettings(m.SettingsSchema)
	return clone
}

// CloneAdapterSettings returns a shallow copy of the adapter settings slice.
func CloneAdapterSettings(settings []AdapterSetting) []AdapterSetting {
	if len(settings) == 0 {
		return nil
	}
	out := make([]AdapterSetting, len(settings))
	copy(out, settings)
	return out
}

// RuntimeMetadata summarizes a running provider instance.
type RuntimeMetadata struct {
	Name            string         `json:"name"`
	Exchange        string         `json:"exchange"`
	Identifier      string         `json:"identifier"`
	InstrumentCount int            `json:"instrumentCount"`
	Settings        map[string]any `json:"settings,omitempty"`
}

// RuntimeDetail contains the detailed metadata for a provider instance.
type RuntimeDetail struct {
	RuntimeMetadata
	Instruments     []schema.Instrument `json:"instruments"`
	AdapterMetadata AdapterMetadata     `json:"adapter"`
}

// CloneRuntimeMetadata returns a copy of the runtime metadata.
func CloneRuntimeMetadata(meta RuntimeMetadata) RuntimeMetadata {
	clone := meta
	if len(meta.Settings) > 0 {
		clone.Settings = make(map[string]any, len(meta.Settings))
		for k, v := range meta.Settings {
			clone.Settings[k] = v
		}
	}
	return clone
}

// CloneRuntimeDetail returns a copy of the runtime detail metadata.
func CloneRuntimeDetail(detail RuntimeDetail) RuntimeDetail {
	clone := detail
	clone.RuntimeMetadata = CloneRuntimeMetadata(detail.RuntimeMetadata)
	clone.Instruments = cloneInstruments(detail.Instruments)
	clone.AdapterMetadata = detail.AdapterMetadata.Clone()
	return clone
}

func cloneInstruments(instruments []schema.Instrument) []schema.Instrument {
	if len(instruments) == 0 {
		return nil
	}
	out := make([]schema.Instrument, len(instruments))
	for i, inst := range instruments {
		out[i] = schema.CloneInstrument(inst)
	}
	return out
}

// SortRuntimeMetadata sorts the slice in-place by provider name.
func SortRuntimeMetadata(meta []RuntimeMetadata) {
	sort.Slice(meta, func(i, j int) bool { return meta[i].Name < meta[j].Name })
}
