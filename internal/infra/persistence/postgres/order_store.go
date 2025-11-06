package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/domain/orderstore"
	"github.com/coachpo/meltica/internal/infra/persistence/postgres/sqlc"
	json "github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OrderStore persists order lifecycle information.
type OrderStore struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

// NewOrderStore constructs an OrderStore backed by the provided pool.
func NewOrderStore(pool *pgxpool.Pool) *OrderStore {
	if pool == nil {
		return &OrderStore{
			pool:    nil,
			queries: nil,
		}
	}
	return &OrderStore{
		pool:    pool,
		queries: sqlc.New(pool),
	}
}

const (
	defaultOrderLimit     = 50
	maxOrderLimit         = 500
	defaultExecutionLimit = 100
	defaultBalanceLimit   = 100
)

type orderTx struct {
	tx      pgx.Tx
	store   *OrderStore
	queries *sqlc.Queries
}

func (s *OrderStore) ensurePool() (*pgxpool.Pool, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("order store: nil pool")
	}
	return s.pool, nil
}

func (s *OrderStore) ensureQueries() (*sqlc.Queries, error) {
	if s.pool == nil || s.queries == nil {
		return nil, fmt.Errorf("order store: nil pool")
	}
	return s.queries, nil
}

func (s *OrderStore) createOrderWith(ctx context.Context, queries *sqlc.Queries, order orderstore.Order) error {
	if queries == nil {
		return fmt.Errorf("order store: nil queries")
	}
	orderID, err := parseUUID(strings.TrimSpace(order.ID))
	if err != nil {
		return err
	}
	providerID, err := s.lookupProviderID(ctx, queries, order.Provider)
	if err != nil {
		return err
	}
	strategyID, err := s.lookupStrategyUUID(ctx, queries, order.StrategyInstance)
	if err != nil {
		return err
	}
	quantity, err := numericFromString(order.Quantity)
	if err != nil {
		return fmt.Errorf("order store: quantity: %w", err)
	}
	price, err := numericFromOptional(order.Price)
	if err != nil {
		return fmt.Errorf("order store: price: %w", err)
	}
	metadata, err := encodeMetadata(order.Metadata)
	if err != nil {
		return fmt.Errorf("order store: encode metadata: %w", err)
	}
	placedAt := order.PlacedAt
	if placedAt == 0 {
		placedAt = time.Now().Unix()
	}

	params := sqlc.InsertOrderParams{
		ID:                 orderID,
		ProviderID:         providerID,
		StrategyInstanceID: strategyID,
		ClientOrderID:      strings.TrimSpace(order.ClientOrderID),
		Instrument:         strings.TrimSpace(order.Symbol),
		Side:               strings.TrimSpace(order.Side),
		OrderType:          strings.TrimSpace(order.Type),
		Quantity:           quantity,
		Price:              price,
		State:              strings.TrimSpace(order.State),
		ExternalOrderRef:   textFromString(order.ExternalReference),
		PlacedAt:           timestamptzFromUnix(placedAt),
		AcknowledgedAt:     nullTimestamptz(),
		CompletedAt:        nullTimestamptz(),
		Metadata:           metadata,
	}
	if _, err := queries.InsertOrder(ctx, params); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("order store: insert order: %w", err)
	}
	return nil
}

func (s *OrderStore) updateOrderWith(ctx context.Context, queries *sqlc.Queries, update orderstore.OrderUpdate) error {
	if queries == nil {
		return fmt.Errorf("order store: nil queries")
	}
	orderID, err := parseUUID(strings.TrimSpace(update.ID))
	if err != nil {
		return err
	}
	if strings.TrimSpace(update.State) == "" {
		return fmt.Errorf("order store: state required")
	}
	metadata, err := encodeMetadata(update.Metadata)
	if err != nil {
		return fmt.Errorf("order store: encode metadata: %w", err)
	}
	params := sqlc.UpdateOrderStateParams{
		State:          strings.TrimSpace(update.State),
		AcknowledgedAt: timestamptzFromPtr(update.AcknowledgedAt),
		CompletedAt:    timestamptzFromPtr(update.CompletedAt),
		Metadata:       metadata,
		ID:             orderID,
	}
	if _, err := queries.UpdateOrderState(ctx, params); err != nil {
		return fmt.Errorf("order store: update order: %w", err)
	}
	return nil
}

