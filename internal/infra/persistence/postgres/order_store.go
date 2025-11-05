package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/domain/orderstore"
	json "github.com/goccy/go-json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OrderStore persists order lifecycle information.
type OrderStore struct {
	pool *pgxpool.Pool
}

// NewOrderStore constructs an OrderStore backed by the provided pool.
func NewOrderStore(pool *pgxpool.Pool) *OrderStore {
	return &OrderStore{pool: pool}
}

const (
	orderInsertSQL = `
INSERT INTO orders (
    id,
    provider_id,
    strategy_instance_id,
    client_order_id,
    instrument,
    side,
    order_type,
    quantity,
    price,
    state,
    external_order_ref,
    placed_at,
    acknowledged_at,
    completed_at,
    metadata,
    created_at,
    updated_at
) 
VALUES (
    @id,
    (SELECT id FROM providers WHERE alias = @provider),
    (SELECT id FROM strategy_instances WHERE instance_id = @strategy_instance_id),
    @client_order_id,
    @instrument,
    @side,
    @order_type,
    @quantity,
    @price,
    @state,
    @external_ref,
    to_timestamp(@placed_at),
    NULL,
    NULL,
    @metadata::jsonb,
    NOW(),
    NOW()
)
ON CONFLICT (id) DO NOTHING;
`

	orderUpdateSQL = `
UPDATE orders
SET state = @state,
    acknowledged_at = COALESCE(to_timestamp(@ack_at), acknowledged_at),
    completed_at = COALESCE(to_timestamp(@done_at), completed_at),
    metadata = COALESCE(@metadata::jsonb, metadata),
    updated_at = NOW()
WHERE id = @id;
`

	executionUpsertSQL = `
INSERT INTO executions (
    order_id,
    provider_id,
    execution_id,
    fill_quantity,
    fill_price,
    fee,
    fee_asset,
    liquidity,
    traded_at,
    metadata,
    created_at
)
VALUES (
    @order_id,
    (SELECT id FROM providers WHERE alias = @provider),
    @execution_id,
    @quantity,
    @price,
    @fee,
    @fee_asset,
    @liquidity,
    to_timestamp(@traded_at),
    @metadata::jsonb,
    NOW()
)
ON CONFLICT (order_id, execution_id) DO UPDATE SET
    fill_quantity = EXCLUDED.fill_quantity,
    fill_price = EXCLUDED.fill_price,
    fee = EXCLUDED.fee,
    fee_asset = EXCLUDED.fee_asset,
    liquidity = EXCLUDED.liquidity,
    traded_at = EXCLUDED.traded_at,
    metadata = EXCLUDED.metadata,
    created_at = EXCLUDED.created_at;
`

	balanceUpsertSQL = `
INSERT INTO balances (
    provider_id,
    asset,
    total,
    available,
    snapshot_at,
    metadata,
    created_at,
    updated_at
)
VALUES (
    (SELECT id FROM providers WHERE alias = @provider),
    @asset,
    @total,
    @available,
    to_timestamp(@snapshot_at),
    @metadata::jsonb,
    NOW(),
    NOW()
)
ON CONFLICT (provider_id, asset, snapshot_at) DO UPDATE SET
    total = EXCLUDED.total,
    available = EXCLUDED.available,
    metadata = EXCLUDED.metadata,
    updated_at = NOW();
`

	orderSelectBase = `
SELECT
    o.id::text,
    p.alias,
    COALESCE(si.instance_id, ''),
    o.client_order_id,
    o.instrument,
    o.side,
    o.order_type,
    o.quantity::text,
    o.price::text,
    o.state,
    o.external_order_ref,
    o.placed_at,
    o.acknowledged_at,
    o.completed_at,
    o.metadata,
    o.created_at,
    o.updated_at
FROM orders o
JOIN providers p ON p.id = o.provider_id
LEFT JOIN strategy_instances si ON si.id = o.strategy_instance_id
`

	executionSelectBase = `
SELECT
    e.order_id::text,
    p.alias,
    COALESCE(si.instance_id, ''),
    e.execution_id,
    e.fill_quantity::text,
    e.fill_price::text,
    e.fee::text,
    e.fee_asset,
    e.liquidity,
    e.traded_at,
    e.metadata,
    e.created_at
FROM executions e
JOIN orders o ON o.id = e.order_id
JOIN providers p ON p.id = e.provider_id
LEFT JOIN strategy_instances si ON si.id = o.strategy_instance_id
`

	balanceSelectBase = `
SELECT
    p.alias,
    b.asset,
    b.total::text,
    b.available::text,
    b.snapshot_at,
    b.metadata,
    b.created_at,
    b.updated_at
FROM balances b
JOIN providers p ON p.id = b.provider_id
`

	defaultOrderLimit     = 50
	maxOrderLimit         = 500
	defaultExecutionLimit = 100
	defaultBalanceLimit   = 100
)

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type orderTx struct {
	tx    pgx.Tx
	store *OrderStore
}

