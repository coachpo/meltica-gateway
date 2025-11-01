package httpserver

import (
	"context"
	"log"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	lambdaruntime "github.com/coachpo/meltica/internal/app/lambda/runtime"
	"github.com/coachpo/meltica/internal/app/provider"
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
	if err := poolMgr.RegisterPool("Event", appCfg.Pools.Event.Size, appCfg.Pools.Event.QueueSize(), func() interface{} { return new(struct{}) }); err != nil {
		t.Fatalf("register Event pool: %v", err)
	}
	if err := poolMgr.RegisterPool("OrderRequest", appCfg.Pools.OrderRequest.Size, appCfg.Pools.OrderRequest.QueueSize(), func() interface{} { return new(struct{}) }); err != nil {
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
	if providerCfg["depth"] != float64(100) {
		t.Fatalf("expected depth to be retained, got %v", providerCfg["depth"])
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
	if err := poolMgr.RegisterPool("Event", 8, 8, func() interface{} { return new(struct{}) }); err != nil {
		t.Fatalf("register Event pool: %v", err)
	}
	if err := poolMgr.RegisterPool("OrderRequest", 4, 4, func() interface{} { return new(struct{}) }); err != nil {
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

type ioDiscards struct{}

func (ioDiscards) Write(p []byte) (int, error) {
	return len(p), nil
}