func (s *OrderStore) recordExecutionWith(ctx context.Context, queries *sqlc.Queries, execution orderstore.Execution) error {
	if queries == nil {
		return fmt.Errorf("order store: nil queries")
	}
	orderID, err := parseUUID(strings.TrimSpace(execution.OrderID))
	if err != nil {
		return err
	}
	providerID, err := s.lookupProviderID(ctx, queries, execution.Provider)
	if err != nil {
		return err
	}
	fillQty, err := numericFromString(execution.Quantity)
	if err != nil {
		return fmt.Errorf("order store: execution quantity: %w", err)
	}
	fillPrice, err := numericFromString(execution.Price)
	if err != nil {
		return fmt.Errorf("order store: execution price: %w", err)
	}
	fee, err := numericFromOptional(execution.Fee)
	if err != nil {
		return fmt.Errorf("order store: execution fee: %w", err)
	}
	metadata, err := encodeMetadata(execution.Metadata)
	if err != nil {
		return fmt.Errorf("order store: encode metadata: %w", err)
	}
	params := sqlc.InsertExecutionParams{
		OrderID:      orderID,
		ProviderID:   providerID,
		ExecutionID:  strings.TrimSpace(execution.ExecutionID),
		FillQuantity: fillQty,
		FillPrice:    fillPrice,
		Fee:          fee,
		FeeAsset:     textFromPtr(execution.FeeAsset),
		Liquidity:    strings.TrimSpace(execution.Liquidity),
		TradedAt:     timestamptzFromUnix(execution.TradedAt),
		Metadata:     metadata,
	}
	if _, err := queries.InsertExecution(ctx, params); err != nil {
		return fmt.Errorf("order store: upsert execution: %w", err)
	}
	return nil
}

func (s *OrderStore) upsertBalanceWith(ctx context.Context, queries *sqlc.Queries, balance orderstore.BalanceSnapshot) error {
	if queries == nil {
		return fmt.Errorf("order store: nil queries")
	}
	providerID, err := s.lookupProviderID(ctx, queries, balance.Provider)
	if err != nil {
		return err
	}
	total, err := numericFromString(balance.Total)
	if err != nil {
		return fmt.Errorf("order store: balance total: %w", err)
	}
	available, err := numericFromString(balance.Available)
	if err != nil {
		return fmt.Errorf("order store: balance available: %w", err)
	}
	metadata, err := encodeMetadata(balance.Metadata)
	if err != nil {
		return fmt.Errorf("order store: encode metadata: %w", err)
	}
	params := sqlc.UpsertBalanceSnapshotParams{
		ProviderID: providerID,
		Asset:      strings.ToUpper(strings.TrimSpace(balance.Asset)),
		Total:      total,
		Available:  available,
		SnapshotAt: timestamptzFromUnix(balance.SnapshotAt),
		Metadata:   metadata,
	}
	if _, err := queries.UpsertBalanceSnapshot(ctx, params); err != nil {
		return fmt.Errorf("order store: upsert balance: %w", err)
	}
	return nil
}

// CreateOrder inserts a new order snapshot.
func (s *OrderStore) CreateOrder(ctx context.Context, order orderstore.Order) error {
	queries, err := s.ensureQueries()
	if err != nil {
		return err
	}
	return s.createOrderWith(ctx, queries, order)
}

// UpdateOrder updates order state details.
func (s *OrderStore) UpdateOrder(ctx context.Context, update orderstore.OrderUpdate) error {
	queries, err := s.ensureQueries()
	if err != nil {
		return err
	}
	return s.updateOrderWith(ctx, queries, update)
}

// RecordExecution upserts an execution record.
func (s *OrderStore) RecordExecution(ctx context.Context, execution orderstore.Execution) error {
	queries, err := s.ensureQueries()
	if err != nil {
		return err
	}
	return s.recordExecutionWith(ctx, queries, execution)
}

