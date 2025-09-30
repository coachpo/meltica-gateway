package observability

import "sync"

// DeadLetterQueue stores telemetry events that failed delivery.
type DeadLetterQueue struct {
	mu       sync.Mutex
	capacity int
	events   []TelemetryEvent
}

// NewDeadLetterQueue creates a DLQ with the provided capacity. Capacity <=0 implies unbounded.
func NewDeadLetterQueue(capacity int) *DeadLetterQueue {
	queue := new(DeadLetterQueue)
	queue.capacity = capacity
	queue.events = make([]TelemetryEvent, 0)
	return queue
}

// Offer records a telemetry event in the DLQ.
func (q *DeadLetterQueue) Offer(event TelemetryEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.capacity > 0 && len(q.events) >= q.capacity {
		// Drop oldest event to make space for new record.
		copy(q.events[0:], q.events[1:])
		q.events[len(q.events)-1] = cloneTelemetryEvent(event)
		return
	}
	q.events = append(q.events, cloneTelemetryEvent(event))
}

// Drain retrieves and clears all queued telemetry events.
func (q *DeadLetterQueue) Drain() []TelemetryEvent {
	q.mu.Lock()
	defer q.mu.Unlock()
	drained := make([]TelemetryEvent, len(q.events))
	copy(drained, q.events)
	q.events = q.events[:0]
	return drained
}

// Len returns the number of queued telemetry events.
func (q *DeadLetterQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.events)
}
