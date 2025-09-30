package dispatcher

import (
	"container/heap"
	"fmt"
	"sync"
	"time"

	"github.com/coachpo/meltica/config"
	"github.com/coachpo/meltica/internal/schema"
)

// StreamKey uniquely identifies an ordering stream (provider, symbol, event type).
type StreamKey struct {
	Provider  string
	Symbol    string
	EventType schema.EventType
}

func (k StreamKey) String() string {
	return fmt.Sprintf("%s|%s|%s", k.Provider, k.Symbol, k.EventType)
}

// StreamOrdering buffers events per stream and releases them in order.
type StreamOrdering struct {
	cfg   config.StreamOrderingConfig
	clock func() time.Time

	mu      sync.RWMutex
	buffers map[StreamKey]*streamBuffer
}

// NewStreamOrdering constructs a stream ordering buffer with the provided configuration.
func NewStreamOrdering(cfg config.StreamOrderingConfig, clock func() time.Time) *StreamOrdering {
	if clock == nil {
		clock = time.Now
	}
	ordering := new(StreamOrdering)
	ordering.cfg = cfg
	ordering.clock = clock
	ordering.buffers = make(map[StreamKey]*streamBuffer)
	return ordering
}

// OnEvent inserts the event into the ordering buffer and returns any events ready for delivery along with a flag indicating whether the event was buffered.
func (o *StreamOrdering) OnEvent(evt *schema.Event) ([]*schema.Event, bool) {
	if evt == nil {
		return nil, false
	}
	key := StreamKey{Provider: evt.Provider, Symbol: evt.Symbol, EventType: evt.Type}

	o.mu.Lock()
	defer o.mu.Unlock()

	buf, ok := o.buffers[key]
	if !ok {
		buf = newStreamBuffer(o.cfg, key)
		o.buffers[key] = buf
	}

	ready, buffered := buf.add(o.clock(), evt)
	if buf.empty() {
		delete(o.buffers, key)
	}
	return ready, buffered
}

// Depth returns the number of buffered events awaiting release for the given stream.
func (o *StreamOrdering) Depth(key StreamKey) int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if buf, ok := o.buffers[key]; ok {
		return buf.len()
	}
	return 0
}

// Flush releases events that have exceeded the lateness tolerance across all streams.
func (o *StreamOrdering) Flush(now time.Time) []*schema.Event {
	o.mu.Lock()
	defer o.mu.Unlock()

	if now.IsZero() {
		now = o.clock()
	}

	var ready []*schema.Event
	for key, buf := range o.buffers {
		ready = append(ready, buf.flush(now)...)
		if buf.empty() {
			delete(o.buffers, key)
		}
	}
	return ready
}

type streamBuffer struct {
	key         StreamKey
	cfg         config.StreamOrderingConfig
	lastEmitted uint64
	events      eventHeap
}

type bufferedEvent struct {
	arrival time.Time
	event   *schema.Event
}

type eventHeap []*bufferedEvent

func newStreamBuffer(cfg config.StreamOrderingConfig, key StreamKey) *streamBuffer {
	buffer := new(streamBuffer)
	buffer.key = key
	buffer.cfg = cfg
	buffer.events = make(eventHeap, 0)
	return buffer
}

func (b *streamBuffer) add(now time.Time, evt *schema.Event) ([]*schema.Event, bool) {
	if evt.SeqProvider <= b.lastEmitted {
		return nil, false
	}
	heap.Push(&b.events, &bufferedEvent{arrival: now, event: evt})
	overflow := b.enforceMax()
	ready := b.releaseContiguous()
	if len(overflow) > 0 {
		ready = append(overflow, ready...)
	}
	return ready, true
}

func (b *streamBuffer) flush(now time.Time) []*schema.Event {
	var ready []*schema.Event
	tolerance := b.cfg.LatenessTolerance
	if tolerance <= 0 {
		tolerance = 50 * time.Millisecond
	}

	for b.events.Len() > 0 {
		be := b.events[0]
		if now.Sub(be.arrival) < tolerance {
			break
		}
		heap.Pop(&b.events)
		if be.event.SeqProvider <= b.lastEmitted {
			continue
		}
		ready = append(ready, be.event)
		b.lastEmitted = be.event.SeqProvider
	}
	if len(ready) == 0 {
		ready = b.releaseContiguous()
	}
	return ready
}

func (b *streamBuffer) releaseContiguous() []*schema.Event {
	var ready []*schema.Event
	for b.events.Len() > 0 {
		be := b.events[0]
		if be.event.SeqProvider != b.lastEmitted+1 {
			break
		}
		heap.Pop(&b.events)
		ready = append(ready, be.event)
		b.lastEmitted = be.event.SeqProvider
	}
	return ready
}

func (b *streamBuffer) empty() bool {
	return b.events.Len() == 0
}

func (b *streamBuffer) len() int {
	return b.events.Len()
}

func (b *streamBuffer) enforceMax() []*schema.Event {
	maxSize := b.cfg.MaxBufferSize
	if maxSize <= 0 {
		return nil
	}
	var released []*schema.Event
	for b.events.Len() > maxSize {
		be := heap.Pop(&b.events).(*bufferedEvent)
		if be.event.SeqProvider <= b.lastEmitted {
			continue
		}
		released = append(released, be.event)
		b.lastEmitted = be.event.SeqProvider
	}
	return released
}

func (h eventHeap) Len() int { return len(h) }

func (h eventHeap) Less(i, j int) bool {
	if h[i].event.SeqProvider != h[j].event.SeqProvider {
		return h[i].event.SeqProvider < h[j].event.SeqProvider
	}
	return h[i].arrival.Before(h[j].arrival)
}

func (h eventHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *eventHeap) Push(x any) {
	*h = append(*h, x.(*bufferedEvent))
}

func (h *eventHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func cloneEventForFanOut(evt *schema.Event) *schema.Event {
	return schema.CloneEvent(evt)
}