// UpsertBalance records balances snapshots.
func (s *OrderStore) UpsertBalance(ctx context.Context, balance orderstore.BalanceSnapshot) error {
	queries, err := s.ensureQueries()
	if err != nil {
		return err
	}
	return s.upsertBalanceWith(ctx, queries, balance)
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
	baseQueries, err := s.ensureQueries()
	if err != nil {
		return err
	}
	var txOptions pgx.TxOptions
	txOptions.IsoLevel = pgx.ReadCommitted
	txOptions.AccessMode = pgx.ReadWrite
	txOptions.DeferrableMode = pgx.NotDeferrable
	txOptions.BeginQuery = ""
	txOptions.CommitQuery = ""

	tx, err := pool.BeginTx(ctx, txOptions)
	if err != nil {
		return fmt.Errorf("order store: begin tx: %w", err)
	}
	wrapped := &orderTx{tx: tx, store: s, queries: baseQueries.WithTx(tx)}
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
	queries, err := s.ensureQueries()
	if err != nil {
		return nil, err
	}
	limit := clampLimit(query.Limit, defaultOrderLimit, maxOrderLimit)
	params := sqlc.ListOrdersParams{
		StrategyInstance: textFromString(query.StrategyInstance),
		ProviderAlias:    textFromString(query.Provider),
		States:           normalizedStates(query.States),
		Limit:            safeInt32(limit),
	}
	rows, err := queries.ListOrders(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("order store: list orders: %w", err)
	}
	records := make([]orderstore.OrderRecord, 0, len(rows))
	for _, row := range rows {
		metadata, err := decodeMetadata(row.MetadataJson)
		if err != nil {
			return nil, err
		}
		order := orderstore.Order{
			ID:                row.OrderID,
			Provider:          row.ProviderAlias,
			StrategyInstance:  row.StrategyInstanceID,
			ClientOrderID:     row.ClientOrderID,
			Symbol:            row.Instrument,
			Side:              row.Side,
			Type:              row.OrderType,
			Quantity:          row.QuantityText,
			Price:             nil,
			State:             row.State,
			ExternalReference: "",
			PlacedAt:          timestamptzToUnix(row.PlacedAt),
			Metadata:          metadata,
		}
		if len(order.Metadata) == 0 {
			order.Metadata = nil
		}
		if price := strings.TrimSpace(row.PriceText); price != "" {
			order.Price = &price
		}
		if ref, ok := textToString(row.ExternalOrderRef); ok {
			order.ExternalReference = ref
		}
		record := orderstore.OrderRecord{
			Order:          order,
			AcknowledgedAt: timestamptzToUnixPtr(row.AcknowledgedAt),
			CompletedAt:    timestamptzToUnixPtr(row.CompletedAt),
			CreatedAt:      row.CreatedAt.Time.Unix(),
			UpdatedAt:      row.UpdatedAt.Time.Unix(),
		}
		if len(record.Metadata) == 0 {
			record.Metadata = nil
		}
		records = append(records, record)
	}
	return records, nil
}

