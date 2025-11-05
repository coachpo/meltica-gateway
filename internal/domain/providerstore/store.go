// Package providerstore defines persistence contracts for provider metadata and routes.
package providerstore

import (
	"context"
	"time"

	"github.com/coachpo/meltica/internal/domain/schema"
)

// Snapshot captures the persisted view of a provider specification and its runtime status.
type Snapshot struct {
	Name        string
	DisplayName string
	Adapter     string
	Config      map[string]any
	Status      string
	Metadata    map[string]any
}

// RouteSnapshot captures dispatcher routing metadata persisted per provider.
type RouteSnapshot struct {
	Type     schema.RouteType
	WSTopics []string
	RestFns  []RouteRestFn
	Filters  []RouteFilter
}

// RouteRestFn mirrors dispatcher REST polling configuration.
type RouteRestFn struct {
	Name     string
	Endpoint string
	Interval time.Duration
	Parser   string
}

// RouteFilter mirrors dispatcher filter configuration.
type RouteFilter struct {
	Field string
	Op    string
	Value any
}

// Store abstracts persistence operations for provider specifications.
type Store interface {
	SaveProvider(ctx context.Context, snapshot Snapshot) error
	DeleteProvider(ctx context.Context, name string) error
	LoadProviders(ctx context.Context) ([]Snapshot, error)
	SaveRoutes(ctx context.Context, provider string, routes []RouteSnapshot) error
	LoadRoutes(ctx context.Context, provider string) ([]RouteSnapshot, error)
	DeleteRoutes(ctx context.Context, provider string) error
}
