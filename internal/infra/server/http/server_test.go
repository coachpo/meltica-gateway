package httpserver

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	"github.com/coachpo/meltica/internal/app/lambda/js"
	lambdaruntime "github.com/coachpo/meltica/internal/app/lambda/runtime"
	"github.com/coachpo/meltica/internal/app/provider"
	"github.com/coachpo/meltica/internal/domain/orderstore"
	"github.com/coachpo/meltica/internal/domain/outboxstore"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/bus/eventbus"
	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
	strategiestest "github.com/coachpo/meltica/internal/testutil/strategies"
)

func TestDecodeRiskConfig_NormalizesAllowedOrderTypes(t *testing.T) {
	payload := `{
		"maxPositionSize": "10",
		"maxNotionalValue": "100",
		"notionalCurrency": "USD",
		"orderThrottle": 5,
		"orderBurst": 1,
		"maxConcurrentOrders": 0,
		"priceBandPercent": 0,
		"allowedOrderTypes": [" limit", "LIMIT", "Market", "market ", "Stop "],
		"killSwitchEnabled": false,
		"maxRiskBreaches": 0,
		"circuitBreaker": {
			"enabled": false,
			"threshold": 0,
			"cooldown": ""
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/risk", strings.NewReader(payload))
	cfg, err := decodeRiskConfig(req)
	if err != nil {
		t.Fatalf("decodeRiskConfig: %v", err)
	}
	expected := []string{"limit", "Market", "Stop"}
	if !reflect.DeepEqual(cfg.AllowedOrderTypes, expected) {
		t.Fatalf("expected allowed order types %v, got %v", expected, cfg.AllowedOrderTypes)
	}
}

func TestBuildContextBackup(t *testing.T) {
	strategyDir := strategiestest.WriteStubStrategies(t)
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
		Strategies: config.StrategiesConfig{Directory: strategyDir},
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
	lambdaManager, err := lambdaruntime.NewManager(appCfg, bus, poolMgr, providerManager, logger, registrar)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	lambdaSpec := config.LambdaSpec{
		ID:        "alpha",
		Strategy:  config.LambdaStrategySpec{Identifier: "logging", Config: map[string]any{}},
		Providers: []string{"binance"},
		ProviderSymbols: map[string]config.ProviderSymbols{
			"binance": {Symbols: []string{"BTC-USDT"}},
		},
	}
	if _, err := lambdaManager.Create(lambdaSpec); err != nil {
		t.Fatalf("Create lambda spec: %v", err)
	}

	server := &httpServer{
		manager:       lambdaManager,
		providers:     providerManager,
		orderStore:    nil,
		baseProviders: map[string]struct{}{},
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

func TestWriteStrategyModuleErrorReturnsDiagnostics(t *testing.T) {
	server := &httpServer{}
	recorder := httptest.NewRecorder()
	diag := js.Diagnostic{
		Stage:   js.DiagnosticStageValidation,
		Message: "displayName required",
		Line:    0,
		Column:  0,
		Hint:    "metadata.displayName",
	}
	diagErr := js.NewDiagnosticError("metadata validation failed", nil, diag)

	server.writeStrategyModuleError(recorder, diagErr)

	result := recorder.Result()
	t.Cleanup(func() { _ = result.Body.Close() })
	if result.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", result.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(result.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["error"] != "strategy_validation_failed" {
		t.Fatalf("expected error strategy_validation_failed, got %v", payload["error"])
	}
	if payload["message"] != "metadata validation failed" {
		t.Fatalf("expected message preserved, got %v", payload["message"])
	}
	diagnostics, ok := payload["diagnostics"].([]any)
	if !ok || len(diagnostics) == 0 {
		t.Fatalf("expected diagnostics in payload")
	}
	first, ok := diagnostics[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected diagnostic payload type %T", diagnostics[0])
	}
	if first["stage"] != string(js.DiagnosticStageValidation) {
		t.Fatalf("expected validation stage, got %v", first["stage"])
	}
	if first["message"] != diag.Message {
		t.Fatalf("expected diagnostic message %q, got %v", diag.Message, first["message"])
	}
}

func TestApplyContextBackupRestoresState(t *testing.T) {
	strategyDir := strategiestest.WriteStubStrategies(t)
	appCfg := config.AppConfig{
		Strategies: config.StrategiesConfig{Directory: strategyDir},
	}

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
	lambdaManager, err := lambdaruntime.NewManager(appCfg, bus, poolMgr, providerManager, logger, registrar)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	server := &httpServer{
		manager:       lambdaManager,
		providers:     providerManager,
		orderStore:    nil,
		baseProviders: map[string]struct{}{},
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
	strategyDir := strategiestest.WriteStubStrategies(t)
	appCfg := config.AppConfig{
		Strategies: config.StrategiesConfig{Directory: strategyDir},
	}

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
	lambdaManager, err := lambdaruntime.NewManager(appCfg, bus, poolMgr, providerManager, logger, registrar)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	server := &httpServer{
		manager:       lambdaManager,
		providers:     providerManager,
		orderStore:    nil,
		baseProviders: map[string]struct{}{},
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
	if _, err := lambdaManager.Create(lambdaSpec); err != nil {
		t.Fatalf("create lambda: %v", err)
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
	strategyDir := strategiestest.WriteStubStrategies(t)
	appCfg := config.AppConfig{
		Strategies: config.StrategiesConfig{Directory: strategyDir},
	}

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
	lambdaManager, err := lambdaruntime.NewManager(appCfg, bus, poolMgr, providerManager, logger, registrar)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

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
	if _, err := lambdaManager.Create(lambdaSpec); err != nil {
		t.Fatalf("create lambda: %v", err)
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
		orderStore:    nil,
		baseProviders: map[string]struct{}{},
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

func TestCreateProviderRespondsAcceptedPending(t *testing.T) {
	registry := provider.NewRegistry()
	started := make(chan struct{}, 1)
	registry.Register("stub", func(ctx context.Context, pools *pool.PoolManager, cfg map[string]any) (provider.Instance, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		name, _ := cfg["provider_name"].(string)
		if name == "" {
			name = "stub"
		}
		return &httpTestProviderInstance{name: name}, nil
	})

	logger := log.New(ioDiscards{}, "", 0)
	providerManager := provider.NewManager(registry, nil, nil, dispatcher.NewTable(), logger)

	server := &httpServer{
		providers:     providerManager,
		orderStore:    nil,
		baseProviders: map[string]struct{}{},
	}

	body := `{"name":"stub","adapter":{"identifier":"stub","config":{}},"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(body))
	res := httptest.NewRecorder()

	server.createProvider(res, req)

	if res.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", res.Code)
	}
	if location := res.Header().Get("Location"); location != "/providers/stub" {
		t.Fatalf("expected Location header /providers/stub, got %q", location)
	}

	var detail provider.RuntimeDetail
	if err := json.Unmarshal(res.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if detail.Name != "stub" {
		t.Fatalf("expected provider name stub, got %s", detail.Name)
	}
	if detail.Status != provider.StatusPending {
		t.Fatalf("expected pending status, got %s", detail.Status)
	}
	if detail.Running {
		t.Fatal("expected provider not running immediately after creation")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for provider factory")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		meta, ok := providerManager.ProviderMetadataFor("stub")
		if ok && meta.Status == provider.StatusRunning && meta.Running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected provider to transition to running state")
}

func TestStartProviderActionReturnsAccepted(t *testing.T) {
	registry := provider.NewRegistry()
	started := make(chan struct{}, 1)
	registry.Register("stub", func(ctx context.Context, pools *pool.PoolManager, cfg map[string]any) (provider.Instance, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		name, _ := cfg["provider_name"].(string)
		if name == "" {
			name = "stub"
		}
		return &httpTestProviderInstance{name: name}, nil
	})

	logger := log.New(ioDiscards{}, "", 0)
	providerManager := provider.NewManager(registry, nil, nil, dispatcher.NewTable(), logger)

	spec := config.ProviderSpec{
		Name:    "stub",
		Adapter: "stub",
		Config: map[string]any{
			"identifier":    "stub",
			"provider_name": "stub",
		},
	}
	if _, err := providerManager.Create(context.Background(), spec, false); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	server := &httpServer{
		providers:     providerManager,
		orderStore:    nil,
		baseProviders: map[string]struct{}{},
	}

	req := httptest.NewRequest(http.MethodPost, "/providers/stub/start", nil)
	res := httptest.NewRecorder()

	server.handleProviderAction(res, req, "stub", "start")

	if res.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", res.Code)
	}
	if location := res.Header().Get("Location"); location != "/providers/stub" {
		t.Fatalf("expected Location header /providers/stub, got %q", location)
	}

	var detail provider.RuntimeDetail
	if err := json.Unmarshal(res.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if detail.Status != provider.StatusStarting {
		t.Fatalf("expected starting status, got %s", detail.Status)
	}
	if detail.Running {
		t.Fatal("expected provider not running during startup")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for provider factory")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		meta, ok := providerManager.ProviderMetadataFor("stub")
		if ok && meta.Status == provider.StatusRunning && meta.Running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected provider to transition to running state")
}

func TestInstanceOrdersEndpointReturnsRecords(t *testing.T) {
	store := &stubOrderStore{
		orders: []orderstore.OrderRecord{
			{
				Order: orderstore.Order{
					ID:               "ord-1",
					Provider:         "binance",
					StrategyInstance: "demo",
					ClientOrderID:    "ord-1",
					Symbol:           "BTC-USDT",
					Side:             "BUY",
					Type:             "LIMIT",
					Quantity:         "1.000",
					Price:            strPtr("21000"),
					State:            "ACK",
					PlacedAt:         1_700_000_000,
					Metadata:         map[string]any{"note": "test"},
				},
				AcknowledgedAt: strInt64Ptr(1_700_000_001),
				CreatedAt:      1_700_000_000,
				UpdatedAt:      1_700_000_010,
			},
		},
	}
	handler := NewHandler(config.AppConfig{}, nil, nil, store, nil)

	req := httptest.NewRequest(http.MethodGet, "/strategy/instances/demo/orders?limit=9999", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", res.Code, res.Body.String())
	}

	var payload struct {
		Orders []orderstore.OrderRecord `json:"orders"`
		Count  int                      `json:"count"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("expected count 1, got %d", payload.Count)
	}
	if len(payload.Orders) != 1 || payload.Orders[0].Order.ID != "ord-1" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestInstanceOrdersEndpointInvalidLimit(t *testing.T) {
	handler := NewHandler(config.AppConfig{}, nil, nil, &stubOrderStore{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/strategy/instances/demo/orders?limit=bogus", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", res.Code)
	}
}

func TestProviderBalancesEndpointReturnsRecords(t *testing.T) {
	store := &stubOrderStore{
		balances: []orderstore.BalanceRecord{
			{
				BalanceSnapshot: orderstore.BalanceSnapshot{
					Provider:   "binance",
					Asset:      "USDT",
					Total:      "1000",
					Available:  "500",
					SnapshotAt: 1_700_000_000,
					Metadata:   map[string]any{"note": "snapshot"},
				},
				CreatedAt: 1_700_000_000,
				UpdatedAt: 1_700_000_100,
			},
		},
	}
	handler := NewHandler(config.AppConfig{}, nil, nil, store, nil)

	req := httptest.NewRequest(http.MethodGet, "/providers/binance/balances", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", res.Code, res.Body.String())
	}

	var payload struct {
		Balances []orderstore.BalanceRecord `json:"balances"`
		Count    int                        `json:"count"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Count != 1 || len(payload.Balances) != 1 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if payload.Balances[0].BalanceSnapshot.Provider != "binance" {
		t.Fatalf("unexpected provider in payload: %#v", payload.Balances[0])
	}
}

func TestOutboxListRequiresStore(t *testing.T) {
	handler := NewHandler(config.AppConfig{}, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/outbox", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", res.Code)
	}
}

func TestOutboxListReturnsRecords(t *testing.T) {
	store := &stubOutboxStore{
		records: []outboxstore.EventRecord{
			{
				ID:            1,
				AggregateType: "provider",
				AggregateID:   "binance",
				EventType:     "Trade",
				Payload:       json.RawMessage(`{"eventId":"evt-1"}`),
			},
		},
	}
	handler := NewHandler(config.AppConfig{}, nil, nil, nil, store)

	req := httptest.NewRequest(http.MethodGet, "/outbox?limit=10", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", res.Code, res.Body.String())
	}
	var payload struct {
		Events []outboxstore.EventRecord `json:"events"`
		Count  int                       `json:"count"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Count != 1 || len(payload.Events) != 1 || payload.Events[0].ID != 1 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestOutboxDeleteRemovesRecord(t *testing.T) {
	store := &stubOutboxStore{}
	handler := NewHandler(config.AppConfig{}, nil, nil, nil, store)

	req := httptest.NewRequest(http.MethodDelete, "/outbox/42", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", res.Code, res.Body.String())
	}
	if len(store.deleted) != 1 || store.deleted[0] != 42 {
		t.Fatalf("expected deleted id 42, got %v", store.deleted)
	}
}

func TestOutboxDeleteInvalidID(t *testing.T) {
	store := &stubOutboxStore{}
	handler := NewHandler(config.AppConfig{}, nil, nil, nil, store)

	req := httptest.NewRequest(http.MethodDelete, "/outbox/not-an-id", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", res.Code)
	}
}

type stubOrderStore struct {
	orders     []orderstore.OrderRecord
	executions []orderstore.ExecutionRecord
	balances   []orderstore.BalanceRecord
}

func (s *stubOrderStore) CreateOrder(context.Context, orderstore.Order) error             { return nil }
func (s *stubOrderStore) UpdateOrder(context.Context, orderstore.OrderUpdate) error       { return nil }
func (s *stubOrderStore) RecordExecution(context.Context, orderstore.Execution) error     { return nil }
func (s *stubOrderStore) UpsertBalance(context.Context, orderstore.BalanceSnapshot) error { return nil }
func (s *stubOrderStore) WithTransaction(ctx context.Context, fn func(context.Context, orderstore.Tx) error) error {
	if fn == nil {
		return nil
	}
	return fn(ctx, s)
}
func (s *stubOrderStore) ListOrders(context.Context, orderstore.OrderQuery) ([]orderstore.OrderRecord, error) {
	return s.orders, nil
}
func (s *stubOrderStore) ListExecutions(context.Context, orderstore.ExecutionQuery) ([]orderstore.ExecutionRecord, error) {
	return s.executions, nil
}
func (s *stubOrderStore) ListBalances(context.Context, orderstore.BalanceQuery) ([]orderstore.BalanceRecord, error) {
	return s.balances, nil
}

func strPtr(value string) *string {
	return &value
}

func strInt64Ptr(value int64) *int64 {
	return &value
}

type stubOutboxStore struct {
	records []outboxstore.EventRecord
	deleted []int64
}

func (s *stubOutboxStore) Enqueue(context.Context, outboxstore.Event) (outboxstore.EventRecord, error) {
	return outboxstore.EventRecord{}, nil
}

func (s *stubOutboxStore) ListPending(context.Context, int) ([]outboxstore.EventRecord, error) {
	return s.records, nil
}

func (s *stubOutboxStore) MarkDelivered(context.Context, int64) error { return nil }

func (s *stubOutboxStore) MarkFailed(context.Context, int64, string) error { return nil }

func (s *stubOutboxStore) Delete(_ context.Context, id int64) error {
	s.deleted = append(s.deleted, id)
	return nil
}

type ioDiscards struct{}

func (ioDiscards) Write(p []byte) (int, error) {
	return len(p), nil
}

type httpTestProviderInstance struct {
	name string
}

func (i *httpTestProviderInstance) Name() string                    { return i.name }
func (i *httpTestProviderInstance) Start(ctx context.Context) error { return nil }
func (i *httpTestProviderInstance) Events() <-chan *schema.Event    { return nil }
func (i *httpTestProviderInstance) Errors() <-chan error            { return nil }
func (i *httpTestProviderInstance) SubmitOrder(ctx context.Context, req schema.OrderRequest) error {
	return nil
}
func (i *httpTestProviderInstance) SubscribeRoute(route dispatcher.Route) error   { return nil }
func (i *httpTestProviderInstance) UnsubscribeRoute(route dispatcher.Route) error { return nil }
func (i *httpTestProviderInstance) Instruments() []schema.Instrument              { return nil }

func TestFilterModuleSummaries(t *testing.T) {
	modules := []js.ModuleSummary{
		{
			Name:      "alpha",
			Revisions: []js.ModuleRevision{{Hash: "sha256:a"}},
			Running:   []js.ModuleUsage{{Hash: "sha256:a", Count: 2, Instances: []string{"one"}}},
		},
		{
			Name:      "beta",
			Revisions: []js.ModuleRevision{{Hash: "sha256:b"}},
		},
	}

	values := url.Values{}
	values.Set("runningOnly", "true")
	filtered, total, offset, limit, err := filterModuleSummaries(modules, values)
	if err != nil {
		t.Fatalf("runningOnly filter: %v", err)
	}
	if total != 1 || len(filtered) != 1 {
		t.Fatalf("expected single running module, got total=%d filtered=%d", total, len(filtered))
	}
	if filtered[0].Name != "alpha" {
		t.Fatalf("expected alpha module, got %s", filtered[0].Name)
	}
	if offset != 0 || limit != -1 {
		t.Fatalf("unexpected pagination defaults offset=%d limit=%d", offset, limit)
	}

	values = url.Values{}
	values.Set("hash", "sha256:b")
	filtered, total, _, _, err = filterModuleSummaries(modules, values)
	if err != nil {
		t.Fatalf("hash filter: %v", err)
	}
	if total != 1 || len(filtered) != 1 {
		t.Fatalf("expected single module after hash filter, got total=%d filtered=%d", total, len(filtered))
	}
	if len(filtered[0].Revisions) != 1 || filtered[0].Revisions[0].Hash != "sha256:b" {
		t.Fatalf("expected revision filtered to sha256:b, got %+v", filtered[0].Revisions)
	}

	values = url.Values{}
	values.Set("strategy", "alpha")
	values.Set("limit", "1")
	values.Set("offset", "0")
	filtered, total, offset, limit, err = filterModuleSummaries(modules, values)
	if err != nil {
		t.Fatalf("pagination filter: %v", err)
	}
	if total != 1 || len(filtered) != 1 {
		t.Fatalf("expected single module for strategy alpha, got total=%d filtered=%d", total, len(filtered))
	}
	if offset != 0 || limit != 1 {
		t.Fatalf("expected offset=0 limit=1, got offset=%d limit=%d", offset, limit)
	}

	values = url.Values{}
	values.Set("limit", "-1")
	if _, _, _, _, err := filterModuleSummaries(modules, values); err == nil {
		t.Fatalf("expected error for negative limit")
	}
}

func TestBuildUsageSelector(t *testing.T) {
	if sel := buildUsageSelector("noop@hash", "noop", "hash"); sel != "noop@hash" {
		t.Fatalf("expected selector passthrough, got %s", sel)
	}
	if sel := buildUsageSelector("", "Logging", "sha256:abc"); sel != "logging@sha256:abc" {
		t.Fatalf("expected auto selector, got %s", sel)
	}
	if sel := buildUsageSelector("", "", "sha256:abc"); sel != "" {
		t.Fatalf("expected empty selector when identifier missing, got %s", sel)
	}
	expected := strategyModulePrefix + url.PathEscape("logging@sha256:abc") + strategyUsageSuffix
	if url := buildModuleUsageURL("logging@sha256:abc"); url != expected {
		t.Fatalf("unexpected usage URL %s", url)
	}
}

func TestHandleStrategyModuleTagRoutes(t *testing.T) {
	server := &httpServer{}
	req := httptest.NewRequest(http.MethodDelete, "/strategies/modules/logging/tags/v1.0.1", nil)
	rec := httptest.NewRecorder()
	server.handleStrategyModule(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 due to nil manager, got %d (%s)", rec.Code, rec.Body.String())
	}
}