// ListExecutions retrieves execution records matching the supplied query filters.
func (s *OrderStore) ListExecutions(ctx context.Context, query orderstore.ExecutionQuery) ([]orderstore.ExecutionRecord, error) {
	queries, err := s.ensureQueries()
	if err != nil {
		return nil, err
	}
	limit := clampLimit(query.Limit, defaultExecutionLimit, maxOrderLimit)
	orderUUID, err := optionalUUID(query.OrderID)
	if err != nil {
		return nil, fmt.Errorf("order store: execution order id: %w", err)
	}
	params := sqlc.ListExecutionsParams{
		StrategyInstance: textFromString(query.StrategyInstance),
		ProviderAlias:    textFromString(query.Provider),
		OrderID:          orderUUID,
		Limit:            safeInt32(limit),
	}
	rows, err := queries.ListExecutions(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("order store: list executions: %w", err)
	}
	records := make([]orderstore.ExecutionRecord, 0, len(rows))
	for _, row := range rows {
		metadata, err := decodeMetadata(row.MetadataJson)
		if err != nil {
			return nil, err
		}
		exec := orderstore.Execution{
			OrderID:     row.OrderID,
			Provider:    row.ProviderAlias,
			ExecutionID: row.ExecutionID,
			Quantity:    row.FillQuantityText,
			Price:       row.FillPriceText,
			Fee:         nil,
			FeeAsset:    nil,
			Liquidity:   "",
			TradedAt:    timestamptzToUnix(row.TradedAt),
			Metadata:    metadata,
		}
		if len(exec.Metadata) == 0 {
			exec.Metadata = nil
		}
		if fee := strings.TrimSpace(row.FeeText); fee != "" {
			exec.Fee = &fee
		}
		if asset, ok := textToString(row.FeeAsset); ok {
			exec.FeeAsset = &asset
		}
		if liquidity, ok := textToString(row.Liquidity); ok {
			exec.Liquidity = liquidity
		}
		record := orderstore.ExecutionRecord{
			Execution:        exec,
			StrategyInstance: row.StrategyInstanceID,
			CreatedAt:        row.CreatedAt.Time.Unix(),
		}
		if len(record.Metadata) == 0 {
			record.Metadata = nil
		}
		records = append(records, record)
	}
	return records, nil
}

// ListBalances retrieves balance snapshots matching the supplied query filters.
func (s *OrderStore) ListBalances(ctx context.Context, query orderstore.BalanceQuery) ([]orderstore.BalanceRecord, error) {
	if strings.TrimSpace(query.Provider) == "" {
		return nil, fmt.Errorf("order store: provider alias required")
	}
	queries, err := s.ensureQueries()
	if err != nil {
		return nil, err
	}
	limit := clampLimit(query.Limit, defaultBalanceLimit, maxOrderLimit)
	assetFilter := strings.TrimSpace(query.Asset)
	if assetFilter != "" {
		assetFilter = strings.ToUpper(assetFilter)
	}
	params := sqlc.ListBalancesParams{
		ProviderAlias: textFromString(query.Provider),
		Asset:         textFromString(assetFilter),
		Limit:         safeInt32(limit),
	}
	rows, err := queries.ListBalances(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("order store: list balances: %w", err)
	}
	records := make([]orderstore.BalanceRecord, 0, len(rows))
	for _, row := range rows {
		metadata, err := decodeMetadata(row.MetadataJson)
		if err != nil {
			return nil, err
		}
		record := orderstore.BalanceRecord{
			BalanceSnapshot: orderstore.BalanceSnapshot{
				Provider:   row.ProviderAlias,
				Asset:      row.Asset,
				Total:      row.TotalText,
				Available:  row.AvailableText,
				SnapshotAt: row.SnapshotAt.Unix(),
				Metadata:   metadata,
			},
			CreatedAt: row.CreatedAt.Time.Unix(),
			UpdatedAt: row.UpdatedAt.Time.Unix(),
		}
		if len(record.Metadata) == 0 { // BalanceRecord.Metadata refers to snapshot Metadata
			record.Metadata = nil
		}
		records = append(records, record)
	}
	return records, nil
}

func (t *orderTx) CreateOrder(ctx context.Context, order orderstore.Order) error {
	if t == nil || t.queries == nil {
		return fmt.Errorf("order store: nil transaction")
	}
	return t.store.createOrderWith(ctx, t.queries, order)
}

func (t *orderTx) UpdateOrder(ctx context.Context, update orderstore.OrderUpdate) error {
	if t == nil || t.queries == nil {
		return fmt.Errorf("order store: nil transaction")
	}
	return t.store.updateOrderWith(ctx, t.queries, update)
}

func (t *orderTx) RecordExecution(ctx context.Context, execution orderstore.Execution) error {
	if t == nil || t.queries == nil {
		return fmt.Errorf("order store: nil transaction")
	}
	return t.store.recordExecutionWith(ctx, t.queries, execution)
}

