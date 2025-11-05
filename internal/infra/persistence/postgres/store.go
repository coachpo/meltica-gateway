package postgres

import (
	"github.com/coachpo/meltica/internal/infra/persistence"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store exposes PostgreSQL-backed repositories generated via sqlc.
type Store struct {
	*persistence.Store
}

// New constructs a PostgreSQL persistence store.
func New(pool *pgxpool.Pool) *Store {
	return &Store{Store: persistence.NewStore(pool)}
}
