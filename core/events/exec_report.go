package events

// ExecReport represents an execution report for order lifecycle tracking.
type ExecReport struct {
	TraceID       string
	ClientOrderID string
	ExchangeID    string
	Status        string
	Reason        string
}

// Reset clears the execution report for pool reuse.
func (e *ExecReport) Reset() {
	if e == nil {
		return
	}
	e.TraceID = ""
	e.ClientOrderID = ""
	e.ExchangeID = ""
	e.Status = ""
	e.Reason = ""
}
