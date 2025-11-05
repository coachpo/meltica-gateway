package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/coachpo/meltica/internal/domain/strategystore"
	json "github.com/goccy/go-json"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StrategyStore persists lambda strategy instance metadata.
type StrategyStore struct {
	pool *pgxpool.Pool
}

// NewStrategyStore constructs a StrategyStore backed by the provided pgx pool.
func NewStrategyStore(pool *pgxpool.Pool) *StrategyStore {
	return &StrategyStore{pool: pool}
}

const (
	strategyUpsertSQL = `
INSERT INTO strategy_instances (
    instance_id,
    strategy_identifier,
    version,
    status,
    config_hash,
    metadata,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, NOW())
ON CONFLICT (instance_id) DO UPDATE SET
    strategy_identifier = EXCLUDED.strategy_identifier,
    version = EXCLUDED.version,
    status = EXCLUDED.status,
    config_hash = EXCLUDED.config_hash,
    metadata = EXCLUDED.metadata,
    updated_at = NOW();
`
	strategyDeleteSQL = `DELETE FROM strategy_instances WHERE instance_id = $1;`
	strategySelectSQL = `
SELECT instance_id, strategy_identifier, version, status, config_hash, metadata, updated_at
FROM strategy_instances
ORDER BY instance_id;
`
)

// Save upserts the provided strategy snapshot.
func (s *StrategyStore) Save(ctx context.Context, snapshot strategystore.Snapshot) error {
	if s.pool == nil {
		return fmt.Errorf("strategy store: nil pool")
	}
	id := strings.TrimSpace(snapshot.ID)
	if id == "" {
		return fmt.Errorf("strategy store: instance id required")
	}

	payload, err := encodeStrategyMetadata(snapshot)
	if err != nil {
		return err
	}
	status := "stopped"
	if snapshot.Running {
		status = "running"
	}
	configHash := computeStrategyConfigHash(snapshot)

	metadataBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("strategy store: encode metadata: %w", err)
	}

	if _, err := s.pool.Exec(ctx, strategyUpsertSQL,
		id,
		strings.TrimSpace(snapshot.Strategy.Identifier),
		strings.TrimSpace(snapshot.Strategy.Version),
		status,
		configHash,
		metadataBytes,
	); err != nil {
		return fmt.Errorf("strategy store: upsert snapshot: %w", err)
	}
	return nil
}

// Delete removes a strategy snapshot.
func (s *StrategyStore) Delete(ctx context.Context, id string) error {
	if s.pool == nil {
		return fmt.Errorf("strategy store: nil pool")
	}
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("strategy store: instance id required")
	}
	if _, err := s.pool.Exec(ctx, strategyDeleteSQL, trimmed); err != nil {
		return fmt.Errorf("strategy store: delete snapshot: %w", err)
	}
	return nil
}

// Load retrieves all strategy snapshots.
func (s *StrategyStore) Load(ctx context.Context) ([]strategystore.Snapshot, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("strategy store: nil pool")
	}
	rows, err := s.pool.Query(ctx, strategySelectSQL)
	if err != nil {
		return nil, fmt.Errorf("strategy store: select snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []strategystore.Snapshot
	for rows.Next() {
		var (
			instanceID string
			identifier string
			version    string
			status     string
			configHash string
			metadata   []byte
			updatedAt  pgtype.Timestamptz
		)
		if err := rows.Scan(&instanceID, &identifier, &version, &status, &configHash, &metadata, &updatedAt); err != nil {
			return nil, fmt.Errorf("strategy store: scan snapshot: %w", err)
		}
		decoded, err := decodeStrategyMetadata(metadata)
		if err != nil {
			return nil, err
		}
		snapshot := strategystore.Snapshot{
			ID:              instanceID,
			Strategy:        decoded.Strategy,
			Providers:       decoded.Providers,
			ProviderSymbols: decoded.ProviderSymbols,
			Running:         status == "running",
			Dynamic:         decoded.Dynamic,
			Baseline:        decoded.Baseline,
			Metadata:        decoded.Metadata,
			UpdatedAt:       updatedAt.Time,
		}
		if snapshot.Strategy.Identifier == "" {
			snapshot.Strategy.Identifier = identifier
		}
		if snapshot.Strategy.Version == "" {
			snapshot.Strategy.Version = version
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("strategy store: iterate snapshots: %w", err)
	}
	return snapshots, nil
}

type strategyMetadata struct {
	Strategy        strategystore.Strategy `json:"strategy"`
	Providers       []string               `json:"providers"`
	ProviderSymbols map[string][]string    `json:"providerSymbols"`
	Dynamic         bool                   `json:"dynamic"`
	Baseline        bool                   `json:"baseline"`
	Metadata        map[string]any         `json:"metadata"`
}

func encodeStrategyMetadata(snapshot strategystore.Snapshot) (strategyMetadata, error) {
	meta := strategyMetadata{
		Strategy: strategystore.Strategy{
			Identifier: strings.TrimSpace(snapshot.Strategy.Identifier),
			Selector:   strings.TrimSpace(snapshot.Strategy.Selector),
			Tag:        strings.TrimSpace(snapshot.Strategy.Tag),
			Hash:       strings.TrimSpace(snapshot.Strategy.Hash),
			Version:    strings.TrimSpace(snapshot.Strategy.Version),
			Config:     cloneMap(snapshot.Strategy.Config),
		},
		Providers:       cloneStringSlice(snapshot.Providers),
		ProviderSymbols: cloneProviderSymbols(snapshot.ProviderSymbols),
		Dynamic:         snapshot.Dynamic,
		Baseline:        snapshot.Baseline,
		Metadata:        cloneMap(snapshot.Metadata),
	}
	return meta, nil
}

func decodeStrategyMetadata(raw []byte) (strategyMetadata, error) {
	if len(raw) == 0 {
		return strategyMetadata{
			Strategy: strategystore.Strategy{
				Identifier: "",
				Selector:   "",
				Tag:        "",
				Hash:       "",
				Version:    "",
				Config:     make(map[string]any),
			},
			Providers:       []string{},
			ProviderSymbols: map[string][]string{},
			Dynamic:         false,
			Baseline:        false,
			Metadata:        make(map[string]any),
		}, nil
	}
	var meta strategyMetadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return strategyMetadata{}, fmt.Errorf("strategy store: decode metadata: %w", err)
	}
	if meta.Strategy.Config == nil {
		meta.Strategy.Config = make(map[string]any)
	}
	if meta.ProviderSymbols == nil {
		meta.ProviderSymbols = make(map[string][]string)
	}
	if meta.Metadata == nil {
		meta.Metadata = make(map[string]any)
	}
	return meta, nil
}

func computeStrategyConfigHash(snapshot strategystore.Snapshot) string {
	payload := struct {
		Identifier string              `json:"identifier"`
		Selector   string              `json:"selector"`
		Providers  []string            `json:"providers"`
		Symbols    map[string][]string `json:"symbols"`
		Config     map[string]any      `json:"config"`
	}{
		Identifier: strings.TrimSpace(snapshot.Strategy.Identifier),
		Selector:   strings.TrimSpace(snapshot.Strategy.Selector),
		Providers:  cloneStringSlice(snapshot.Providers),
		Symbols:    cloneProviderSymbols(snapshot.ProviderSymbols),
		Config:     cloneMap(snapshot.Strategy.Config),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneProviderSymbols(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, symbols := range in {
		out[key] = cloneStringSlice(symbols)
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
