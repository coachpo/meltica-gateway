// Package contracts defines internal API contracts for Event Distribution & Lifecycle Optimization
// This is a specification file, not production code.
//
//nolint:revive,exhaustruct,unused
package contracts

import (
	"context"
	"sync"
	"time"
)

// EventKind classifies events for filtering and delivery guarantees
type EventKind int

const (
	KindMarketData    EventKind = iota // May be filtered during routing flips
	KindExecReport                     // ALWAYS delivered (critical)
	KindControlAck                     // ALWAYS delivered (critical)
	KindControlResult                  // ALWAYS delivered (critical)
)

// IsCritical returns true if event must bypass version filtering
func (k EventKind) IsCritical() bool {
	return k == KindExecReport || k == KindControlAck || k == KindControlResult
}

// Event represents a canonical market data or order lifecycle message
type Event struct {
	TraceID        string      // Unique trace identifier for observability
	RoutingVersion uint64      // Current routing topology version (stamped by Orchestrator)
	Kind           EventKind   // Event classification
	Payload        interface{} // Type-specific data
	IngestTS       time.Time   // Provider ingestion timestamp
	SeqProvider    uint64      // Provider sequence number for ordering
	ProviderID     string      // Source provider identifier
}

// Reset clears event fields for pool reuse
func (e *Event) Reset() {
	e.TraceID = ""
	e.RoutingVersion = 0
	e.Kind = 0
	e.Payload = nil
	e.IngestTS = time.Time{}
	e.SeqProvider = 0
	e.ProviderID = ""
}

// MergedEvent represents a merged canonical event from multiple providers
type MergedEvent struct {
	Event                    // Embedded base event
	SourceProviders []string // List of providers merged
	MergeWindowID   string   // Window identifier for tracing
}

// ExecReport represents an execution report for order lifecycle
type ExecReport struct {
	TraceID       string
	ClientOrderID string
	ExchangeID    string
	Status        string
	// ... additional fields
}

// RecyclerMetrics tracks observability counters for the Recycler
type RecyclerMetrics struct {
	EventsRecycledTotal  map[EventKind]uint64 // Counter by event type
	DoublePutsDetected   uint64               // Counter of double-put violations
	RecycleDurationNanos []time.Duration      // Histogram data
}

// Recycler provides centralized resource return gateway for all event structures
type Recycler interface {
	// RecycleEvent resets event fields, poisons if debug mode, returns to pool
	// Panics if double-put detected in debug mode
	RecycleEvent(ev *Event)

	// RecycleMergedEvent resets merged event and returns to pool
	RecycleMergedEvent(mev *MergedEvent)

	// RecycleExecReport resets execution report and returns to pool
	RecycleExecReport(er *ExecReport)

	// RecycleMany bulk recycles events (optimized for Orchestrator partial cleanup)
	RecycleMany(events []*Event)

	// EnableDebugMode activates poisoning and double-put tracking
	EnableDebugMode()

	// DisableDebugMode deactivates debug features (production mode)
	DisableDebugMode()

	// Metrics returns current observability counters
	Metrics() RecyclerMetrics
}

// RecyclerImpl is the concrete implementation (singleton)
type RecyclerImpl struct {
	eventPool       *sync.Pool
	mergedEventPool *sync.Pool
	execReportPool  *sync.Pool
	debugMode       bool
	putTracker      sync.Map // map[unsafe.Pointer]struct{} for double-put detection
	metrics         RecyclerMetrics
}

// NewRecycler creates a new Recycler instance
// pools: map of pool name -> sync.Pool for different event types
// debugMode: enable poisoning and double-put tracking
func NewRecycler(pools map[string]*sync.Pool, debugMode bool) Recycler {
	return &RecyclerImpl{
		eventPool:       pools["event"],
		mergedEventPool: pools["merged"],
		execReportPool:  pools["execreport"],
		debugMode:       debugMode,
		putTracker:      sync.Map{},
		metrics:         RecyclerMetrics{EventsRecycledTotal: make(map[EventKind]uint64)},
	}
}

func (r *RecyclerImpl) RecycleEvent(ev *Event)              {}
func (r *RecyclerImpl) RecycleMergedEvent(mev *MergedEvent) {}
func (r *RecyclerImpl) RecycleExecReport(er *ExecReport)    {}
func (r *RecyclerImpl) RecycleMany(events []*Event)         {}
func (r *RecyclerImpl) EnableDebugMode()                    {}
func (r *RecyclerImpl) DisableDebugMode()                   {}
func (r *RecyclerImpl) Metrics() RecyclerMetrics            { return r.metrics }

// Global singleton instance (initialized at startup)
var GlobalRecycler Recycler

// ConsumerFunc is the lambda function signature for event consumers
type ConsumerFunc func(ctx context.Context, ev *Event) error

// ConsumerMetrics tracks per-consumer observability
type ConsumerMetrics struct {
	InvocationsTotal     uint64          // Counter of lambda invocations
	ProcessingDurationNs []time.Duration // Histogram data
	PanicsTotal          uint64          // Counter of panic recoveries
	FilteredEventsTotal  uint64          // Counter of version-filtered events
}

// ConsumerWrapper wraps consumer lambda functions for auto-recycle and filtering
type ConsumerWrapper interface {
	// Invoke executes lambda with auto-recycle and version filtering
	// Returns error from lambda or panic recovery
	Invoke(ctx context.Context, ev *Event, lambda ConsumerFunc) error

	// UpdateMinVersion atomically updates minimum acceptable routing version
	UpdateMinVersion(version uint64)

	// ShouldProcess checks if event should be processed (version + critical kind logic)
	ShouldProcess(ev *Event) bool

	// Metrics returns current consumer observability counters
	Metrics() ConsumerMetrics
}

// ConsumerWrapperImpl is the concrete implementation
type ConsumerWrapperImpl struct {
	consumerID       string
	minAcceptVersion uint64 // Atomic access via sync/atomic
	recycler         Recycler
	metrics          ConsumerMetrics
}

// NewConsumerWrapper creates a new consumer wrapper
func NewConsumerWrapper(consumerID string, recycler Recycler) ConsumerWrapper {
	return &ConsumerWrapperImpl{
		consumerID:       consumerID,
		minAcceptVersion: 0,
		recycler:         recycler,
		metrics:          ConsumerMetrics{},
	}
}

func (c *ConsumerWrapperImpl) Invoke(ctx context.Context, ev *Event, lambda ConsumerFunc) error {
	return nil
}

func (c *ConsumerWrapperImpl) UpdateMinVersion(version uint64) {}

func (c *ConsumerWrapperImpl) ShouldProcess(ev *Event) bool { return true }

func (c *ConsumerWrapperImpl) Metrics() ConsumerMetrics { return c.metrics }

// DispatcherFanout defines the fan-out interface for parallel event delivery
type DispatcherFanout interface {
	// DeliverParallel delivers event to multiple subscribers concurrently
	// Returns aggregated errors from all deliveries
	// Recycles original event after all duplicates sent
	DeliverParallel(ctx context.Context, original *Event, subscribers []string) error

	// CreateDuplicate allocates a new event from pool and clones original
	CreateDuplicate(original *Event) *Event
}

// OrchestratorMerge defines the merge interface with Recycler integration
type OrchestratorMerge interface {
	// MergeEvents creates merged event from partials
	// Recycles partials immediately via Recycler after merge
	// Stamps current RoutingVersion on merged event
	MergeEvents(ctx context.Context, partials []*Event) (*MergedEvent, error)

	// StampRoutingVersion sets current routing version on event
	StampRoutingVersion(ev *Event)
}
