package httpserver

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	lambdaruntime "github.com/coachpo/meltica/internal/app/lambda/runtime"
	"github.com/coachpo/meltica/internal/app/provider"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/bus/eventbus"
	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
)

func TestBuildContextBackup(t *testing.T) {
	appCfg := config.AppConfig{
		Environment: config.EnvDev,
		Eventbus: config.EventbusConfig{
			BufferSize: 16,
		},
		Pools: config.PoolConfig{
			Event: config.ObjectPoolConfig{
				Size:          8,
				WaitQueueSize: 8,
			},
			OrderRequest: config.ObjectPoolConfig{
				Size:          4,
				WaitQueueSize: 4,
			},
		},
		Risk: config.RiskConfig{
			MaxPositionSize:     "10",
			MaxNotionalValue:    "1000",
			NotionalCurrency:    "USD",
			OrderThrottle:       5,
			OrderBurst:          1,
			MaxConcurrentOrders: 0,
			PriceBandPercent:    1.0,
			AllowedOrderTypes:   []string{"Limit"},
			KillSwitchEnabled:   true,
			MaxRiskBreaches:     1,
			CircuitBreaker: config.CircuitBreakerConfig{
				Enabled:   true,
				Threshold: 1,
				Cooldown:  "30s",
			},
		},
		APIServer: config.APIServerConfig{
			Addr: ":0",
		},
		Telemetry: config.TelemetryConfig{
			OTLPEndpoint:  "http://localhost:4318",
			ServiceName:   "test-gateway",
			OTLPInsecure:  true,
			EnableMetrics: true,
		},
	}

	poolMgr := pool.NewPoolManager()
	if err := poolMgr.RegisterPool("Event", appCfg.Pools.Event.Size, appCfg.Pools.Event.QueueSize(), func() interface{} { return new(schema.Event) }); err != nil {
		t.Fatalf("register Event pool: %v", err)
	}
	if err := poolMgr.RegisterPool("OrderRequest", appCfg.Pools.OrderRequest.Size, appCfg.Pools.OrderRequest.QueueSize(), func() interface{} { return new(schema.OrderRequest) }); err != nil {
		t.Fatalf("register OrderRequest pool: %v", err)
	}

	bus := eventbus.NewMemoryBus(eventbus.MemoryConfig{
		BufferSize:    appCfg.Eventbus.BufferSize,
		FanoutWorkers: appCfg.Eventbus.FanoutWorkerCount(),
		Pools:         poolMgr,
	})

	table := dispatcher.NewTable()
	providerManager := provider.NewManager(nil, poolMgr, bus, table, log.New(ioDiscards{}, "", 0))

	// Register a provider spec with sensitive fields.
	providerSpec := config.ProviderSpec{
		Name:    "binance",
		Adapter: "binance",
		Config: map[string]any{
			"identifier": "binance",
			"config": map[string]any{
				"api_key": "secret",
				"depth":   100,
			},
		},
	}
	if _, err := providerManager.Create(context.Background(), providerSpec, false); err != nil {
		t.Fatalf("Create provider spec failed: %v", err)
	}

	logger := log.New(ioDiscards{}, "", 0)
	registrar := dispatcher.NewRegistrar(table, providerManager)
	lambdaManager := lambdaruntime.NewManager(appCfg, bus, poolMgr, providerManager, logger, registrar)

	manifest := config.LambdaManifest{
		Lambdas: []config.LambdaSpec{
			{
				ID:       "alpha",
				Strategy: config.LambdaStrategySpec{Identifier: "logging", Config: map[string]any{}},
				ProviderSymbols: map[string]config.ProviderSymbols{
					"binance": {
						Symbols: []string{"BTC-USDT"},
					},
				},
			},
		},
	}

	if err := lambdaManager.StartFromManifest(manifest); err != nil {
		t.Fatalf("StartFromManifest failed: %v", err)
	}

	server := &httpServer{
		manager:       lambdaManager,
		providers:     providerManager,
		baseProviders: map[string]struct{}{},
		baseLambdas:   map[string]struct{}{},
	}

	snapshot := server.buildContextBackup()

	if len(snapshot.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(snapshot.Providers))
	}

	providerCfg, ok := snapshot.Providers[0].Config["config"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested config map, got %T", snapshot.Providers[0].Config["config"])
	}
	if _, present := providerCfg["api_key"]; present {
		t.Fatal("expected api_key to be removed from exported provider config")
	}
	switch depth := providerCfg["depth"].(type) {
	case float64:
		if depth != 100 {
			t.Fatalf("expected depth 100, got %v", depth)
		}
	case int:
		if depth != 100 {
			t.Fatalf("expected depth 100, got %v", depth)
		}
	default:
		t.Fatalf("expected numeric depth, got %T", depth)
	}

	if len(snapshot.Lambdas) != 1 {
		t.Fatalf("expected 1 lambda snapshot, got %d", len(snapshot.Lambdas))
	}
	if snapshot.Lambdas[0].ID != "alpha" {
		t.Fatalf("expected lambda id alpha, got %s", snapshot.Lambdas[0].ID)
	}

	if snapshot.Risk.MaxPositionSize != appCfg.Risk.MaxPositionSize {
		t.Fatalf("expected risk maxPositionSize %s, got %s", appCfg.Risk.MaxPositionSize, snapshot.Risk.MaxPositionSize)
	}

	expectedNotional := decimal.RequireFromString(appCfg.Risk.MaxNotionalValue)
	actualNotional := decimal.RequireFromString(snapshot.Risk.MaxNotionalValue)
	if !expectedNotional.Equal(actualNotional) {
		t.Fatalf("expected maxNotionalValue %s, got %s", expectedNotional, actualNotional)
	}
}

