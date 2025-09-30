package binance

import (
	"context"
	"sync"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

// SnapshotFetcher retrieves REST snapshot payloads.
type SnapshotFetcher interface {
	Fetch(ctx context.Context, endpoint string) ([]byte, error)
}

// SnapshotParser converts REST payloads into canonical events.
type SnapshotParser interface {
	ParseSnapshot(ctx context.Context, name string, body []byte, ingestTS time.Time) ([]*schema.Event, error)
}

// RESTPoller declares a REST snapshot poll configuration.
type RESTPoller struct {
	Name     string
	Endpoint string
	Interval time.Duration
	Parser   string
}

// RESTClient polls Binance REST endpoints on configured intervals.
type RESTClient struct {
	fetcher SnapshotFetcher
	parser  SnapshotParser
	clock   func() time.Time
}

// NewRESTClient constructs a REST snapshot client.
func NewRESTClient(fetcher SnapshotFetcher, parser SnapshotParser, clock func() time.Time) *RESTClient {
	if clock == nil {
		clock = time.Now
	}
	return &RESTClient{fetcher: fetcher, parser: parser, clock: clock}
}

// Poll executes the provided REST pollers concurrently and emits canonical events.
func (c *RESTClient) Poll(ctx context.Context, pollers []RESTPoller) (<-chan *schema.Event, <-chan error) {
	events := make(chan *schema.Event)
	errs := make(chan error, len(pollers))

	if len(pollers) == 0 {
		close(events)
		close(errs)
		return events, errs
	}

	var wg sync.WaitGroup
	wg.Add(len(pollers))

	for _, poller := range pollers {
		poller := poller
		if poller.Interval <= 0 {
			poller.Interval = time.Second
		}
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(poller.Interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					body, err := c.fetcher.Fetch(ctx, poller.Endpoint)
					if err != nil {
						select {
						case errs <- err:
						default:
						}
						return
					}
					ingest := c.clock().UTC()
					parsed, err := c.parser.ParseSnapshot(ctx, poller.Parser, body, ingest)
					if err != nil {
						select {
						case errs <- err:
						default:
						}
						continue
					}
					for _, evt := range parsed {
						if evt == nil {
							continue
						}
						if evt.IngestTS.IsZero() {
							evt.IngestTS = ingest
						}
						if evt.EmitTS.IsZero() {
							evt.EmitTS = ingest
						}
						select {
						case <-ctx.Done():
							return
						case events <- evt:
						}
					}
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(events)
		close(errs)
	}()

	return events, errs
}
