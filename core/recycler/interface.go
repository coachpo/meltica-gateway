package recycler

import "github.com/coachpo/meltica/core/events"

// Recycler defines the centralized gateway for returning pooled resources.
type Recycler interface {
	RecycleEvent(ev *events.Event)
	RecycleMergedEvent(ev *events.MergedEvent)
	RecycleExecReport(er *events.ExecReport)
	RecycleMany(events []*events.Event)
	EnableDebugMode()
	DisableDebugMode()
	CheckoutEvent(ev *events.Event)
	CheckoutMergedEvent(ev *events.MergedEvent)
	CheckoutExecReport(er *events.ExecReport)
}
