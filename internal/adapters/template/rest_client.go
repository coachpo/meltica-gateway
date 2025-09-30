package template

import (
	"context"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

// RESTPoller describes a periodic REST request used to enrich market data.
type RESTPoller struct {
	Name     string
	Endpoint string
	Interval time.Duration
	Parser   string
}

// RESTClient polls REST endpoints and emits canonical events.
type RESTClient interface {
	Poll(ctx context.Context, pollers []RESTPoller) (<-chan *schema.Event, <-chan error)
}

// SnapshotFetcher retrieves raw REST payloads from a provider API.
type SnapshotFetcher interface {
	Fetch(ctx context.Context, endpoint string) ([]byte, error)
}

// SnapshotParser converts provider snapshot payloads into canonical events.
type SnapshotParser interface {
	ParseSnapshot(ctx context.Context, name string, body []byte, ingestTS time.Time) ([]*schema.Event, error)
}
