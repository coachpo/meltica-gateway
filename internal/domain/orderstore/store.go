// Package orderstore defines persistence contracts for order lifecycle state.
package orderstore

import "context"

// Order represents the persisted snapshot of an order submission.
type Order struct {
	ID                string         `json:"id"`
	Provider          string         `json:"provider"`
	StrategyInstance  string         `json:"strategyInstance"`
	ClientOrderID     string         `json:"clientOrderId"`
	Symbol            string         `json:"symbol"`
	Side              string         `json:"side"`
	Type              string         `json:"type"`
	Quantity          string         `json:"quantity"`
	Price             *string        `json:"price,omitempty"`
	State             string         `json:"state"`
	ExternalReference string         `json:"externalReference,omitempty"`
	PlacedAt          int64          `json:"placedAt"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

// OrderUpdate captures state transitions for an existing order.
type OrderUpdate struct {
	ID             string         `json:"id"`
	State          string         `json:"state"`
	AcknowledgedAt *int64         `json:"acknowledgedAt,omitempty"`
	CompletedAt    *int64         `json:"completedAt,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// Execution represents a trade fill associated with an order.
type Execution struct {
	OrderID     string         `json:"orderId"`
	Provider    string         `json:"provider"`
	ExecutionID string         `json:"executionId"`
	Quantity    string         `json:"quantity"`
	Price       string         `json:"price"`
	Fee         *string        `json:"fee,omitempty"`
	FeeAsset    *string        `json:"feeAsset,omitempty"`
	Liquidity   string         `json:"liquidity,omitempty"`
	TradedAt    int64          `json:"tradedAt"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// BalanceSnapshot captures account balance updates.
type BalanceSnapshot struct {
	Provider   string         `json:"provider"`
	Asset      string         `json:"asset"`
	Total      string         `json:"total"`
	Available  string         `json:"available"`
	SnapshotAt int64          `json:"snapshotAt"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// OrderRecord represents a stored order enriched with timestamps.
type OrderRecord struct {
	Order
	AcknowledgedAt *int64 `json:"acknowledgedAt,omitempty"`
	CompletedAt    *int64 `json:"completedAt,omitempty"`
	CreatedAt      int64  `json:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt"`
}

// ExecutionRecord represents a stored execution enriched with metadata.
type ExecutionRecord struct {
	Execution
	StrategyInstance string `json:"strategyInstance"`
	CreatedAt        int64  `json:"createdAt"`
}

// BalanceRecord represents a stored balance snapshot enriched with audit timestamps.
type BalanceRecord struct {
	BalanceSnapshot
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// OrderQuery scopes order lookups.
type OrderQuery struct {
	StrategyInstance string   `json:"strategyInstance"`
	Provider         string   `json:"provider,omitempty"`
	States           []string `json:"states,omitempty"`
	Limit            int      `json:"limit,omitempty"`
}

// ExecutionQuery scopes execution lookups.
type ExecutionQuery struct {
	StrategyInstance string `json:"strategyInstance"`
	Provider         string `json:"provider,omitempty"`
	OrderID          string `json:"orderId,omitempty"`
	Limit            int    `json:"limit,omitempty"`
}

// BalanceQuery scopes balance lookups.
type BalanceQuery struct {
	Provider string `json:"provider"`
	Asset    string `json:"asset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// Tx encapsulates order persistence operations executed within a single transaction.
type Tx interface {
	CreateOrder(ctx context.Context, order Order) error
	UpdateOrder(ctx context.Context, update OrderUpdate) error
	RecordExecution(ctx context.Context, execution Execution) error
	UpsertBalance(ctx context.Context, balance BalanceSnapshot) error
}

// Store defines the contract for order persistence operations.
type Store interface {
	Tx
	WithTransaction(ctx context.Context, fn func(context.Context, Tx) error) error
	ListOrders(ctx context.Context, query OrderQuery) ([]OrderRecord, error)
	ListExecutions(ctx context.Context, query ExecutionQuery) ([]ExecutionRecord, error)
	ListBalances(ctx context.Context, query BalanceQuery) ([]BalanceRecord, error)
}
