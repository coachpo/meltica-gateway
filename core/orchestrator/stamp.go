package orchestrator

import (
	"sync/atomic"

	"github.com/coachpo/meltica/core/events"
)

// Stamper manages routing version state and stamps events before dispatch.
type Stamper struct {
	current atomic.Uint64
}

// NewStamper constructs a Stamper with the specified initial routing version.
func NewStamper(initial uint64) *Stamper {
	s := &Stamper{} //nolint:exhaustruct
	s.current.Store(initial)
	return s
}

// UpdateVersion sets the current routing version for subsequent events.
func (s *Stamper) UpdateVersion(version uint64) {
	s.current.Store(version)
}

// CurrentVersion returns the presently configured routing version.
func (s *Stamper) CurrentVersion() uint64 {
	return s.current.Load()
}

// StampRoutingVersion assigns the current routing version to the supplied event.
func (s *Stamper) StampRoutingVersion(ev *events.Event) {
	if ev == nil {
		return
	}
	ev.RoutingVersion = s.CurrentVersion()
}