func TestApplyContextBackupRestoresState(t *testing.T) {
	appCfg := config.AppConfig{}

	poolMgr := pool.NewPoolManager()
	if err := poolMgr.RegisterPool("Event", 8, 8, func() interface{} { return new(schema.Event) }); err != nil {
		t.Fatalf("register Event pool: %v", err)
	}
	if err := poolMgr.RegisterPool("OrderRequest", 4, 4, func() interface{} { return new(schema.OrderRequest) }); err != nil {
		t.Fatalf("register OrderRequest pool: %v", err)
	}

	bus := eventbus.NewMemoryBus(eventbus.MemoryConfig{
		BufferSize:    16,
		FanoutWorkers: 1,
		Pools:         poolMgr,
	})

	table := dispatcher.NewTable()
	providerManager := provider.NewManager(nil, poolMgr, bus, table, log.New(ioDiscards{}, "", 0))
	logger := log.New(ioDiscards{}, "", 0)
	registrar := dispatcher.NewRegistrar(table, providerManager)
	lambdaManager := lambdaruntime.NewManager(appCfg, bus, poolMgr, providerManager, logger, registrar)

	server := &httpServer{
		manager:       lambdaManager,
		providers:     providerManager,
		baseProviders: map[string]struct{}{},
		baseLambdas:   map[string]struct{}{},
	}

	payload := contextBackup{
		Providers: []config.ProviderSpec{
			{
				Name:    "binance",
				Adapter: "binance",
				Config: map[string]any{
					"identifier": "binance",
					"config": map[string]any{
						"depth": 100,
					},
				},
			},
		},
		Lambdas: []config.LambdaSpec{
			{
				ID:       "alpha",
				Strategy: config.LambdaStrategySpec{Identifier: "logging", Config: map[string]any{}},
				ProviderSymbols: map[string]config.ProviderSymbols{
					"binance": {
						Symbols: []string{"BTC-USDT"},
					},
				},
				Providers: []string{"binance"},
			},
		},
		Risk: config.RiskConfig{
			MaxPositionSize:  "20",
			MaxNotionalValue: "2000",
			NotionalCurrency: "USD",
			OrderThrottle:    10,
			OrderBurst:       2,
		},
	}

	if err := server.applyContextBackup(context.Background(), payload); err != nil {
		t.Fatalf("applyContextBackup failed: %v", err)
	}

	detail, ok := providerManager.ProviderMetadataFor("binance")
	if !ok {
		t.Fatal("expected provider binance to exist after restore")
	}
	if detail.Running {
		t.Fatal("expected provider to be stopped after restore")
	}

	snapshot, ok := lambdaManager.Instance("alpha")
	if !ok {
		t.Fatal("expected lambda alpha to exist after restore")
	}
	if snapshot.Running {
		t.Fatal("expected lambda alpha to be stopped after restore")
	}

	limits := lambdaManager.RiskLimits()
	if !limits.MaxPositionSize.Equal(decimal.RequireFromString("20")) {
		t.Fatalf("expected max position size 20, got %s", limits.MaxPositionSize.String())
	}
	if !limits.MaxNotionalValue.Equal(decimal.RequireFromString("2000")) {
		t.Fatalf("expected max notional value 2000, got %s", limits.MaxNotionalValue.String())
	}
}

