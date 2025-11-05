// Package strategystore defines persistence contracts for lambda strategy instances.
package strategystore

import (
	"context"
	"time"
)

// Snapshot captures the persisted view of a strategy instance and its configuration.
type Snapshot struct {
	ID              string
	Strategy        Strategy
	Providers       []string
	ProviderSymbols map[string][]string
	Running         bool
	Dynamic         bool
	Baseline        bool
	Metadata        map[string]any
	UpdatedAt       time.Time
}

// Strategy describes the executable strategy metadata.
type Strategy struct {
	Identifier string
	Selector   string
	Tag        string
	Hash       string
	Version    string
	Config     map[string]any
}

// Store abstracts persistence operations for strategy instances.
type Store interface {
	Save(ctx context.Context, snapshot Snapshot) error
	Delete(ctx context.Context, id string) error
	Load(ctx context.Context) ([]Snapshot, error)
}
