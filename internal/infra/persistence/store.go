// Package persistence exposes shared wiring for database-backed repositories.
package persistence

import "github.com/jackc/pgx/v5/pgxpool"

// Store coordinates database-backed repositories. Concrete implementations live
// in subpackages (e.g. postgres).
type Store struct {
	pool *pgxpool.Pool
}

// NewStore constructs a Store backed by the provided pgx pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Pool exposes the underlying pgx pool for repository implementations.
func (s *Store) Pool() *pgxpool.Pool {
	if s == nil {
		return nil
	}
	return s.pool
}
