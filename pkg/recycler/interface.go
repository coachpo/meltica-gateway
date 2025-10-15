package recycler

import "github.com/coachpo/meltica/pkg/events"

// Recycler defines the centralized gateway for returning pooled resources.
type Recycler interface {
	RecycleEvent(ev *events.Event)
	RecycleExecReport(er *events.ExecReport)
	RecycleMany(events []*events.Event)
	EnableDebugMode()
	DisableDebugMode()
	CheckoutEvent(ev *events.Event)
	CheckoutExecReport(er *events.ExecReport)
}
