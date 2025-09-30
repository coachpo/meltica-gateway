package schema

import json "github.com/goccy/go-json"

// ProviderRaw captures provider-specific payloads prior to normalization.
type ProviderRaw struct {
	returned   bool
	Provider   string
	StreamName string
	ReceivedAt int64
	Payload    json.RawMessage
}

// Reset zeroes the provider payload for pooling reuse.
func (p *ProviderRaw) Reset() {
	if p == nil {
		return
	}
	p.Provider = ""
	p.StreamName = ""
	p.ReceivedAt = 0
	p.Payload = nil
	p.returned = false
}

// SetReturned updates the pooled ownership flag.
func (p *ProviderRaw) SetReturned(flag bool) {
	if p == nil {
		return
	}
	p.returned = flag
}

// IsReturned reports whether the payload is currently pooled.
func (p *ProviderRaw) IsReturned() bool {
	if p == nil {
		return false
	}
	return p.returned
}