func (s *OrderStore) ensurePool() (*pgxpool.Pool, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("order store: nil pool")
	}
	return s.pool, nil
}

func (s *OrderStore) createOrderWith(ctx context.Context, exec execer, order orderstore.Order) error {
	if strings.TrimSpace(order.ID) == "" {
		return fmt.Errorf("order store: order id required")
	}
	metadata, err := encodeMetadata(order.Metadata)
	if err != nil {
		return fmt.Errorf("order store: encode metadata: %w", err)
	}
	args := pgx.NamedArgs{
		"id":                   order.ID,
		"provider":             strings.TrimSpace(order.Provider),
		"strategy_instance_id": strings.TrimSpace(order.StrategyInstance),
		"client_order_id":      order.ClientOrderID,
		"instrument":           order.Symbol,
		"side":                 strings.TrimSpace(order.Side),
		"order_type":           strings.TrimSpace(order.Type),
		"quantity":             order.Quantity,
		"price":                nullableText(order.Price),
		"state":                strings.TrimSpace(order.State),
		"external_ref":         nullableString(order.ExternalReference),
		"placed_at":            order.PlacedAt,
		"metadata":             metadata,
	}
	if _, err := exec.Exec(ctx, orderInsertSQL, args); err != nil {
		return fmt.Errorf("order store: insert order: %w", err)
	}
	return nil
}

func (s *OrderStore) updateOrderWith(ctx context.Context, exec execer, update orderstore.OrderUpdate) error {
	metadata, err := encodeMetadata(update.Metadata)
	if err != nil {
		return fmt.Errorf("order store: encode metadata: %w", err)
	}
	args := pgx.NamedArgs{
		"id":       strings.TrimSpace(update.ID),
		"state":    strings.TrimSpace(update.State),
		"ack_at":   nullableInt64(update.AcknowledgedAt),
		"done_at":  nullableInt64(update.CompletedAt),
		"metadata": metadata,
	}
	if _, err := exec.Exec(ctx, orderUpdateSQL, args); err != nil {
		return fmt.Errorf("order store: update order: %w", err)
	}
	return nil
}

func (s *OrderStore) recordExecutionWith(ctx context.Context, exec execer, execution orderstore.Execution) error {
	metadata, err := encodeMetadata(execution.Metadata)
	if err != nil {
		return fmt.Errorf("order store: encode metadata: %w", err)
	}
	args := pgx.NamedArgs{
		"order_id":     strings.TrimSpace(execution.OrderID),
		"provider":     strings.TrimSpace(execution.Provider),
		"execution_id": strings.TrimSpace(execution.ExecutionID),
		"quantity":     execution.Quantity,
		"price":        execution.Price,
		"fee":          nullableText(execution.Fee),
		"fee_asset":    nullableText(execution.FeeAsset),
		"liquidity":    strings.TrimSpace(execution.Liquidity),
		"traded_at":    execution.TradedAt,
		"metadata":     metadata,
	}
	if _, err := exec.Exec(ctx, executionUpsertSQL, args); err != nil {
		return fmt.Errorf("order store: upsert execution: %w", err)
	}
	return nil
}

func (s *OrderStore) upsertBalanceWith(ctx context.Context, exec execer, balance orderstore.BalanceSnapshot) error {
	metadata, err := encodeMetadata(balance.Metadata)
	if err != nil {
		return fmt.Errorf("order store: encode metadata: %w", err)
	}
	args := pgx.NamedArgs{
		"provider":    strings.TrimSpace(balance.Provider),
		"asset":       strings.TrimSpace(balance.Asset),
		"total":       balance.Total,
		"available":   balance.Available,
		"snapshot_at": balance.SnapshotAt,
		"metadata":    metadata,
	}
	if _, err := exec.Exec(ctx, balanceUpsertSQL, args); err != nil {
		return fmt.Errorf("order store: upsert balance: %w", err)
	}
	return nil
}

