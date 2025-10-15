package events

// EventKind classifies events for filtering and delivery guarantees.
type EventKind int

const (
	// KindMarketData represents non-critical market data events.
	KindMarketData EventKind = iota
	// KindExecReport represents critical execution report events.
	KindExecReport
	// KindControlAck represents critical control acknowledgement events.
	KindControlAck
	// KindControlResult represents critical control result events.
	KindControlResult
)

// IsCritical reports whether the event must bypass routing version filtering.
func (k EventKind) IsCritical() bool {
	switch k {
	case KindExecReport, KindControlAck, KindControlResult:
		return true
	case KindMarketData:
		return false
	default:
		return false
	}
}

// String returns the symbolic name for the event kind.
func (k EventKind) String() string {
	switch k {
	case KindMarketData:
		return "market_data"
	case KindExecReport:
		return "exec_report"
	case KindControlAck:
		return "control_ack"
	case KindControlResult:
		return "control_result"
	default:
		return "unknown"
	}
}
