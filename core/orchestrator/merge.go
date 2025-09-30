// Package orchestrator coordinates event merging and routing helpers.
package orchestrator

import (
	"context"
	"sync"

	"github.com/coachpo/meltica/core/events"
	"github.com/coachpo/meltica/core/recycler"
)

// Merger coordinates aggregation of partial events into a merged representation.
type Merger struct {
	recycler   recycler.Recycler
	mergedPool *sync.Pool
}

// NewMerger constructs a merger with the provided pools and recycler gateway.
func NewMerger(mergedPool *sync.Pool, rec recycler.Recycler) *Merger {
	if mergedPool == nil {
		mergedPool = &sync.Pool{New: func() any {
			return &events.MergedEvent{} //nolint:exhaustruct
		}}
	}
	return &Merger{
		recycler:   rec,
		mergedPool: mergedPool,
	}
}

// MergeEvents combines the supplied partial events into a single merged event and recycles partials.
func (m *Merger) MergeEvents(_ context.Context, partials []*events.Event) (*events.MergedEvent, error) {
	if len(partials) == 0 {
		return nil, nil
	}
	merged := m.checkoutMerged()
	m.composeMerged(merged, partials)
	m.recyclePartials(partials)
	return merged, nil
}

func (m *Merger) checkoutMerged() *events.MergedEvent {
	value := m.mergedPool.Get()
	if mev, ok := value.(*events.MergedEvent); ok {
		m.checkoutMergedEvent(mev)
		return mev
	}
	return &events.MergedEvent{} //nolint:exhaustruct
}

func (m *Merger) composeMerged(dest *events.MergedEvent, partials []*events.Event) {
	if dest == nil {
		return
	}
	var base *events.Event
	providers := dest.SourceProviders[:0]
	for _, ev := range partials {
		if ev == nil {
			continue
		}
		if base == nil {
			base = ev
		}
		if ev.ProviderID != "" {
			providers = append(providers, ev.ProviderID)
		}
	}
	dest.SourceProviders = providers
	if base == nil {
		dest.Event.Reset()
		return
	}
	dest.Event = events.Event{
		TraceID:        base.TraceID,
		RoutingVersion: base.RoutingVersion,
		Kind:           base.Kind,
		Payload:        base.Payload,
		IngestTS:       base.IngestTS,
		SeqProvider:    base.SeqProvider,
		ProviderID:     base.ProviderID,
	}
}

func (m *Merger) recyclePartials(partials []*events.Event) {
	if m.recycler == nil {
		return
	}
	nonNil := make([]*events.Event, 0, len(partials))
	for _, ev := range partials {
		if ev != nil {
			nonNil = append(nonNil, ev)
		}
	}
	m.recycler.RecycleMany(nonNil)
}

func (m *Merger) checkoutMergedEvent(ev *events.MergedEvent) {
	if ev == nil {
		return
	}
	ev.Reset()
	if m.recycler != nil {
		m.recycler.CheckoutMergedEvent(ev)
	}
}

// RecycleMerged returns a merged event to the underlying pool via the recycler gateway.
func (m *Merger) RecycleMerged(ev *events.MergedEvent) {
	if ev == nil {
		return
	}
	if m.recycler != nil {
		m.recycler.RecycleMergedEvent(ev)
	}
	m.mergedPool.Put(ev)
}
