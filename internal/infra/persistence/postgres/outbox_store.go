package postgres

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/domain/outboxstore"
	"github.com/coachpo/meltica/internal/infra/persistence/postgres/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OutboxStore persists events destined for the event bus outbox.
type OutboxStore struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

// NewOutboxStore constructs an OutboxStore backed by the provided pool.
func NewOutboxStore(pool *pgxpool.Pool) *OutboxStore {
	if pool == nil {
		return &OutboxStore{
			pool:    nil,
			queries: nil,
		}
	}
	return &OutboxStore{
		pool:    pool,
		queries: sqlc.New(pool),
	}
}

const (
	defaultOutboxLimit = 128
	maxOutboxLimit     = 1024
)

func (s *OutboxStore) ensureQueries() (*sqlc.Queries, error) {
	if s.pool == nil || s.queries == nil {
		return nil, fmt.Errorf("outbox store: nil pool")
	}
	return s.queries, nil
}

// Enqueue inserts a new event into the outbox.
func (s *OutboxStore) Enqueue(ctx context.Context, evt outboxstore.Event) (outboxstore.EventRecord, error) {
	q, err := s.ensureQueries()
	if err != nil {
		return outboxstore.EventRecord{}, err
	}
	aggregateType := strings.TrimSpace(evt.AggregateType)
	if aggregateType == "" {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: aggregate type required")
	}
	aggregateID := strings.TrimSpace(evt.AggregateID)
	if aggregateID == "" {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: aggregate id required")
	}
	eventType := strings.TrimSpace(evt.EventType)
	if eventType == "" {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: event type required")
	}
	payload := strings.TrimSpace(string(evt.Payload))
	if payload == "" {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: payload required")
	}
	headers, err := encodeMap(evt.Headers)
	if err != nil {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: encode headers: %w", err)
	}
	availableAt := evt.AvailableAt
	if availableAt.IsZero() {
		availableAt = time.Now()
	}
	record, err := q.EnqueueEvent(ctx, sqlc.EnqueueEventParams{
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		EventType:     eventType,
		Payload:       []byte(payload),
		Headers:       headers,
		AvailableAt: pgtype.Timestamptz{
			Time:             availableAt,
			InfinityModifier: pgtype.Finite,
			Valid:            true,
		},
	})
	if err != nil {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: enqueue: %w", err)
	}
	return convertOutboxRecord(record)
}

// ListPending returns undelivered events that are ready for replay.
func (s *OutboxStore) ListPending(ctx context.Context, limit int) ([]outboxstore.EventRecord, error) {
	q, err := s.ensureQueries()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultOutboxLimit
	} else if limit > maxOutboxLimit {
		limit = maxOutboxLimit
	}
	rows, err := q.DequeuePendingEvents(ctx, boundedInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("outbox store: list pending: %w", err)
	}

	records := make([]outboxstore.EventRecord, 0, len(rows))
	for _, row := range rows {
		record, err := convertOutboxRecord(row)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// MarkDelivered flags a stored event as successfully published.
func (s *OutboxStore) MarkDelivered(ctx context.Context, id int64) error {
	q, err := s.ensureQueries()
	if err != nil {
		return err
	}
	if _, err := q.MarkEventDelivered(ctx, id); err != nil {
		return fmt.Errorf("outbox store: mark delivered: %w", err)
	}
	return nil
}

// MarkFailed records a failed publish attempt and schedules a retry.
func (s *OutboxStore) MarkFailed(ctx context.Context, id int64, lastError string) error {
	q, err := s.ensureQueries()
	if err != nil {
		return err
	}
	if _, err := q.IncrementEventAttempt(ctx, sqlc.IncrementEventAttemptParams{
		LastError: strings.TrimSpace(lastError),
		ID:        id,
	}); err != nil {
		return fmt.Errorf("outbox store: mark failed: %w", err)
	}
	return nil
}

// Delete removes an outbox entry by identifier.
func (s *OutboxStore) Delete(ctx context.Context, id int64) error {
	q, err := s.ensureQueries()
	if err != nil {
		return err
	}
	if err := q.DeleteEvent(ctx, id); err != nil {
		return fmt.Errorf("outbox store: delete: %w", err)
	}
	return nil
}

func convertOutboxRecord(row sqlc.EventsOutbox) (outboxstore.EventRecord, error) {
	var (
		payloadJSON = append([]byte(nil), row.Payload...)
		headerJSON  = append([]byte(nil), row.Headers...)
	)
	var record outboxstore.EventRecord
	record.ID = row.ID
	record.AggregateType = row.AggregateType
	record.AggregateID = row.AggregateID
	record.EventType = row.EventType
	record.Payload = json.RawMessage(payloadJSON)
	record.AvailableAt = row.AvailableAt.Time
	record.Attempts = int(row.Attempts)
	record.Delivered = row.Delivered
	record.CreatedAt = row.CreatedAt.Time
	if row.PublishedAt.Valid {
		t := row.PublishedAt.Time
		record.PublishedAt = &t
	}
	if row.LastError.Valid {
		record.LastError = row.LastError.String
	}
	headers, err := decodeMap(headerJSON)
	if err != nil {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: decode headers: %w", err)
	}
	record.Headers = headers
	return record, nil
}

func encodeMap(value map[string]any) ([]byte, error) {
	if len(value) == 0 {
		return []byte("{}"), nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	return data, nil
}

func decodeMap(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}
	return out, nil
}

var _ outboxstore.Store = (*OutboxStore)(nil)

func boundedInt32(value int) int32 {
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	if value < math.MinInt32 {
		return math.MinInt32
	}
	return int32(value)
}