// CreateOrder inserts a new order snapshot.
func (s *OrderStore) CreateOrder(ctx context.Context, order orderstore.Order) error {
	pool, err := s.ensurePool()
	if err != nil {
		return err
	}
	return s.createOrderWith(ctx, pool, order)
}

// UpdateOrder updates order state details.
func (s *OrderStore) UpdateOrder(ctx context.Context, update orderstore.OrderUpdate) error {
	pool, err := s.ensurePool()
	if err != nil {
		return err
	}
	return s.updateOrderWith(ctx, pool, update)
}

// RecordExecution upserts an execution record.
func (s *OrderStore) RecordExecution(ctx context.Context, execution orderstore.Execution) error {
	pool, err := s.ensurePool()
	if err != nil {
		return err
	}
	return s.recordExecutionWith(ctx, pool, execution)
}

// UpsertBalance records balances snapshots.
func (s *OrderStore) UpsertBalance(ctx context.Context, balance orderstore.BalanceSnapshot) error {
	pool, err := s.ensurePool()
	if err != nil {
		return err
	}
	return s.upsertBalanceWith(ctx, pool, balance)
}

// WithTransaction executes the supplied callback within a database transaction.
func (s *OrderStore) WithTransaction(ctx context.Context, fn func(context.Context, orderstore.Tx) error) error {
	if fn == nil {
		return fmt.Errorf("order store: transaction callback required")
	}
	pool, err := s.ensurePool()
	if err != nil {
		return err
	}
	var txOptions pgx.TxOptions
	txOptions.IsoLevel = pgx.ReadCommitted
	txOptions.AccessMode = pgx.ReadWrite
	txOptions.DeferrableMode = pgx.NotDeferrable

	tx, err := pool.BeginTx(ctx, txOptions)
	if err != nil {
		return fmt.Errorf("order store: begin tx: %w", err)
	}
	wrapped := &orderTx{tx: tx, store: s}
	runErr := fn(ctx, wrapped)
	if runErr != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return fmt.Errorf("order store: rollback tx: %w (original error: %v)", rbErr, runErr)
		}
		return runErr
	}
	if err := tx.Commit(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return fmt.Errorf("order store: commit tx: %w", err)
	}
	return nil
}

