package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/domain/outboxstore"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OutboxStore persists events destined for the event bus outbox.
type OutboxStore struct {
	pool *pgxpool.Pool
}

// NewOutboxStore constructs an OutboxStore backed by the provided pool.
func NewOutboxStore(pool *pgxpool.Pool) *OutboxStore {
	return &OutboxStore{pool: pool}
}

const (
	defaultOutboxLimit  = 128
	maxOutboxLimit      = 1024
	outboxRetryInterval = 30 * time.Second
)

const (
	outboxInsertSQL = `
INSERT INTO events_outbox (
    aggregate_type,
    aggregate_id,
    event_type,
    payload,
    headers,
    available_at
)
VALUES ($1, $2, $3, COALESCE($4::jsonb, '{}'::jsonb), COALESCE($5::jsonb, '{}'::jsonb), $6)
RETURNING
    id,
    aggregate_type,
    aggregate_id,
    event_type,
    payload,
    headers,
    available_at,
    published_at,
    attempts,
    last_error,
    delivered,
    created_at;
`

	outboxListPendingSQL = `
SELECT
    id,
    aggregate_type,
    aggregate_id,
    event_type,
    payload,
    headers,
    available_at,
    published_at,
    attempts,
    last_error,
    delivered,
    created_at
FROM events_outbox
WHERE delivered = FALSE
  AND available_at <= NOW()
ORDER BY available_at ASC
LIMIT $1;
`

	outboxMarkDeliveredSQL = `
UPDATE events_outbox
SET delivered = TRUE,
    published_at = NOW(),
    attempts = attempts + 1
WHERE id = $1;
`

	outboxMarkFailedSQL = `
UPDATE events_outbox
SET attempts = attempts + 1,
    last_error = $2,
    available_at = $3
WHERE id = $1;
`

	outboxDeleteSQL = `
DELETE FROM events_outbox
WHERE id = $1;
`
)

// Enqueue inserts a new event into the outbox.
func (s *OutboxStore) Enqueue(ctx context.Context, evt outboxstore.Event) (outboxstore.EventRecord, error) {
	if s.pool == nil {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: nil pool")
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
	payload, err := encodeJSON(evt.Payload)
	if err != nil {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: encode payload: %w", err)
	}
	headers, err := encodeJSON(evt.Headers)
	if err != nil {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: encode headers: %w", err)
	}
	availableAt := evt.AvailableAt
	if availableAt.IsZero() {
		availableAt = time.Now()
	}
	row := s.pool.QueryRow(ctx, outboxInsertSQL, aggregateType, aggregateID, eventType, payload, headers, availableAt)
	return scanOutboxRecord(row)
}

// ListPending returns undelivered events that are ready for replay.
func (s *OutboxStore) ListPending(ctx context.Context, limit int) ([]outboxstore.EventRecord, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("outbox store: nil pool")
	}
	if limit <= 0 {
		limit = defaultOutboxLimit
	} else if limit > maxOutboxLimit {
		limit = maxOutboxLimit
	}
	rows, err := s.pool.Query(ctx, outboxListPendingSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("outbox store: list pending: %w", err)
	}
	defer rows.Close()

	var records []outboxstore.EventRecord
	for rows.Next() {
		record, err := scanOutboxRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("outbox store: iterate pending: %w", err)
	}
	return records, nil
}

// MarkDelivered flags a stored event as successfully published.
func (s *OutboxStore) MarkDelivered(ctx context.Context, id int64) error {
	if s.pool == nil {
		return fmt.Errorf("outbox store: nil pool")
	}
	tag, err := s.pool.Exec(ctx, outboxMarkDeliveredSQL, id)
	if err != nil {
		return fmt.Errorf("outbox store: mark delivered: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("outbox store: mark delivered: no rows updated")
	}
	return nil
}

// MarkFailed records a failed publish attempt and schedules a retry.
func (s *OutboxStore) MarkFailed(ctx context.Context, id int64, lastError string) error {
	if s.pool == nil {
		return fmt.Errorf("outbox store: nil pool")
	}
	nextAttempt := time.Now().Add(outboxRetryInterval)
	tag, err := s.pool.Exec(ctx, outboxMarkFailedSQL, id, strings.TrimSpace(lastError), nextAttempt)
	if err != nil {
		return fmt.Errorf("outbox store: mark failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("outbox store: mark failed: no rows updated")
	}
	return nil
}

// Delete removes an outbox entry by identifier.
func (s *OutboxStore) Delete(ctx context.Context, id int64) error {
	if s.pool == nil {
		return fmt.Errorf("outbox store: nil pool")
	}
	tag, err := s.pool.Exec(ctx, outboxDeleteSQL, id)
	if err != nil {
		return fmt.Errorf("outbox store: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("outbox store: delete: no rows deleted")
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanOutboxRecord(row rowScanner) (outboxstore.EventRecord, error) {
	var (
		record      outboxstore.EventRecord
		payloadJSON []byte
		headerJSON  []byte
		publishedAt pgtype.Timestamptz
		lastError   pgtype.Text
	)
	if err := row.Scan(
		&record.ID,
		&record.AggregateType,
		&record.AggregateID,
		&record.EventType,
		&payloadJSON,
		&headerJSON,
		&record.AvailableAt,
		&publishedAt,
		&record.Attempts,
		&lastError,
		&record.Delivered,
		&record.CreatedAt,
	); err != nil {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: scan record: %w", err)
	}
	if publishedAt.Valid {
		t := publishedAt.Time
		record.PublishedAt = &t
	}
	if lastError.Valid {
		record.LastError = lastError.String
	}
	payload, err := decodeJSON(payloadJSON)
	if err != nil {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: decode payload: %w", err)
	}
	headers, err := decodeJSON(headerJSON)
	if err != nil {
		return outboxstore.EventRecord{}, fmt.Errorf("outbox store: decode headers: %w", err)
	}
	record.Payload = payload
	record.Headers = headers
	return record, nil
}

var _ outboxstore.Store = (*OutboxStore)(nil)
