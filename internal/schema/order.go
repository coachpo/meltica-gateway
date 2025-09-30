package schema

import "time"

// OrderRequest represents an order submission from a consumer.
type OrderRequest struct {
	returned      bool
	ClientOrderID string    `json:"client_order_id"`
	ConsumerID    string    `json:"consumer_id"`
	Provider      string    `json:"provider"`
	Symbol        string    `json:"symbol"`
	Side          TradeSide `json:"side"`
	OrderType     OrderType `json:"order_type"`
	Price         *string   `json:"price,omitempty"`
	Quantity      string    `json:"quantity"`
	Timestamp     time.Time `json:"timestamp"`
}

// Reset zeroes the order request for pool reuse.
func (o *OrderRequest) Reset() {
	if o == nil {
		return
	}
	o.ClientOrderID = ""
	o.ConsumerID = ""
	o.Provider = ""
	o.Symbol = ""
	o.Side = ""
	o.OrderType = ""
	o.Price = nil
	o.Quantity = ""
	o.Timestamp = time.Time{}
	o.returned = false
}

// SetReturned toggles the pool ownership flag.
func (o *OrderRequest) SetReturned(flag bool) {
	if o == nil {
		return
	}
	o.returned = flag
}

// IsReturned reports whether the request is in the pool.
func (o *OrderRequest) IsReturned() bool {
	if o == nil {
		return false
	}
	return o.returned
}

// ExecReport captures execution report events flowing through the system.
type ExecReport struct {
	returned        bool
	ClientOrderID   string
	ExchangeOrderID string
	Provider        string
	Symbol          string
	Status          ExecReportState
	FilledQty       string
	RemainingQty    string
	AvgPrice        string
	TransactTime    int64
	ReceivedAt      int64
	TraceID         string
	DecisionID      string
}

// Reset zeroes the execution report for pool reuse.
func (e *ExecReport) Reset() {
	if e == nil {
		return
	}
	e.ClientOrderID = ""
	e.ExchangeOrderID = ""
	e.Provider = ""
	e.Symbol = ""
	e.Status = ""
	e.FilledQty = ""
	e.RemainingQty = ""
	e.AvgPrice = ""
	e.TransactTime = 0
	e.ReceivedAt = 0
	e.TraceID = ""
	e.DecisionID = ""
	e.returned = false
}

// SetReturned toggles the pool ownership flag for the execution report.
func (e *ExecReport) SetReturned(flag bool) {
	if e == nil {
		return
	}
	e.returned = flag
}

// IsReturned reports whether the execution report is currently pooled.
func (e *ExecReport) IsReturned() bool {
	if e == nil {
		return false
	}
	return e.returned
}
