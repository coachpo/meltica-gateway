package schema

import "time"

// OrderRequest represents an order submission from a consumer.
type OrderRequest struct {
	returned      bool
	ClientOrderID string    `json:"clientOrderId"`
	ConsumerID    string    `json:"consumerId"`
	Provider      string    `json:"provider"`
	Symbol        string    `json:"symbol"`
	Side          TradeSide `json:"side"`
	OrderType     OrderType `json:"orderType"`
	Price         *string   `json:"price,omitempty"`
	Quantity      string    `json:"quantity"`
	TIF           string    `json:"tif"`
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
	o.TIF = ""
	o.Timestamp = time.Time{}
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
