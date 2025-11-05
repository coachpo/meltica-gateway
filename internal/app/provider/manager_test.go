package provider

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	"github.com/coachpo/meltica/internal/domain/providerstore"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
)

func TestSanitizedProviderSpecsRemovesSensitiveFields(t *testing.T) {
	manager := NewManager(nil, nil, nil, nil, nil)

	spec := config.ProviderSpec{
		Name:    "binance",
		Adapter: "binance",
		Config: map[string]any{
			"identifier":    "binance",
			"provider_name": "binance",
			"api_key":       "super-secret",
			"config": map[string]any{
				"rest_timeout": "1s",
				"token":        "hidden",
				"depth":        100,
			},
		},
	}

	if _, err := manager.Create(context.Background(), spec, false); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	sanitized := manager.SanitizedProviderSpecs()
	if len(sanitized) != 1 {
		t.Fatalf("expected 1 sanitized spec, got %d", len(sanitized))
	}

	cfg := sanitized[0].Config
	if cfg == nil {
		t.Fatal("expected sanitized config to be present")
	}
	if _, ok := cfg["api_key"]; ok {
		t.Fatal("expected api_key to be removed from sanitized config")
	}

	nested, _ := cfg["config"].(map[string]any)
	if nested == nil {
		t.Fatal("expected nested config to be present")
	}
	if _, ok := nested["token"]; ok {
		t.Fatal("expected token to be removed from nested config")
	}
	if nested["rest_timeout"] != "1s" {
		t.Fatalf("expected rest_timeout to be retained, got %v", nested["rest_timeout"])
	}
}

func TestCreatePersistsProviderSnapshot(t *testing.T) {
	store := &recordingStore{}
	manager := NewManager(nil, nil, nil, nil, nil, WithPersistence(store))

	spec := config.ProviderSpec{
		Name:    "binance",
		Adapter: "binance",
		Config: map[string]any{
			"identifier":    "binance",
			"provider_name": "binance",
		},
	}

	if _, err := manager.Create(context.Background(), spec, false); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if len(store.saved) == 0 {
		t.Fatalf("expected snapshot persisted")
	}
	snapshot := store.saved[len(store.saved)-1]
	if snapshot.Name != "binance" {
		t.Fatalf("expected snapshot name binance, got %s", snapshot.Name)
	}
	if snapshot.Status != string(StatusPending) {
		t.Fatalf("expected pending status, got %s", snapshot.Status)
	}

	if err := manager.Remove("binance"); err != nil {
		t.Fatalf("remove provider: %v", err)
	}
	if len(store.deleted) == 0 || store.deleted[len(store.deleted)-1] != "binance" {
		t.Fatalf("expected delete call for binance, got %v", store.deleted)
	}
}

func TestRestoreProviderSnapshot(t *testing.T) {
	store := &recordingStore{}
	logger := log.New(io.Discard, "", 0)
	manager := NewManager(nil, nil, nil, dispatcher.NewTable(), logger, WithPersistence(store))

	snapshot := providerstore.Snapshot{
		Name:    "binance",
		Adapter: "binance",
		Config: map[string]any{
			"identifier":    "binance",
			"provider_name": "binance",
		},
		Status: string(StatusRunning),
	}

	manager.Restore(snapshot)
	if !manager.HasProvider("binance") {
		t.Fatalf("expected restored provider to exist")
	}
	if len(store.saved) != 0 {
		t.Fatalf("restore should not persist snapshot immediately")
	}

	detail, ok := manager.ProviderMetadataFor("binance")
	if !ok {
		t.Fatalf("expected metadata for restored provider")
	}
	if detail.Status != StatusStopped {
		t.Fatalf("expected restored status to normalise to stopped, got %s", detail.Status)
	}
	if detail.Running {
		t.Fatalf("expected restored provider not running")
	}
}

func TestStopProviderPersistsRoutes(t *testing.T) {
	store := &recordingStore{}
	logger := log.New(io.Discard, "", 0)
	manager := NewManager(nil, nil, nil, dispatcher.NewTable(), logger, WithPersistence(store))

	spec := config.ProviderSpec{
		Name:    "binance",
		Adapter: "binance",
		Config: map[string]any{
			"identifier":    "binance",
			"provider_name": "binance",
		},
	}

	if _, err := manager.Create(context.Background(), spec, false); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	manager.mu.Lock()
	state := manager.states["binance"]
	state.running = true
	state.status = StatusRunning
	state.instance = &testProviderInstance{name: "binance"}
	state.cachedRoutes = []dispatcher.Route{
		{
			Provider: "binance",
			Type:     schema.RouteTypeTrade,
			WSTopics: []string{"trade"},
			RestFns: []dispatcher.RestFn{
				{
					Name:     "depth",
					Endpoint: "/depth",
					Interval: time.Second,
					Parser:   "depth",
				},
			},
			Filters: []dispatcher.FilterRule{
				{Field: "symbol", Op: "eq", Value: "BTCUSDT"},
			},
		},
	}
	manager.mu.Unlock()

	if _, err := manager.StopProvider("binance"); err != nil {
		t.Fatalf("stop provider: %v", err)
	}

	routes := store.savedRoutes["binance"]
	if len(routes) != 1 {
		t.Fatalf("expected 1 route persisted, got %d", len(routes))
	}
	if routes[0].Type != schema.RouteTypeTrade {
		t.Fatalf("expected route type trade, got %s", routes[0].Type)
	}
	if routes[0].RestFns[0].Interval != time.Second {
		t.Fatalf("expected interval 1s, got %s", routes[0].RestFns[0].Interval)
	}
}

