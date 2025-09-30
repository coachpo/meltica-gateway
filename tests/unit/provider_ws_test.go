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

func TestWSClientStreamsCanonicalEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frames := make(chan []byte, 2)
	errCh := make(chan error, 1)
	provider := &stubFrameProvider{frames: frames, errCh: errCh}
	parser := &stubWSParser{}
	clock := newStubClock(time.Unix(0, 0))

	client := binance.NewWSClient("binance", provider, parser, clock.Now, nil)

	events, errs := client.Stream(ctx, []string{"btcusdt@depth"})
	require.NotNil(t, events)
	require.NotNil(t, errs)
	require.ElementsMatch(t, []string{"btcusdt@depth"}, provider.lastTopics)

	want := &schema.Event{
		EventID:        "binance:BTC-USDT:BookUpdate:42",
		Provider:       "binance",
		Symbol:         "BTC-USDT",
		Type:           schema.EventTypeBookUpdate,
		SeqProvider:    42,
		IngestTS:       clock.Now(),
		EmitTS:         clock.Now(),
		Payload:        schema.BookUpdatePayload{UpdateType: schema.BookUpdateTypeDelta},
		RoutingVersion: 0,
	}
	parser.enqueueResult([]*schema.Event{want}, nil)
	frames <- []byte(`{"event":"delta"}`)

	select {
	case got := <-events:
		require.NotNil(t, got)
		require.Equal(t, want.EventID, got.EventID)
		require.Equal(t, want.Provider, got.Provider)
		require.Equal(t, want.Symbol, got.Symbol)
		require.Equal(t, want.Type, got.Type)
		require.Equal(t, want.SeqProvider, got.SeqProvider)
		require.True(t, got.IngestTS.Equal(want.IngestTS))
	case <-time.After(time.Second):
		t.Fatal("expected event from WS client")
	}

	// Cancel should close channels without panicking.
	cancel()
	require.Eventually(t, func() bool {
		_, ok := <-events
		return !ok
	}, time.Second, 10*time.Millisecond, "events channel not closed after cancel")
}

func TestWSClientPropagatesErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frames := make(chan []byte, 1)
	errCh := make(chan error, 1)
	provider := &stubFrameProvider{frames: frames, errCh: errCh}
	parser := &stubWSParser{}
	clock := newStubClock(time.Unix(1, 0))

	client := binance.NewWSClient("binance", provider, parser, clock.Now, nil)
	events, errs := client.Stream(ctx, []string{"btcusdt@aggTrade"})

	parseErr := errors.New("parse failure")
	parser.enqueueResult(nil, parseErr)
	frames <- []byte(`{"event":"trade"}`)

	select {
	case err := <-errs:
		require.ErrorIs(t, err, parseErr)
	case <-time.After(time.Second):
		t.Fatal("expected parse error propagated")
	}

	provider.errCh <- context.DeadlineExceeded
	select {
	case err := <-errs:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("expected provider error propagated")
	}

	// Ensure events channel closes when provider signals completion.
	close(frames)
	close(provider.errCh)
	require.Eventually(t, func() bool {
		_, ok := <-events
		return !ok
	}, time.Second, 10*time.Millisecond)
}

type stubFrameProvider struct {
	frames     <-chan []byte
	errCh      chan error
	lastTopics []string
}

func (s *stubFrameProvider) Subscribe(ctx context.Context, topics []string) (<-chan []byte, <-chan error, error) {
	s.lastTopics = append([]string(nil), topics...)
	return s.frames, s.errCh, nil
}

type stubWSParser struct {
	mu      sync.Mutex
	results [][]*schema.Event
	errors  []error
}

func (p *stubWSParser) enqueueResult(events []*schema.Event, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.results = append(p.results, events)
	p.errors = append(p.errors, err)
}

func (p *stubWSParser) Parse(_ context.Context, frame []byte, ingestTS time.Time) ([]*schema.Event, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.results) == 0 {
		return nil, nil
	}
	events := p.results[0]
	err := p.errors[0]
	p.results = p.results[1:]
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

type stubClock struct {
	mu  sync.Mutex
	now time.Time
}

func newStubClock(start time.Time) *stubClock {
	return &stubClock{now: start}
}

func (c *stubClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stubClock) Advance(delta time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(delta)
	c.mu.Unlock()
}
