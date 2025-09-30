package schema

// MergedEvent captures the orchestrated merge results across providers.
type MergedEvent struct {
	returned    bool
	MergeID     string
	Symbol      string
	EventType   EventType
	WindowOpen  int64
	WindowClose int64
	Fragments   []CanonicalEvent
	IsComplete  bool
	TraceID     string
}

// Reset zeroes the merged event for reuse.
func (m *MergedEvent) Reset() {
	if m == nil {
		return
	}
	m.MergeID = ""
	m.Symbol = ""
	m.EventType = ""
	m.WindowOpen = 0
	m.WindowClose = 0
	m.Fragments = nil
	m.IsComplete = false
	m.TraceID = ""
	m.returned = false
}

// SetReturned toggles the pool ownership flag for the merged event.
func (m *MergedEvent) SetReturned(flag bool) {
	if m == nil {
		return
	}
	m.returned = flag
}

// IsReturned reports whether the merged event is in the pool.
func (m *MergedEvent) IsReturned() bool {
	if m == nil {
		return false
	}
	return m.returned
}