func TestStartProviderAsyncTransitionsToRunning(t *testing.T) {
	registry := NewRegistry()
	started := make(chan struct{}, 1)
	registry.Register("stub", func(ctx context.Context, pools *pool.PoolManager, cfg map[string]any) (Instance, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		name, _ := cfg["provider_name"].(string)
		if name == "" {
			name = "stub"
		}
		return &testProviderInstance{name: name}, nil
	})

	logger := log.New(io.Discard, "", 0)
	manager := NewManager(registry, nil, nil, dispatcher.NewTable(), logger)

	spec := config.ProviderSpec{
		Name:    "stub",
		Adapter: "stub",
		Config: map[string]any{
			"identifier":    "stub",
			"provider_name": "stub",
		},
	}

	detail, err := manager.Create(context.Background(), spec, false)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if detail.Status != StatusPending {
		t.Fatalf("expected pending status, got %s", detail.Status)
	}
	if detail.Running {
		t.Fatalf("expected provider not running after create")
	}

	if _, err := manager.StartProviderAsync("stub"); err != nil {
		t.Fatalf("start async: %v", err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for async start invocation")
	}

	var final RuntimeDetail
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if meta, ok := manager.ProviderMetadataFor("stub"); ok && meta.Status == StatusRunning && meta.Running {
			final = meta
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if final.Status != StatusRunning {
		t.Fatalf("expected running status, got %s", final.Status)
	}
	if !final.Running {
		t.Fatal("expected provider to be running")
	}
	if final.StartupError != "" {
		t.Fatalf("expected empty startup error, got %q", final.StartupError)
	}
}

func TestStartProviderAsyncFailureRecordsError(t *testing.T) {
	registry := NewRegistry()
	registry.Register("failing", func(ctx context.Context, pools *pool.PoolManager, cfg map[string]any) (Instance, error) {
		return nil, errors.New("boom")
	})

	logger := log.New(io.Discard, "", 0)
	manager := NewManager(registry, nil, nil, dispatcher.NewTable(), logger)

	spec := config.ProviderSpec{
		Name:    "failing",
		Adapter: "failing",
		Config: map[string]any{
			"identifier":    "failing",
			"provider_name": "failing",
		},
	}

	if _, err := manager.Create(context.Background(), spec, false); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	if _, err := manager.StartProviderAsync("failing"); err != nil {
		t.Fatalf("start async: %v", err)
	}

	var failing RuntimeDetail
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if meta, ok := manager.ProviderMetadataFor("failing"); ok && meta.Status == StatusFailed {
			failing = meta
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if failing.Status != StatusFailed {
		t.Fatalf("expected failed status, got %s", failing.Status)
	}
	if failing.Running {
		t.Fatal("expected provider not running after failure")
	}
	if !strings.Contains(failing.StartupError, "boom") {
		t.Fatalf("expected startup error to mention boom, got %q", failing.StartupError)
	}
}

type testProviderInstance struct {
	name string
}

func (i *testProviderInstance) Name() string                    { return i.name }
func (i *testProviderInstance) Start(ctx context.Context) error { return nil }
func (i *testProviderInstance) Events() <-chan *schema.Event    { return nil }
func (i *testProviderInstance) Errors() <-chan error            { return nil }
func (i *testProviderInstance) SubmitOrder(ctx context.Context, req schema.OrderRequest) error {
	return nil
}
func (i *testProviderInstance) SubscribeRoute(route dispatcher.Route) error   { return nil }
func (i *testProviderInstance) UnsubscribeRoute(route dispatcher.Route) error { return nil }
func (i *testProviderInstance) Instruments() []schema.Instrument              { return nil }

type recordingStore struct {
	saved       []providerstore.Snapshot
	deleted     []string
	savedRoutes map[string][]providerstore.RouteSnapshot
	seedRoutes  map[string][]providerstore.RouteSnapshot
}

func (r *recordingStore) SaveProvider(ctx context.Context, snapshot providerstore.Snapshot) error {
	r.saved = append(r.saved, snapshot)
	return nil
}

func (r *recordingStore) DeleteProvider(ctx context.Context, name string) error {
	r.deleted = append(r.deleted, name)
	return nil
}

func (r *recordingStore) LoadProviders(ctx context.Context) ([]providerstore.Snapshot, error) {
	return nil, nil
}

func (r *recordingStore) SaveRoutes(ctx context.Context, provider string, routes []providerstore.RouteSnapshot) error {
	if r.savedRoutes == nil {
		r.savedRoutes = make(map[string][]providerstore.RouteSnapshot)
	}
	cloned := make([]providerstore.RouteSnapshot, len(routes))
	copy(cloned, routes)
	r.savedRoutes[provider] = cloned
	return nil
}

func (r *recordingStore) LoadRoutes(ctx context.Context, provider string) ([]providerstore.RouteSnapshot, error) {
	if r.seedRoutes == nil {
		return nil, nil
	}
	cloned := make([]providerstore.RouteSnapshot, len(r.seedRoutes[provider]))
	copy(cloned, r.seedRoutes[provider])
	return cloned, nil
}

func (r *recordingStore) DeleteRoutes(ctx context.Context, provider string) error {
	if r.savedRoutes != nil {
		delete(r.savedRoutes, provider)
	}
	return nil
}
