package unit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/adapters/binance"
	"github.com/coachpo/meltica/internal/schema"
)

func TestRESTClientPollsSnapshotsAndParsesEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fetcher := &stubSnapshotFetcher{}
	parser := &stubRESTParser{}
	clock := newStubClock(time.Unix(0, 0))
	client := binance.NewRESTClient(fetcher, parser, clock.Now)

	pollers := []binance.RESTPoller{{
		Name:     "orderbook",
		Endpoint: "/depth",
		Interval: 5 * time.Millisecond,
		Parser:   "orderbook",
	}}

	event := &schema.Event{
		EventID:     "binance:BTC-USDT:BookSnapshot:100",
		Provider:    "binance",
		Symbol:      "BTC-USDT",
		Type:        schema.EventTypeBookSnapshot,
		SeqProvider: 100,
		Payload: schema.BookSnapshotPayload{
			Checksum: "abc",
		},
	}
	parser.enqueueResult([]*schema.Event{event}, nil)
	fetcher.enqueueResponse([]byte("{}"), nil)

	events, errs := client.Poll(ctx, pollers)

	select {
	case got := <-events:
		require.NotNil(t, got)
		require.Equal(t, event.EventID, got.EventID)
		require.Equal(t, event.Type, got.Type)
		require.Equal(t, pollers[0].Endpoint, fetcher.lastEndpoint())
	case <-time.After(time.Second):
		t.Fatal("expected snapshot event")
	}

	cancel()
	require.Eventually(t, func() bool {
		_, ok := <-events
		return !ok
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		_, ok := <-errs
		return !ok
	}, time.Second, 10*time.Millisecond)
}

func TestRESTClientPropagatesFetcherErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fetcher := &stubSnapshotFetcher{}
	parser := &stubRESTParser{}
	clock := newStubClock(time.Unix(10, 0))
	client := binance.NewRESTClient(fetcher, parser, clock.Now)

	fetchErr := errors.New("fetch boom")
	fetcher.enqueueResponse(nil, fetchErr)

	pollers := []binance.RESTPoller{{
		Name:     "orderbook",
		Endpoint: "/depth",
		Interval: 1 * time.Millisecond,
		Parser:   "orderbook",
	}}

	_, errs := client.Poll(ctx, pollers)
	select {
	case err := <-errs:
		require.ErrorIs(t, err, fetchErr)
	case <-time.After(time.Second):
		t.Fatal("expected fetch error propagated")
	}
}

type stubSnapshotFetcher struct {
	mu        sync.Mutex
	responses [][]byte
	errors    []error
	endpoints []string
}

func (f *stubSnapshotFetcher) enqueueResponse(body []byte, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses = append(f.responses, body)
	f.errors = append(f.errors, err)
}

func (f *stubSnapshotFetcher) Fetch(ctx context.Context, endpoint string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.endpoints = append(f.endpoints, endpoint)
	if len(f.responses) == 0 {
		return nil, nil
	}
	body := f.responses[0]
	err := f.errors[0]
	f.responses = f.responses[1:]
	f.errors = f.errors[1:]
	return body, err
}

func (f *stubSnapshotFetcher) lastEndpoint() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.endpoints) == 0 {
		return ""
	}
	return f.endpoints[len(f.endpoints)-1]
}

type stubRESTParser struct {
	mu     sync.Mutex
	events [][]*schema.Event
	errors []error
}

func (p *stubRESTParser) enqueueResult(events []*schema.Event, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, events)
	p.errors = append(p.errors, err)
}

func (p *stubRESTParser) ParseSnapshot(_ context.Context, name string, body []byte, ingestTS time.Time) ([]*schema.Event, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.events) == 0 {
		return nil, nil
	}
	events := p.events[0]
	err := p.errors[0]
	p.events = p.events[1:]
	p.errors = p.errors[1:]
	for _, evt := range events {
		if evt == nil {
			continue
		}
		evt.IngestTS = ingestTS
		evt.EmitTS = ingestTS
	}
	return events, err
}
