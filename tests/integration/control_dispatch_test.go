package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/bus/controlbus"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/schema"
)

func TestControlPlaneUpdatesDispatchTable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	bus := controlbus.NewMemoryBus(controlbus.MemoryConfig{BufferSize: 4})
	manager := &fakeSubscriptionManager{}
	table := dispatcher.NewTable()
	seedRoute := dispatcher.Route{
		Type:     schema.CanonicalType("TICKER"),
		WSTopics: []string{"ticker.BTCUSDT"},
		RestFns:  []dispatcher.RestFn{{Name: "orderbookSnapshot", Endpoint: "/api/v3/depth", Interval: 5 * time.Second, Parser: "orderbook"}},
	}
	require.NoError(t, table.Upsert(seedRoute))

	controller := dispatcher.NewController(table, bus, manager)
	go func() {
		_ = controller.Start(ctx)
	}()

	handler := dispatcher.NewControlHTTPHandler(bus)
	server := httptest.NewServer(handler)
	defer server.Close()

	subscribeReq := schema.Subscribe{
		Type:    schema.CanonicalType("TICKER"),
		Filters: map[string]any{"instrument": "BTC-USDT"},
	}
	ack := postCommand(t, server.URL+"/control/subscribe", subscribeReq)
	require.True(t, ack.Success)
	require.Equal(t, 1, ack.RoutingVersion)

	require.Eventually(t, func() bool {
		_, ok := table.Lookup(schema.CanonicalType("TICKER"))
		return ok
	}, 100*time.Millisecond, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		return manager.wasActivated(schema.CanonicalType("TICKER"))
	}, 100*time.Millisecond, 5*time.Millisecond)

	unsub := schema.Unsubscribe{Type: schema.CanonicalType("TICKER")}
	ack = postCommand(t, server.URL+"/control/unsubscribe", unsub)
	require.True(t, ack.Success)
	require.Equal(t, 2, ack.RoutingVersion)

	require.Eventually(t, func() bool {
		_, ok := table.Lookup(schema.CanonicalType("TICKER"))
		return !ok
	}, 100*time.Millisecond, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		return manager.wasDeactivated(schema.CanonicalType("TICKER"))
	}, 100*time.Millisecond, 5*time.Millisecond)
}

func postCommand(t *testing.T, url string, payload any) schema.ControlAcknowledgement {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	var ack schema.ControlAcknowledgement
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&ack))
	return ack
}

type fakeSubscriptionManager struct {
	mu          sync.Mutex
	activated   map[schema.CanonicalType]struct{}
	deactivated map[schema.CanonicalType]struct{}
}

func (f *fakeSubscriptionManager) Activate(ctx context.Context, route dispatcher.Route) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.activated == nil {
		f.activated = make(map[schema.CanonicalType]struct{})
	}
	f.activated[route.Type] = struct{}{}
	return nil
}

func (f *fakeSubscriptionManager) Deactivate(ctx context.Context, typ schema.CanonicalType) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deactivated == nil {
		f.deactivated = make(map[schema.CanonicalType]struct{})
	}
	f.deactivated[typ] = struct{}{}
	return nil
}

func (f *fakeSubscriptionManager) wasActivated(typ schema.CanonicalType) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.activated[typ]
	return ok
}

func (f *fakeSubscriptionManager) wasDeactivated(typ schema.CanonicalType) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.deactivated[typ]
	return ok
}
