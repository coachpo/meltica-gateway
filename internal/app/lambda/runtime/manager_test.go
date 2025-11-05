package runtime

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coachpo/meltica/internal/app/lambda/js"
	"github.com/coachpo/meltica/internal/domain/strategystore"
	"github.com/coachpo/meltica/internal/infra/config"
)

func newTestManager(t *testing.T, opts ...Option) *Manager {
	t.Helper()
	cfg := config.AppConfig{
		Strategies: config.StrategiesConfig{
			Directory: filepath.Join("..", "..", "..", "..", "strategies"),
		},
	}
	manager, err := NewManager(cfg, nil, nil, nil, log.New(io.Discard, "", 0), nil, opts...)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager
}

func TestManagerRefreshJavaScriptStrategies(t *testing.T) {
	dir := t.TempDir()
	initialSource := `module.exports = {
	  metadata: {
	    name: "alpha",
	    displayName: "Alpha",
	    description: "Alpha v1",
	    config: [],
	    events: ["Trade"]
	  },
	  create: function () {
	    return {};
	  }
};
`
	if err := os.WriteFile(filepath.Join(dir, "alpha.js"), []byte(initialSource), 0o600); err != nil {
		t.Fatalf("write strategy: %v", err)
	}

	cfg := config.AppConfig{
		Environment: config.EnvDev,
		Eventbus:    config.EventbusConfig{BufferSize: 1},
		Pools: config.PoolConfig{
			Event:        config.ObjectPoolConfig{Size: 1, WaitQueueSize: 1},
			OrderRequest: config.ObjectPoolConfig{Size: 1, WaitQueueSize: 1},
		},
		Risk: config.RiskConfig{
			MaxPositionSize:  "1",
			MaxNotionalValue: "1",
			NotionalCurrency: "USD",
			OrderThrottle:    1,
			OrderBurst:       1,
		},
		Telemetry: config.TelemetryConfig{ServiceName: "test"},
		APIServer: config.APIServerConfig{Addr: ":0"},
		Strategies: config.StrategiesConfig{
			Directory: dir,
		},
	}

	mgr, err := NewManager(cfg, nil, nil, nil, log.New(io.Discard, "", 0), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	meta, ok := mgr.StrategyDetail("alpha")
	if !ok {
		t.Fatalf("expected alpha strategy to be registered")
	}
	if meta.DisplayName != "Alpha" {
		t.Fatalf("unexpected display name %q", meta.DisplayName)
	}

	updatedSource := strings.ReplaceAll(initialSource, "Alpha v1", "Alpha v2")
	if _, err := mgr.UpsertStrategy([]byte(updatedSource), js.ModuleWriteOptions{Filename: "alpha.js"}); err != nil {
		t.Fatalf("UpsertStrategy: %v", err)
	}

	meta, ok = mgr.StrategyDetail("alpha")
	if !ok {
		t.Fatalf("alpha strategy missing after upsert")
	}
	if meta.Description != "Alpha v1" {
		t.Fatalf("expected stale metadata before refresh, got %q", meta.Description)
	}

	if err := mgr.RefreshJavaScriptStrategies(context.Background()); err != nil {
		t.Fatalf("RefreshJavaScriptStrategies: %v", err)
	}

	meta, ok = mgr.StrategyDetail("alpha")
	if !ok {
		t.Fatalf("alpha strategy missing after refresh")
	}
	if meta.Description != "Alpha v2" {
		t.Fatalf("expected refreshed metadata, got %q", meta.Description)
	}

	source, err := mgr.StrategySource("alpha.js")
	if err != nil {
		t.Fatalf("StrategySource: %v", err)
	}
	if !strings.Contains(string(source), "Alpha v2") {
		t.Fatalf("expected updated source, got %q", string(source))
	}
}

func baseLambdaSpec() config.LambdaSpec {
	return config.LambdaSpec{
		ID:        "alpha",
		Strategy:  config.LambdaStrategySpec{Identifier: "logging", Config: map[string]any{"logger_prefix": "[test]"}},
		Providers: []string{"okx-spot"},
		ProviderSymbols: map[string]config.ProviderSymbols{
			"okx-spot": {Symbols: []string{"BTC-USDT"}},
		},
	}
}

func TestManagerStrategyPersistenceLifecycle(t *testing.T) {
	store := &recordingStrategyStore{}
	mgr := newTestManager(t, WithStrategyStore(store))
	spec := baseLambdaSpec()

	if err := mgr.ensureSpec(spec, false); err != nil {
		t.Fatalf("ensureSpec: %v", err)
	}
	if len(store.saved) == 0 {
		t.Fatalf("expected snapshot to be persisted after ensureSpec")
	}
	initial := store.saved[len(store.saved)-1]
	if initial.Running {
		t.Fatalf("expected initial snapshot to be stopped")
	}

	mgr.mu.Lock()
	mgr.instances[spec.ID] = &lambdaInstance{}
	mgr.mu.Unlock()
	mgr.persistStrategy(spec.ID)

	if len(store.saved) < 2 {
		t.Fatalf("expected running snapshot to be persisted")
	}
	latest := store.saved[len(store.saved)-1]
	if !latest.Running {
		t.Fatalf("expected latest snapshot to be running")
	}

	mgr.deleteStrategy(spec.ID)
	if len(store.deleted) == 0 || store.deleted[len(store.deleted)-1] != spec.ID {
		t.Fatalf("expected delete call for %s", spec.ID)
	}
}

func TestManagerUpdateImmutableFields(t *testing.T) {
	t.Run("strategy selector resolution", func(t *testing.T) {
		mgr := newTestManager(t)
		resolution, err := mgr.jsLoader.ResolveReference("delay")
		if err != nil {
			t.Fatalf("resolve delay: %v", err)
		}
		hash := resolution.Hash

		testCases := []struct {
			id         string
			identifier string
			expectTag  string
			expectSel  string
		}{
			{
				id:         "delay-default",
				identifier: "delay",
				expectTag:  "latest",
				expectSel:  "delay",
			},
			{
				id:         "delay-tag",
				identifier: "delay:v1.0.0",
				expectTag:  "v1.0.0",
				expectSel:  "delay:v1.0.0",
			},
			{
				id:         "delay-hash",
				identifier: "delay@" + hash,
				expectTag:  "",
				expectSel:  "delay@" + hash,
			},
		}

		for _, tc := range testCases {
			spec := config.LambdaSpec{
				ID: tc.id,
				Strategy: config.LambdaStrategySpec{
					Identifier: tc.identifier,
					Config:     map[string]any{},
				},
				ProviderSymbols: map[string]config.ProviderSymbols{
					"mock": {Symbols: []string{"BTC-USDT"}},
				},
				Providers: []string{"mock"},
			}
		if _, err := mgr.Create(spec); err != nil {
			t.Fatalf("%s: Create: %v", tc.id, err)
		}
			stored, err := mgr.specForID(tc.id)
			if err != nil {
				t.Fatalf("%s: specForID: %v", tc.id, err)
			}
			if stored.Strategy.Hash == "" {
				t.Fatalf("%s: expected hash resolution", tc.id)
			}
			if tc.expectTag != "" && stored.Strategy.Tag != tc.expectTag {
				t.Fatalf("%s: expected tag %s, got %s", tc.id, tc.expectTag, stored.Strategy.Tag)
			}
			if tc.expectSel != "" && stored.Strategy.Selector != tc.expectSel {
				t.Fatalf("%s: expected selector %s, got %s", tc.id, tc.expectSel, stored.Strategy.Selector)
			}
			if tc.identifier == "delay@"+hash && stored.Strategy.Hash != hash {
				t.Fatalf("%s: expected hash %s, got %s", tc.id, hash, stored.Strategy.Hash)
			}
		}
	})

	t.Run("strategy immutable", func(t *testing.T) {
		mgr := newTestManager(t)
		spec := baseLambdaSpec()
	if _, err := mgr.Create(spec); err != nil {
		t.Fatalf("register spec: %v", err)
	}

		spec.Strategy.Identifier = "delay"
		if err := mgr.Update(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "strategy is immutable") {
			t.Fatalf("expected strategy immutability error, got %v", err)
		}
	})

	t.Run("providers immutable", func(t *testing.T) {
		mgr := newTestManager(t)
		spec := baseLambdaSpec()
	if _, err := mgr.Create(spec); err != nil {
		t.Fatalf("register spec: %v", err)
	}

		spec.ProviderSymbols = nil
		spec.Providers = []string{"binance-spot"}
		if err := mgr.Update(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "providers are immutable") {
			t.Fatalf("expected providers immutability error, got %v", err)
		}
	})

	t.Run("scope immutable", func(t *testing.T) {
		mgr := newTestManager(t)
		spec := baseLambdaSpec()
	if _, err := mgr.Create(spec); err != nil {
		t.Fatalf("register spec: %v", err)
	}

		spec.ProviderSymbols = map[string]config.ProviderSymbols{
			"okx-spot": {Symbols: []string{"ETH-USDT"}},
		}
		if err := mgr.Update(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "scope assignments are immutable") {
			t.Fatalf("expected scope immutability error, got %v", err)
		}
	})

	t.Run("config mutable", func(t *testing.T) {
		mgr := newTestManager(t)
		spec := baseLambdaSpec()
	if _, err := mgr.Create(spec); err != nil {
		t.Fatalf("register spec: %v", err)
	}

		spec.Strategy.Config = map[string]any{"logger_prefix": "[updated]"}
		if err := mgr.Update(context.Background(), spec); err != nil {
			t.Fatalf("expected config update to succeed, got %v", err)
		}

		snapshot, ok := mgr.Instance(spec.ID)
		if !ok {
			t.Fatalf("instance %s not found after update", spec.ID)
		}
		prefix, ok := snapshot.Strategy.Config["logger_prefix"].(string)
		if !ok || prefix != "[updated]" {
			t.Fatalf("expected updated logger_prefix, got %v", snapshot.Strategy.Config["logger_prefix"])
		}
	})
}

type recordingStrategyStore struct {
	saved   []strategystore.Snapshot
	deleted []string
}

func (r *recordingStrategyStore) Save(_ context.Context, snapshot strategystore.Snapshot) error {
	cloned := snapshot
	cloned.Providers = append([]string(nil), snapshot.Providers...)
	if cloned.Metadata == nil {
		cloned.Metadata = make(map[string]any)
	}
	r.saved = append(r.saved, cloned)
	return nil
}

func (r *recordingStrategyStore) Delete(_ context.Context, id string) error {
	r.deleted = append(r.deleted, id)
	return nil
}

func (r *recordingStrategyStore) Load(context.Context) ([]strategystore.Snapshot, error) {
	return nil, nil
}

func TestManagerRevisionUsageDetail(t *testing.T) {
	mgr := newTestManager(t)
	spec := baseLambdaSpec()
	if _, err := mgr.Create(spec); err != nil {
		t.Fatalf("Create lambda: %v", err)
	}
	stored, err := mgr.specForID(spec.ID)
	if err != nil {
		t.Fatalf("specForID: %v", err)
	}
	mgr.mu.Lock()
	revisionKey := mgr.markInstanceRunningLocked(stored, spec.ID)
	mgr.instances[spec.ID] = &lambdaInstance{revKey: revisionKey}
	mgr.mu.Unlock()

	usage := mgr.RevisionUsageFor(stored.Strategy.Identifier, stored.Strategy.Hash)
	if usage.Count != 1 {
		t.Fatalf("expected count 1, got %d", usage.Count)
	}
	if len(usage.Instances) != 1 || usage.Instances[0] != spec.ID {
		t.Fatalf("unexpected instances slice: %+v", usage.Instances)
	}

	detail, canonical, instances, err := mgr.RevisionUsageDetail(stored.Strategy.Selector, false)
	if err != nil {
		t.Fatalf("RevisionUsageDetail: %v", err)
	}
	if canonical == "" {
		t.Fatalf("expected canonical selector, got empty string")
	}
	if detail.Count != 1 {
		t.Fatalf("expected detail count 1, got %d", detail.Count)
	}
	if len(instances) != 1 || !instances[0].Running {
		t.Fatalf("expected single running instance, got %+v", instances)
	}

	mgr.mu.Lock()
	delete(mgr.instances, spec.ID)
	mgr.markInstanceStoppedLocked(revisionKey, spec.ID)
	mgr.mu.Unlock()

	post := mgr.RevisionUsageFor(stored.Strategy.Identifier, stored.Strategy.Hash)
	if post.Count != 0 {
		t.Fatalf("expected count 0 after stop, got %d", post.Count)
	}
	if post.LastSeen.IsZero() {
		t.Fatalf("expected lastSeen to be recorded")
	}

	_, _, allInstances, err := mgr.RevisionUsageDetail(stored.Strategy.Selector, true)
	if err != nil {
		t.Fatalf("RevisionUsageDetail includeStopped: %v", err)
	}
	if len(allInstances) != 1 || allInstances[0].Running {
		t.Fatalf("expected stopped instance in result, got %+v", allInstances)
	}
}

func TestRemoveStrategyGuardedWhenHashInUse(t *testing.T) {
	mgr := newTestManager(t)
	spec := config.LambdaSpec{
		ID: "guarded",
		Strategy: config.LambdaStrategySpec{
			Identifier: "logging",
			Config:     map[string]any{},
		},
		ProviderSymbols: map[string]config.ProviderSymbols{
			"mock": {Symbols: []string{"BTC-USDT"}},
		},
		Providers: []string{"mock"},
	}
	if _, err := mgr.Create(spec); err != nil {
		t.Fatalf("Create lambda: %v", err)
	}
	stored, err := mgr.specForID("guarded")
	if err != nil {
		t.Fatalf("specForID: %v", err)
	}
	if err := mgr.RemoveStrategy("logging@" + stored.Strategy.Hash); err == nil {
		t.Fatalf("expected removal to fail for hash in use")
	}
	if err := mgr.RemoveStrategy("logging"); err == nil {
		t.Fatalf("expected removal to fail for strategy in use")
	}
}
