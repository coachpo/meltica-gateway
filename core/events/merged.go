package events

// MergedEvent represents an event produced by merging partial events from multiple providers.
type MergedEvent struct {
	Event
	SourceProviders []string
	MergeWindowID   string
}

// Reset clears the merged event for pool reuse.
func (m *MergedEvent) Reset() {
	if m == nil {
		return
	}
	m.Event.Reset()
	m.SourceProviders = nil
	m.MergeWindowID = ""
}