func (t *orderTx) UpsertBalance(ctx context.Context, balance orderstore.BalanceSnapshot) error {
	if t == nil || t.queries == nil {
		return fmt.Errorf("order store: nil transaction")
	}
	return t.store.upsertBalanceWith(ctx, t.queries, balance)
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

func (s *OrderStore) lookupProviderID(ctx context.Context, queries *sqlc.Queries, provider string) (int64, error) {
	trimmed := strings.TrimSpace(provider)
	if trimmed == "" {
		return 0, fmt.Errorf("order store: provider required")
	}
	id, err := queries.GetProviderID(ctx, trimmed)
	if err != nil {
		return 0, fmt.Errorf("order store: lookup provider %s: %w", trimmed, err)
	}
	return id, nil
}

func (s *OrderStore) lookupStrategyUUID(ctx context.Context, queries *sqlc.Queries, instance string) (pgtype.UUID, error) {
	trimmed := strings.TrimSpace(instance)
	if trimmed == "" {
		return nullUUID(), nil
	}
	id, err := queries.GetStrategyInternalID(ctx, trimmed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nullUUID(), nil
		}
		return nullUUID(), fmt.Errorf("order store: lookup strategy %s: %w", trimmed, err)
	}
	return id, nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nullUUID(), fmt.Errorf("order store: id required")
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return nullUUID(), fmt.Errorf("order store: parse uuid: %w", err)
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

func numericFromString(value string) (pgtype.Numeric, error) {
	var out pgtype.Numeric
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return out, fmt.Errorf("order store: numeric value required")
	}
	if err := out.Scan(trimmed); err != nil {
		return out, fmt.Errorf("order store: parse numeric %q: %w", trimmed, err)
	}
	return out, nil
}

func numericFromOptional(ptr *string) (pgtype.Numeric, error) {
	var out pgtype.Numeric
	if ptr == nil {
		return out, nil
	}
	trimmed := strings.TrimSpace(*ptr)
	if trimmed == "" {
		return out, nil
	}
	if err := out.Scan(trimmed); err != nil {
		return out, fmt.Errorf("order store: parse numeric %q: %w", trimmed, err)
	}
	return out, nil
}

func textFromString(value string) pgtype.Text {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nullText()
	}
	return pgtype.Text{String: trimmed, Valid: true}
}

func textFromPtr(ptr *string) pgtype.Text {
	if ptr == nil {
		return nullText()
	}
	return textFromString(*ptr)
}

func timestamptzFromUnix(value int64) pgtype.Timestamptz {
	if value == 0 {
		return nullTimestamptz()
	}
	return pgtype.Timestamptz{
		Time:             time.Unix(value, 0).UTC(),
		InfinityModifier: pgtype.Finite,
		Valid:            true,
	}
}

func timestamptzFromPtr(ptr *int64) pgtype.Timestamptz {
	if ptr == nil {
		return nullTimestamptz()
	}
	return timestamptzFromUnix(*ptr)
}

func timestamptzToUnix(value pgtype.Timestamptz) int64 {
	if !value.Valid {
		return 0
	}
	return value.Time.Unix()
}

func timestamptzToUnixPtr(value pgtype.Timestamptz) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Time.Unix()
	return &v
}

func textToString(value pgtype.Text) (string, bool) {
	if !value.Valid {
		return "", false
	}
	trimmed := strings.TrimSpace(value.String)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func optionalUUID(value string) (pgtype.UUID, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nullUUID(), nil
	}
	return parseUUID(trimmed)
}

func nullText() pgtype.Text {
	return pgtype.Text{
		String: "",
		Valid:  false,
	}
}

func nullTimestamptz() pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:             time.Time{},
		InfinityModifier: pgtype.Finite,
		Valid:            false,
	}
}

func nullUUID() pgtype.UUID {
	return pgtype.UUID{
		Bytes: [16]byte{},
		Valid: false,
	}
}

func safeInt32(value int) int32 {
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	if value < math.MinInt32 {
		return math.MinInt32
	}
	return int32(value)
}