func TestBuildProviderSpecFromPayload_SanitizesEmptyConfig(t *testing.T) {
	payload := providerPayload{
		Name: "binance-ui-test",
		Adapter: providerAdapterPayload{
			Identifier: "binance",
			Config: map[string]any{
				"api_key":     "",
				"api_secret":  "   ",
				"recv_window": "5s",
				"list":        []any{" first ", " ", "second"},
				"nested": map[string]any{
					"alpha": "  ",
					"beta":  "value",
				},
			},
		},
	}

	spec, enabled, err := buildProviderSpecFromPayload(payload)
	if err != nil {
		t.Fatalf("buildProviderSpecFromPayload returned error: %v", err)
	}
	if !enabled {
		t.Fatalf("expected provider to default to enabled")
	}
	if spec.Adapter != "binance" {
		t.Fatalf("expected adapter binance, got %s", spec.Adapter)
	}

	cfg, ok := spec.Config["config"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested config map, got %T", spec.Config["config"])
	}
	if _, exists := cfg["api_key"]; exists {
		t.Fatalf("expected empty api_key to be removed, found %v", cfg["api_key"])
	}
	if _, exists := cfg["api_secret"]; exists {
		t.Fatalf("expected empty api_secret to be removed, found %v", cfg["api_secret"])
	}
	if recvWindow, ok := cfg["recv_window"].(string); !ok || recvWindow != "5s" {
		t.Fatalf("expected recv_window to remain trimmed string, got %#v", cfg["recv_window"])
	}
	list, ok := cfg["list"].([]any)
	if !ok {
		t.Fatalf("expected list to be []any, got %T", cfg["list"])
	}
	if len(list) != 2 || list[0] != "first" || list[1] != "second" {
		t.Fatalf("expected cleaned list [first second], got %#v", list)
	}
	nested, ok := cfg["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %T", cfg["nested"])
	}
	if _, present := nested["alpha"]; present {
		t.Fatalf("expected empty nested value to be pruned, nested=%#v", nested)
	}
	if nested["beta"] != "value" {
		t.Fatalf("expected nested beta to be preserved, nested=%#v", nested)
	}
}

func TestBuildProviderSpecFromPayload_OmitsEmptyConfig(t *testing.T) {
	payload := providerPayload{
		Name: "binance-ui-test",
		Adapter: providerAdapterPayload{
			Identifier: "binance",
			Config: map[string]any{
				"api_key": "",
				"nested": map[string]any{
					"secret": " ",
				},
			},
		},
	}

	spec, _, err := buildProviderSpecFromPayload(payload)
	if err != nil {
		t.Fatalf("buildProviderSpecFromPayload returned error: %v", err)
	}
	if _, ok := spec.Config["config"]; ok {
		t.Fatalf("expected empty config map to be omitted, got %#v", spec.Config["config"])
	}
}