// ListOrders retrieves persisted orders matching the supplied query filters.
func (s *OrderStore) ListOrders(ctx context.Context, query orderstore.OrderQuery) ([]orderstore.OrderRecord, error) {
	pool, err := s.ensurePool()
	if err != nil {
		return nil, err
	}
	limit := clampLimit(query.Limit, defaultOrderLimit, maxOrderLimit)

	builder := strings.Builder{}
	builder.WriteString(orderSelectBase)
	builder.WriteString(" WHERE 1=1")

	args := make([]any, 0, 4)
	argPos := 1

	if trimmed := strings.TrimSpace(query.StrategyInstance); trimmed != "" {
		fmt.Fprintf(&builder, " AND COALESCE(si.instance_id, '') = $%d", argPos)
		args = append(args, trimmed)
		argPos++
	}
	if trimmed := strings.TrimSpace(query.Provider); trimmed != "" {
		fmt.Fprintf(&builder, " AND p.alias = $%d", argPos)
		args = append(args, trimmed)
		argPos++
	}
	states := normalizedStates(query.States)
	if len(states) > 0 {
		fmt.Fprintf(&builder, " AND o.state = ANY($%d)", argPos)
		args = append(args, states)
		argPos++
	}
	fmt.Fprintf(&builder, " ORDER BY o.placed_at DESC LIMIT $%d", argPos)
	args = append(args, limit)

	rows, err := pool.Query(ctx, builder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("order store: list orders: %w", err)
	}
	defer rows.Close()

	var records []orderstore.OrderRecord
	for rows.Next() {
		var (
			id               string
			providerAlias    string
			instanceID       string
			clientOrderID    string
			instrument       string
			side             string
			orderType        string
			quantity         string
			priceValue       sql.NullString
			state            string
			externalRefValue sql.NullString
			placedAt         time.Time
			acknowledgedAt   pgtype.Timestamptz
			completedAt      pgtype.Timestamptz
			metadataBytes    []byte
			createdAt        time.Time
			updatedAt        time.Time
		)
		if err := rows.Scan(
			&id,
			&providerAlias,
			&instanceID,
			&clientOrderID,
			&instrument,
			&side,
			&orderType,
			&quantity,
			&priceValue,
			&state,
			&externalRefValue,
			&placedAt,
			&acknowledgedAt,
			&completedAt,
			&metadataBytes,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("order store: scan order: %w", err)
		}
		metadata, err := decodeMetadata(metadataBytes)
		if err != nil {
			return nil, err
		}
		record := orderstore.OrderRecord{
			Order: orderstore.Order{
				ID:                id,
				Provider:          providerAlias,
				StrategyInstance:  instanceID,
				ClientOrderID:     clientOrderID,
				Symbol:            instrument,
				Side:              side,
				Type:              orderType,
				Quantity:          quantity,
				Price:             nil,
				State:             state,
				ExternalReference: "",
				PlacedAt:          placedAt.Unix(),
				Metadata:          metadata,
			},
			AcknowledgedAt: nil,
			CompletedAt:    nil,
			CreatedAt:      createdAt.Unix(),
			UpdatedAt:      updatedAt.Unix(),
		}
		if priceValue.Valid {
			price := priceValue.String
			record.Price = &price
		}
		if externalRefValue.Valid {
			record.ExternalReference = externalRefValue.String
		}
		if acknowledgedAt.Valid {
			ack := acknowledgedAt.Time.Unix()
			record.AcknowledgedAt = &ack
		}
		if completedAt.Valid {
			done := completedAt.Time.Unix()
			record.CompletedAt = &done
		}
		if len(record.Metadata) == 0 {
			record.Metadata = nil
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("order store: iterate orders: %w", err)
	}

	return records, nil
}

// ListExecutions retrieves execution records matching the supplied query filters.
func (s *OrderStore) ListExecutions(ctx context.Context, query orderstore.ExecutionQuery) ([]orderstore.ExecutionRecord, error) {
	pool, err := s.ensurePool()
	if err != nil {
		return nil, err
	}
	limit := clampLimit(query.Limit, defaultExecutionLimit, maxOrderLimit)

	builder := strings.Builder{}
	builder.WriteString(executionSelectBase)
	builder.WriteString(" WHERE 1=1")

	args := make([]any, 0, 4)
	argPos := 1

	if trimmed := strings.TrimSpace(query.StrategyInstance); trimmed != "" {
		fmt.Fprintf(&builder, " AND COALESCE(si.instance_id, '') = $%d", argPos)
		args = append(args, trimmed)
		argPos++
	}
	if trimmed := strings.TrimSpace(query.Provider); trimmed != "" {
		fmt.Fprintf(&builder, " AND p.alias = $%d", argPos)
		args = append(args, trimmed)
		argPos++
	}
	if trimmed := strings.TrimSpace(query.OrderID); trimmed != "" {
		fmt.Fprintf(&builder, " AND e.order_id::text = $%d", argPos)
		args = append(args, trimmed)
		argPos++
	}
	fmt.Fprintf(&builder, " ORDER BY e.traded_at DESC LIMIT $%d", argPos)
	args = append(args, limit)

	rows, err := pool.Query(ctx, builder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("order store: list executions: %w", err)
	}
	defer rows.Close()

	var records []orderstore.ExecutionRecord
	for rows.Next() {
		var (
			orderID       string
			providerAlias string
			instanceID    string
			executionID   string
			quantity      string
			price         string
			feeValue      sql.NullString
			feeAssetValue sql.NullString
			liquidity     sql.NullString
			tradedAt      time.Time
			metadataBytes []byte
			createdAt     time.Time
		)
		if err := rows.Scan(
			&orderID,
			&providerAlias,
			&instanceID,
			&executionID,
			&quantity,
			&price,
			&feeValue,
			&feeAssetValue,
			&liquidity,
			&tradedAt,
			&metadataBytes,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("order store: scan execution: %w", err)
		}
		metadata, err := decodeMetadata(metadataBytes)
		if err != nil {
			return nil, err
		}
		record := orderstore.ExecutionRecord{
			Execution: orderstore.Execution{
				OrderID:     orderID,
				Provider:    providerAlias,
				ExecutionID: executionID,
				Quantity:    quantity,
				Price:       price,
				Fee:         nil,
				FeeAsset:    nil,
				Liquidity:   "",
				TradedAt:    tradedAt.Unix(),
				Metadata:    metadata,
			},
			StrategyInstance: instanceID,
			CreatedAt:        createdAt.Unix(),
		}
		if feeValue.Valid {
			fee := feeValue.String
			record.Fee = &fee
		}
		if feeAssetValue.Valid {
			asset := feeAssetValue.String
			record.FeeAsset = &asset
		}
		if liquidity.Valid {
			record.Liquidity = liquidity.String
		}
		if len(record.Metadata) == 0 {
			record.Metadata = nil
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("order store: iterate executions: %w", err)
	}

	return records, nil
}

// ListBalances retrieves balance snapshots matching the supplied query filters.
func (s *OrderStore) ListBalances(ctx context.Context, query orderstore.BalanceQuery) ([]orderstore.BalanceRecord, error) {
	pool, err := s.ensurePool()
	if err != nil {
		return nil, err
	}
	provider := strings.TrimSpace(query.Provider)
	if provider == "" {
		return nil, fmt.Errorf("order store: provider alias required")
	}
	limit := clampLimit(query.Limit, defaultBalanceLimit, maxOrderLimit)

	builder := strings.Builder{}
	builder.WriteString(balanceSelectBase)
	builder.WriteString(" WHERE p.alias = $1")

	args := []any{provider}
	argPos := 2

	if trimmed := strings.TrimSpace(query.Asset); trimmed != "" {
		fmt.Fprintf(&builder, " AND b.asset = $%d", argPos)
		args = append(args, trimmed)
		argPos++
	}
	fmt.Fprintf(&builder, " ORDER BY b.snapshot_at DESC LIMIT $%d", argPos)
	args = append(args, limit)

	rows, err := pool.Query(ctx, builder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("order store: list balances: %w", err)
	}
	defer rows.Close()

	var records []orderstore.BalanceRecord
	for rows.Next() {
		var (
			providerAlias string
			asset         string
			total         string
			available     string
			snapshotAt    time.Time
			metadataBytes []byte
			createdAt     time.Time
			updatedAt     time.Time
		)
		if err := rows.Scan(
			&providerAlias,
			&asset,
			&total,
			&available,
			&snapshotAt,
			&metadataBytes,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("order store: scan balance: %w", err)
		}
		metadata, err := decodeMetadata(metadataBytes)
		if err != nil {
			return nil, err
		}
		record := orderstore.BalanceRecord{
			BalanceSnapshot: orderstore.BalanceSnapshot{
				Provider:   providerAlias,
				Asset:      asset,
				Total:      total,
				Available:  available,
				SnapshotAt: snapshotAt.Unix(),
				Metadata:   metadata,
			},
			CreatedAt: createdAt.Unix(),
			UpdatedAt: updatedAt.Unix(),
		}
		if len(record.Metadata) == 0 {
			record.Metadata = nil
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("order store: iterate balances: %w", err)
	}

	return records, nil
}

func (t *orderTx) CreateOrder(ctx context.Context, order orderstore.Order) error {
	if t == nil {
		return fmt.Errorf("order store: nil transaction")
	}
	return t.store.createOrderWith(ctx, t.tx, order)
}

func (t *orderTx) UpdateOrder(ctx context.Context, update orderstore.OrderUpdate) error {
	if t == nil {
		return fmt.Errorf("order store: nil transaction")
	}
	return t.store.updateOrderWith(ctx, t.tx, update)
}

func (t *orderTx) RecordExecution(ctx context.Context, execution orderstore.Execution) error {
	if t == nil {
		return fmt.Errorf("order store: nil transaction")
	}
	return t.store.recordExecutionWith(ctx, t.tx, execution)
}

func (t *orderTx) UpsertBalance(ctx context.Context, balance orderstore.BalanceSnapshot) error {
	if t == nil {
		return fmt.Errorf("order store: nil transaction")
	}
	return t.store.upsertBalanceWith(ctx, t.tx, balance)
}

func encodeMetadata(meta map[string]any) ([]byte, error) {
	if len(meta) == 0 {
		return []byte("{}"), nil
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("order store: encode metadata: %w", err)
	}
	return data, nil
}

func nullableString(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func nullableText(ptr *string) any {
	if ptr == nil {
		return nil
	}
	return nullableString(*ptr)
}

func nullableInt64(ptr *int64) any {
	if ptr == nil {
		return nil
	}
	return *ptr
}

func clampLimit(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func normalizedStates(states []string) []string {
	if len(states) == 0 {
		return nil
	}
	out := make([]string, 0, len(states))
	for _, state := range states {
		trimmed := strings.ToUpper(strings.TrimSpace(state))
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decodeMetadata(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("order store: decode metadata: %w", err)
	}
	if len(meta) == 0 {
		return nil, nil
	}
	return meta, nil
}
