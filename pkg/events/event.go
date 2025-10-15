// Package events defines canonical event structures used across Meltica.
package events

import "time"

// Event represents a canonical event delivered through the Meltica pipeline.
type Event struct {
	TraceID        string
	RoutingVersion uint64
	Kind           EventKind
	Payload        any
	IngestTS       time.Time
	SeqProvider    uint64
	ProviderID     string
}

// Reset clears the event's fields for pool reuse.
func (e *Event) Reset() {
	if e == nil {
		return
	}
	e.TraceID = ""
	e.RoutingVersion = 0
	e.Kind = 0
	e.Payload = nil
	e.IngestTS = time.Time{}
	e.SeqProvider = 0
	e.ProviderID = ""
}
