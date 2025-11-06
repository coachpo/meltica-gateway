package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/coachpo/meltica/internal/domain/providerstore"
	"github.com/coachpo/meltica/internal/infra/persistence/postgres/sqlc"
	json "github.com/goccy/go-json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProviderStore persists provider specifications and status metadata in PostgreSQL.
type ProviderStore struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

// NewProviderStore constructs a ProviderStore backed by the provided pgx pool.
func NewProviderStore(pool *pgxpool.Pool) *ProviderStore {
	if pool == nil {
		return &ProviderStore{pool: nil, queries: nil}
	}
	return &ProviderStore{
		pool:    pool,
		queries: sqlc.New(pool),
	}
}

func (s *ProviderStore) ensureQueries() (*sqlc.Queries, error) {
	if s.pool == nil || s.queries == nil {
		return nil, fmt.Errorf("provider store: nil pool")
	}
	return s.queries, nil
}

// SaveProvider upserts a provider snapshot.
func (s *ProviderStore) SaveProvider(ctx context.Context, snapshot providerstore.Snapshot) error {
	q, err := s.ensureQueries()
	if err != nil {
		return err
	}
	name := strings.TrimSpace(snapshot.Name)
	if name == "" {
		return fmt.Errorf("provider store: provider name required")
	}

	display := strings.TrimSpace(snapshot.DisplayName)
	if display == "" {
		display = name
	}

	connection, err := encodeJSON(snapshot.Config)
	if err != nil {
		return fmt.Errorf("marshal provider config: %w", err)
	}
	metadata, err := encodeJSON(snapshot.Metadata)
	if err != nil {
		return fmt.Errorf("marshal provider metadata: %w", err)
	}

	params := sqlc.UpsertProviderParams{
		Alias:             name,
		DisplayName:       display,
		AdapterIdentifier: strings.TrimSpace(snapshot.Adapter),
		Connection:        connection,
		Status:            strings.TrimSpace(snapshot.Status),
		Metadata:          metadata,
	}
	if _, err := q.UpsertProvider(ctx, params); err != nil {
		return fmt.Errorf("upsert provider: %w", err)
	}
	return nil
}

// DeleteProvider removes a provider snapshot.
func (s *ProviderStore) DeleteProvider(ctx context.Context, name string) error {
	q, err := s.ensureQueries()
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("provider store: provider name required")
	}
	if err := q.DeleteProviderByAlias(ctx, trimmed); err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	return nil
}

// LoadProviders retrieves all provider snapshots.
func (s *ProviderStore) LoadProviders(ctx context.Context) ([]providerstore.Snapshot, error) {
	q, err := s.ensureQueries()
	if err != nil {
		return nil, err
	}
	rows, err := q.ListProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	var snapshots []providerstore.Snapshot
	for _, row := range rows {
		configMap, err := decodeJSON(row.Connection)
		if err != nil {
			return nil, fmt.Errorf("decode provider config: %w", err)
		}
		metadataMap, err := decodeJSON(row.Metadata)
		if err != nil {
			return nil, fmt.Errorf("decode provider metadata: %w", err)
		}
		snapshots = append(snapshots, providerstore.Snapshot{
			Name:        row.Alias,
			DisplayName: row.DisplayName,
			Adapter:     row.AdapterIdentifier,
			Config:      configMap,
			Status:      row.Status,
			Metadata:    metadataMap,
		})
	}
	return snapshots, nil
}

// SaveRoutes replaces all persisted routes for a provider.
func (s *ProviderStore) SaveRoutes(ctx context.Context, provider string, routes []providerstore.RouteSnapshot) error {
	q, err := s.ensureQueries()
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(provider)
	if trimmed == "" {
		return fmt.Errorf("provider store: provider name required")
	}
	if len(routes) == 0 {
		return s.DeleteRoutes(ctx, trimmed)
	}
	providerID, err := s.lookupProviderID(ctx, trimmed)
	if err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:       pgx.ReadCommitted,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.NotDeferrable,
		BeginQuery:     "",
		CommitQuery:    "",
	})
	if err != nil {
		return fmt.Errorf("begin route tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := q.WithTx(tx)
	if err := qtx.DeleteRoutesByProvider(ctx, providerID); err != nil {
		return fmt.Errorf("clear routes: %w", err)
	}
	for idx, route := range routes {
		payload, err := json.Marshal(route)
		if err != nil {
			return fmt.Errorf("marshal route: %w", err)
		}
		symbol := strings.ToUpper(string(route.Type))
		if symbol == "" {
			symbol = fmt.Sprintf("ROUTE-%d", idx)
		}
		params := sqlc.UpsertProviderRouteParams{
			ProviderID: providerID,
			Symbol:     symbol,
			Route:      payload,
			Version:    1,
			Metadata:   []byte("{}"),
		}
		if _, err := qtx.UpsertProviderRoute(ctx, params); err != nil {
			return fmt.Errorf("upsert route: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit route tx: %w", err)
	}
	return nil
}

// LoadRoutes retrieves persisted routes for a provider.
func (s *ProviderStore) LoadRoutes(ctx context.Context, provider string) ([]providerstore.RouteSnapshot, error) {
	q, err := s.ensureQueries()
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(provider)
	if trimmed == "" {
		return nil, fmt.Errorf("provider store: provider name required")
	}
	providerID, err := s.lookupProviderID(ctx, trimmed)
	if err != nil {
		return nil, err
	}
	rows, err := q.ListProviderRoutes(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("select routes: %w", err)
	}
	var routes []providerstore.RouteSnapshot
	for _, row := range rows {
		var snapshot providerstore.RouteSnapshot
		if err := json.Unmarshal(row.Route, &snapshot); err != nil {
			return nil, fmt.Errorf("unmarshal route: %w", err)
		}
		routes = append(routes, snapshot)
	}
	return routes, nil
}

// DeleteRoutes removes persisted routes for a provider.
func (s *ProviderStore) DeleteRoutes(ctx context.Context, provider string) error {
	q, err := s.ensureQueries()
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(provider)
	if trimmed == "" {
		return fmt.Errorf("provider store: provider name required")
	}
	providerID, err := s.lookupProviderID(ctx, trimmed)
	if err != nil {
		return err
	}
	if err := q.DeleteRoutesByProvider(ctx, providerID); err != nil {
		return fmt.Errorf("delete routes: %w", err)
	}
	return nil
}

func (s *ProviderStore) lookupProviderID(ctx context.Context, name string) (int64, error) {
	q, err := s.ensureQueries()
	if err != nil {
		return 0, err
	}
	id, err := q.GetProviderID(ctx, strings.TrimSpace(name))
	if err != nil {
		return 0, fmt.Errorf("lookup provider id: %w", err)
	}
	return id, nil
}

func encodeJSON(value map[string]any) ([]byte, error) {
	if len(value) == 0 {
		return []byte("{}"), nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	return data, nil
}

func decodeJSON(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}
	return out, nil
}
