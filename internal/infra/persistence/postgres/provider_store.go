package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/coachpo/meltica/internal/domain/providerstore"
	json "github.com/goccy/go-json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProviderStore persists provider specifications and status metadata in PostgreSQL.
type ProviderStore struct {
	pool *pgxpool.Pool
}

// NewProviderStore constructs a ProviderStore backed by the provided pgx pool.
func NewProviderStore(pool *pgxpool.Pool) *ProviderStore {
	return &ProviderStore{pool: pool}
}

const (
	providerUpsertSQL = `
INSERT INTO providers (
    alias,
    display_name,
    adapter_identifier,
    connection,
    status,
    metadata,
    updated_at
)
VALUES ($1, $2, $3, $4::jsonb, $5, $6::jsonb, NOW())
ON CONFLICT (alias) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    adapter_identifier = EXCLUDED.adapter_identifier,
    connection = EXCLUDED.connection,
    status = EXCLUDED.status,
    metadata = EXCLUDED.metadata,
    updated_at = NOW();
`
	providerDeleteSQL = `DELETE FROM providers WHERE alias = $1;`
	providerListSQL   = `
SELECT alias, display_name, adapter_identifier, connection, status, metadata
FROM providers
ORDER BY alias;
`
	providerLookupIDSQL = `SELECT id FROM providers WHERE alias = $1;`
	providerRouteDelete = `DELETE FROM provider_routes WHERE provider_id = $1;`
	providerRouteSelect = `SELECT route FROM provider_routes WHERE provider_id = $1 ORDER BY symbol;`
	providerRouteUpsert = `
INSERT INTO provider_routes (
    provider_id,
    symbol,
    route,
    version,
    metadata,
    updated_at
)
VALUES ($1, $2, $3::jsonb, 1, '{}'::jsonb, NOW())
ON CONFLICT (provider_id, symbol) DO UPDATE SET
    route = EXCLUDED.route,
    version = EXCLUDED.version,
    metadata = EXCLUDED.metadata,
    updated_at = NOW();
`
)

// SaveProvider upserts a provider snapshot.
func (s *ProviderStore) SaveProvider(ctx context.Context, snapshot providerstore.Snapshot) error {
	if s.pool == nil {
		return fmt.Errorf("provider store: nil pool")
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

	if _, err := s.pool.Exec(ctx, providerUpsertSQL, name, display, snapshot.Adapter, connection, snapshot.Status, metadata); err != nil {
		return fmt.Errorf("upsert provider: %w", err)
	}
	return nil
}

// DeleteProvider removes a provider snapshot.
func (s *ProviderStore) DeleteProvider(ctx context.Context, name string) error {
	if s.pool == nil {
		return fmt.Errorf("provider store: nil pool")
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("provider store: provider name required")
	}
	if _, err := s.pool.Exec(ctx, providerDeleteSQL, trimmed); err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	return nil
}

// LoadProviders retrieves all provider snapshots.
func (s *ProviderStore) LoadProviders(ctx context.Context) ([]providerstore.Snapshot, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("provider store: nil pool")
	}
	rows, err := s.pool.Query(ctx, providerListSQL)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()

	var snapshots []providerstore.Snapshot
	for rows.Next() {
		var alias, display, adapter, status string
		var connectionBytes, metadataBytes []byte
		if err := rows.Scan(&alias, &display, &adapter, &connectionBytes, &status, &metadataBytes); err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		configMap, err := decodeJSON(connectionBytes)
		if err != nil {
			return nil, fmt.Errorf("decode provider config: %w", err)
		}
		metadataMap, err := decodeJSON(metadataBytes)
		if err != nil {
			return nil, fmt.Errorf("decode provider metadata: %w", err)
		}
		snapshots = append(snapshots, providerstore.Snapshot{
			Name:        alias,
			DisplayName: display,
			Adapter:     adapter,
			Config:      configMap,
			Status:      status,
			Metadata:    metadataMap,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate providers: %w", err)
	}
	return snapshots, nil
}

// SaveRoutes replaces all persisted routes for a provider.
func (s *ProviderStore) SaveRoutes(ctx context.Context, provider string, routes []providerstore.RouteSnapshot) error {
	if s.pool == nil {
		return fmt.Errorf("provider store: nil pool")
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
	var txOptions pgx.TxOptions
	txOptions.IsoLevel = pgx.ReadCommitted
	txOptions.AccessMode = pgx.ReadWrite
	txOptions.DeferrableMode = pgx.NotDeferrable

	tx, err := s.pool.BeginTx(ctx, txOptions)
	if err != nil {
		return fmt.Errorf("begin route tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, providerRouteDelete, providerID); err != nil {
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
		if _, err := tx.Exec(ctx, providerRouteUpsert, providerID, symbol, payload); err != nil {
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
	if s.pool == nil {
		return nil, fmt.Errorf("provider store: nil pool")
	}
	trimmed := strings.TrimSpace(provider)
	if trimmed == "" {
		return nil, fmt.Errorf("provider store: provider name required")
	}
	providerID, err := s.lookupProviderID(ctx, trimmed)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, providerRouteSelect, providerID)
	if err != nil {
		return nil, fmt.Errorf("select routes: %w", err)
	}
	defer rows.Close()
	var routes []providerstore.RouteSnapshot
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan route: %w", err)
		}
		var snapshot providerstore.RouteSnapshot
		if err := json.Unmarshal(payload, &snapshot); err != nil {
			return nil, fmt.Errorf("unmarshal route: %w", err)
		}
		routes = append(routes, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routes: %w", err)
	}
	return routes, nil
}

// DeleteRoutes removes persisted routes for a provider.
func (s *ProviderStore) DeleteRoutes(ctx context.Context, provider string) error {
	if s.pool == nil {
		return fmt.Errorf("provider store: nil pool")
	}
	trimmed := strings.TrimSpace(provider)
	if trimmed == "" {
		return fmt.Errorf("provider store: provider name required")
	}
	providerID, err := s.lookupProviderID(ctx, trimmed)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, providerRouteDelete, providerID); err != nil {
		return fmt.Errorf("delete routes: %w", err)
	}
	return nil
}

func (s *ProviderStore) lookupProviderID(ctx context.Context, name string) (int64, error) {
	row := s.pool.QueryRow(ctx, providerLookupIDSQL, name)
	var id int64
	if err := row.Scan(&id); err != nil {
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
