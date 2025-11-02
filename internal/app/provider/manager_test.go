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