func TestHandleProviderDeleteBlockedWhenInUse(t *testing.T) {
	appCfg := config.AppConfig{}

	poolMgr := pool.NewPoolManager()
	if err := poolMgr.RegisterPool("Event", 8, 8, func() interface{} { return new(schema.Event) }); err != nil {
		t.Fatalf("register Event pool: %v", err)
	}
	if err := poolMgr.RegisterPool("OrderRequest", 4, 4, func() interface{} { return new(schema.OrderRequest) }); err != nil {
		t.Fatalf("register OrderRequest pool: %v", err)
	}

	bus := eventbus.NewMemoryBus(eventbus.MemoryConfig{
		BufferSize:    16,
		FanoutWorkers: 1,
		Pools:         poolMgr,
	})

	table := dispatcher.NewTable()
	providerManager := provider.NewManager(nil, poolMgr, bus, table, log.New(ioDiscards{}, "", 0))
	logger := log.New(ioDiscards{}, "", 0)
	registrar := dispatcher.NewRegistrar(table, providerManager)
	lambdaManager := lambdaruntime.NewManager(appCfg, bus, poolMgr, providerManager, logger, registrar)

	server := &httpServer{
		manager:       lambdaManager,
		providers:     providerManager,
		baseProviders: map[string]struct{}{},
		baseLambdas:   map[string]struct{}{},
	}

	providerSpec := config.ProviderSpec{
		Name:    "binance",
		Adapter: "binance",
		Config: map[string]any{
			"identifier": "binance",
		},
	}
	if _, err := providerManager.Create(context.Background(), providerSpec, false); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	lambdaSpec := config.LambdaSpec{
		ID:        "logging-alpha",
		Strategy:  config.LambdaStrategySpec{Identifier: "logging", Config: map[string]any{}},
		Providers: []string{"binance"},
		ProviderSymbols: map[string]config.ProviderSymbols{
			"binance": {Symbols: []string{"BTC-USDT"}},
		},
	}
	if err := lambdaManager.StartFromManifest(config.LambdaManifest{Lambdas: []config.LambdaSpec{lambdaSpec}}); err != nil {
		t.Fatalf("start manifest: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/providers/binance", nil)
	res := httptest.NewRecorder()
	server.handleProviderResource(res, req, "binance")
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %d (%s)", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "logging-alpha") {
		t.Fatalf("expected dependent instance to be reported, body=%s", res.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/providers", nil)
	listRes := httptest.NewRecorder()
	server.listProviders(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list providers unexpected status %d", listRes.Code)
	}
	var payload struct {
		Providers []struct {
			Name                   string   `json:"name"`
			DependentInstanceCount int      `json:"dependentInstanceCount"`
			DependentInstances     []string `json:"dependentInstances"`
		}
	}
	if err := json.Unmarshal(listRes.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode providers: %v", err)
	}
	if len(payload.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(payload.Providers))
	}
	if payload.Providers[0].DependentInstanceCount != 1 {
		t.Fatalf("expected dependent instance count 1, got %d", payload.Providers[0].DependentInstanceCount)
	}
	if len(payload.Providers[0].DependentInstances) != 1 || payload.Providers[0].DependentInstances[0] != "logging-alpha" {
		t.Fatalf("unexpected dependent instances %#v", payload.Providers[0].DependentInstances)
	}
}

func TestProviderUsageInfersProvidersFromScope(t *testing.T) {
	appCfg := config.AppConfig{}

	poolMgr := pool.NewPoolManager()
	if err := poolMgr.RegisterPool("Event", 8, 8, func() interface{} { return new(schema.Event) }); err != nil {
		t.Fatalf("register Event pool: %v", err)
	}
	if err := poolMgr.RegisterPool("OrderRequest", 4, 4, func() interface{} { return new(schema.OrderRequest) }); err != nil {
		t.Fatalf("register OrderRequest pool: %v", err)
	}

	bus := eventbus.NewMemoryBus(eventbus.MemoryConfig{
		BufferSize:    16,
		FanoutWorkers: 1,
		Pools:         poolMgr,
	})

	table := dispatcher.NewTable()
	providerManager := provider.NewManager(nil, poolMgr, bus, table, log.New(ioDiscards{}, "", 0))
	logger := log.New(ioDiscards{}, "", 0)
	registrar := dispatcher.NewRegistrar(table, providerManager)
	lambdaManager := lambdaruntime.NewManager(appCfg, bus, poolMgr, providerManager, logger, registrar)

	if _, err := providerManager.Create(context.Background(), config.ProviderSpec{
		Name:    "binance",
		Adapter: "binance",
		Config: map[string]any{
			"identifier": "binance",
		},
	}, false); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	lambdaSpec := config.LambdaSpec{
		ID:       "logging-beta",
		Strategy: config.LambdaStrategySpec{Identifier: "logging", Config: map[string]any{}},
		ProviderSymbols: map[string]config.ProviderSymbols{
			"binance": {Symbols: []string{"BTC-USDT"}},
		},
	}
	if err := lambdaManager.StartFromManifest(config.LambdaManifest{Lambdas: []config.LambdaSpec{lambdaSpec}}); err != nil {
		t.Fatalf("start manifest: %v", err)
	}

	// Simulate a summary with no providers while scope is populated.
	managerValue := reflect.ValueOf(lambdaManager).Elem()
	specsField := managerValue.FieldByName("specs")
	if !specsField.IsValid() {
		t.Fatal("manager specs field missing")
	}
	specsField = reflect.NewAt(specsField.Type(), unsafe.Pointer(specsField.UnsafeAddr())).Elem()
	key := reflect.ValueOf("logging-beta")
	specValue := specsField.MapIndex(key)
	if !specValue.IsValid() {
		t.Fatal("expected spec for logging-beta")
	}
	spec := specValue.Interface().(config.LambdaSpec)
	spec.Providers = nil
	specsField.SetMapIndex(key, reflect.ValueOf(spec))

	server := &httpServer{
		manager:       lambdaManager,
		providers:     providerManager,
		baseProviders: map[string]struct{}{},
		baseLambdas:   map[string]struct{}{},
	}

	summaries := lambdaManager.Instances()
	var found bool
	for _, summary := range summaries {
		if summary.ID == "logging-beta" {
			found = true
			if len(summary.Providers) != 0 {
				t.Fatalf("expected summary providers empty, got %v", summary.Providers)
			}
		}
	}
	if !found {
		t.Fatal("expected logging-beta summary")
	}

	usage := server.providerUsage()
	dependents, ok := usage["binance"]
	if !ok {
		t.Fatalf("expected binance dependencies, got %#v", usage)
	}
	if len(dependents) != 1 || dependents[0] != "logging-beta" {
		t.Fatalf("unexpected dependents for binance: %#v", dependents)
	}
}

type ioDiscards struct{}

func (ioDiscards) Write(p []byte) (int, error) {
	return len(p), nil
}
