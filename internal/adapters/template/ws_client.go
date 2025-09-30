package template

import (
	"context"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

// WSClient streams canonical events from a websocket transport.
type WSClient interface {
	Stream(ctx context.Context, topics []string) (<-chan *schema.Event, <-chan error)
}

// FrameProvider establishes websocket subscriptions for provider adapters.
type FrameProvider interface {
	Subscribe(ctx context.Context, topics []string) (<-chan []byte, <-chan error, error)
}

// WSParser converts raw websocket frames into canonical events.
type WSParser interface {
	Parse(ctx context.Context, frame []byte, ingestTS time.Time) ([]*schema.Event, error)
}
