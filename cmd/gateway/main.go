// Command gateway launches the Meltica runtime entrypoint.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	lambdaruntime "github.com/coachpo/meltica/internal/app/lambda/runtime"
	"github.com/coachpo/meltica/internal/app/provider"
	"github.com/coachpo/meltica/internal/domain/orderstore"
	"github.com/coachpo/meltica/internal/domain/outboxstore"
	"github.com/coachpo/meltica/internal/domain/providerstore"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/domain/strategystore"
	"github.com/coachpo/meltica/internal/infra/adapters"
	"github.com/coachpo/meltica/internal/infra/bus/eventbus"
	"github.com/coachpo/meltica/internal/infra/config"
	postgresstore "github.com/coachpo/meltica/internal/infra/persistence/postgres"
	"github.com/coachpo/meltica/internal/infra/pool"
	httpserver "github.com/coachpo/meltica/internal/infra/server/http"
	"github.com/coachpo/meltica/internal/infra/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sourcegraph/conc"
)

const (
	defaultConfigPath            = "config/app.yaml"
	gatewayLoggerPrefix          = "gateway "
	eventPoolName                = "Event"
	orderRequestPoolName         = "OrderRequest"
	shutdownTimeout              = 30 * time.Second
	controlServerShutdownTimeout = 5 * time.Second
	lifecycleShutdownTimeout     = 10 * time.Second
	dataBusShutdownTimeout       = 2 * time.Second
	poolManagerShutdownTimeout   = 5 * time.Second
	telemetryShutdownTimeout     = 5 * time.Second
	controlReadHeaderTimeout     = 5 * time.Second
	databaseConnectTimeout       = 15 * time.Second
	databaseShutdownTimeout      = 5 * time.Second
)

func main() {
	cfgPathFlag := parseFlags()
	ctx, cancel := newSignalContext()
	defer cancel()

	logger := newGatewayLogger()

	appCfg, err := config.Load(ctx, resolveConfigPath(cfgPathFlag))
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}
	logger.Printf("configuration loaded: env=%s, providers=%d",
		appCfg.Environment, len(appCfg.Providers))

	logger.Printf("providers configured: %d", len(appCfg.Providers))

	dbPool, err := initDatabase(ctx, logger, appCfg.Database)
	if err != nil {
		logger.Fatalf("connect database: %v", err)
	}
	providerStore := postgresstore.NewProviderStore(dbPool)
	strategyStore := postgresstore.NewStrategyStore(dbPool)
	orderStore := postgresstore.NewOrderStore(dbPool)
	outboxStore := postgresstore.NewOutboxStore(dbPool)

	telemetryProvider, err := initTelemetry(ctx, logger, appCfg)
	if err != nil {
		logger.Fatalf("initialize telemetry: %v", err)
	}

	poolMgr, err := buildPoolManager(appCfg.Pools)
	if err != nil {
		logger.Fatalf("initialise pools: %v", err)
	}

	var lifecycle conc.WaitGroup

	bus := newEventBus(appCfg.Eventbus, poolMgr, outboxStore, logger)

	table := dispatcher.NewTable()
	providerManager, err := initProviders(ctx, logger, appCfg, poolMgr, table, bus, providerStore)
	if err != nil {
		logger.Fatalf("initialise providers: %v", err)
	}

	registrar := dispatcher.NewRegistrar(table, providerManager)

	lambdaManager, err := startLambdaManager(ctx, appCfg, bus, poolMgr, providerManager, registrar, logger, strategyStore, orderStore)
	if err != nil {
		logger.Fatalf("initialise lambdas: %v", err)
	}
	logger.Printf("strategy instances registered: %d", len(lambdaManager.Instances()))

	apiServer := buildAPIServer(appCfg, lambdaManager, providerManager, orderStore, outboxStore)
	startAPIServer(&lifecycle, logger, apiServer)
	logger.Printf("control API listening on %s", apiServer.Addr)

	logger.Print("gateway started; awaiting shutdown signal")
	<-ctx.Done()
	logger.Print("shutdown signal received, initiating graceful shutdown")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	shutdownStart := time.Now()
	performGracefulShutdown(shutdownCtx, logger, gracefulShutdownConfig{
		server:     apiServer,
		mainCancel: cancel,
		lifecycle:  &lifecycle,
		dataBus:    bus,
		poolMgr:    poolMgr,
		telemetry:  telemetryProvider,
		dbPool:     dbPool,
	})

	logger.Printf("shutdown completed in %v", time.Since(shutdownStart))
}

func parseFlags() string {
	cfgPath := flag.String("config", "", fmt.Sprintf("Path to application configuration file (default: %s)", defaultConfigPath))
	flag.Parse()
	return *cfgPath
}

func newSignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

func newGatewayLogger() *log.Logger {
	return log.New(os.Stdout, gatewayLoggerPrefix, log.LstdFlags|log.Lmicroseconds)
}

func initTelemetry(ctx context.Context, logger *log.Logger, appCfg config.AppConfig) (*telemetry.Provider, error) {
	telemetryCfg := telemetry.DefaultConfig()
	if appCfg.Telemetry.OTLPEndpoint != "" {
		telemetryCfg.OTLPEndpoint = appCfg.Telemetry.OTLPEndpoint
	}
	if appCfg.Telemetry.ServiceName != "" {
		telemetryCfg.ServiceName = appCfg.Telemetry.ServiceName
	}
	telemetryCfg.Environment = string(appCfg.Environment)
	telemetryCfg.OTLPInsecure = appCfg.Telemetry.OTLPInsecure
	telemetryCfg.EnableMetrics = appCfg.Telemetry.EnableMetrics

	provider, err := telemetry.NewProvider(ctx, telemetryCfg)
	if err != nil {
		return nil, fmt.Errorf("initialize telemetry provider: %w", err)
	}

	if telemetryCfg.Enabled {
		logger.Printf("telemetry initialized: endpoint=%s, service=%s", telemetryCfg.OTLPEndpoint, telemetryCfg.ServiceName)
	} else {
		logger.Printf("telemetry disabled")
	}
	return provider, nil
}

func initDatabase(ctx context.Context, logger *log.Logger, dbCfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(dbCfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse database dsn: %w", err)
	}

	poolCfg.MaxConns = dbCfg.MaxConns
	poolCfg.MinConns = dbCfg.MinConns
	poolCfg.MaxConnLifetime = dbCfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = dbCfg.MaxConnIdleTime
	poolCfg.HealthCheckPeriod = dbCfg.HealthCheckPeriod
	poolCfg.ConnConfig.ConnectTimeout = databaseConnectTimeout

	connectCtx, cancel := context.WithTimeout(ctx, databaseConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	pingCtx, pingCancel := context.WithTimeout(ctx, databaseConnectTimeout)
	defer pingCancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database ping: %w", err)
	}

	logger.Printf("database connected: maxConns=%d minConns=%d runMigrations=%t",
		poolCfg.MaxConns, poolCfg.MinConns, dbCfg.RunMigrations)

	return pool, nil
}

func buildPoolManager(cfg config.PoolConfig) (*pool.PoolManager, error) {
	manager := pool.NewPoolManager()
	eventQueueSize := cfg.Event.QueueSize()
	if err := manager.RegisterPool(eventPoolName, cfg.Event.Size, eventQueueSize, func() interface{} { return new(schema.Event) }); err != nil {
		return nil, fmt.Errorf("register Event pool: %w", err)
	}
	orderQueueSize := cfg.OrderRequest.QueueSize()
	if err := manager.RegisterPool(orderRequestPoolName, cfg.OrderRequest.Size, orderQueueSize, func() interface{} { return new(schema.OrderRequest) }); err != nil {
		return nil, fmt.Errorf("register OrderRequest pool: %w", err)
	}
	return manager, nil
}

func newEventBus(cfg config.EventbusConfig, pools *pool.PoolManager, outbox outboxstore.Store, logger *log.Logger) eventbus.Bus {
	memoryBus := eventbus.NewMemoryBus(eventbus.MemoryConfig{
		BufferSize:    cfg.BufferSize,
		FanoutWorkers: cfg.FanoutWorkerCount(),
		Pools:         pools,
	})
	return eventbus.NewDurableBus(memoryBus, outbox, eventbus.WithDurableLogger(logger))
}

func initProviders(ctx context.Context, logger *log.Logger, appCfg config.AppConfig, poolMgr *pool.PoolManager, table *dispatcher.Table, bus eventbus.Bus, store providerstore.Store) (*provider.Manager, error) {
	registry := provider.NewRegistry()
	adapters.RegisterAll(registry)

	opts := []provider.Option{}
	if store != nil {
		opts = append(opts, provider.WithPersistence(store))
	}
	manager := provider.NewManager(registry, poolMgr, bus, table, logger, opts...)
	manager.SetLifecycleContext(ctx)
	restoreProviderSnapshots(ctx, logger, store, manager)
	specs, err := config.BuildProviderSpecs(appCfg.Providers)
	if err != nil {
		return nil, fmt.Errorf("build provider specs: %w", err)
	}
	started := 0
	for _, spec := range specs {
		if manager.HasProvider(spec.Name) {
			if _, err := manager.Update(ctx, spec, true); err != nil {
				return nil, fmt.Errorf("update provider %s: %w", spec.Name, err)
			}
		} else {
			if _, err := manager.Create(ctx, spec, true); err != nil {
				return nil, fmt.Errorf("create provider %s: %w", spec.Name, err)
			}
		}
		started++
	}
	if started > 0 {
		logger.Printf("providers started: %d", len(manager.Providers()))
	} else {
		logger.Printf("no providers configured; skipping provider startup")
	}

	return manager, nil
}

func restoreProviderSnapshots(ctx context.Context, logger *log.Logger, store providerstore.Store, manager *provider.Manager) {
	if store == nil || manager == nil {
		return
	}
	snapshots, err := store.LoadProviders(ctx)
	if err != nil {
		if logger != nil {
			logger.Printf("provider persistence load failed: %v", err)
		}
		return
	}
	if len(snapshots) == 0 {
		return
	}
	for _, snapshot := range snapshots {
		manager.Restore(snapshot)
	}
	if logger != nil {
		logger.Printf("provider snapshots restored: %d", len(snapshots))
	}
}

func restoreStrategySnapshots(ctx context.Context, logger *log.Logger, store strategystore.Store, manager *lambdaruntime.Manager) {
	if store == nil || manager == nil {
		return
	}
	snapshots, err := store.Load(ctx)
	if err != nil {
		if logger != nil {
			logger.Printf("strategy persistence load failed: %v", err)
		}
		return
	}
	if len(snapshots) == 0 {
		return
	}
	for _, snapshot := range snapshots {
		manager.RestoreSnapshot(ctx, snapshot)
	}
	if logger != nil {
		logger.Printf("strategy snapshots restored: %d", len(snapshots))
	}
}

func startLambdaManager(ctx context.Context, appCfg config.AppConfig, bus eventbus.Bus, poolMgr *pool.PoolManager, providers *provider.Manager, registrar lambdaruntime.RouteRegistrar, logger *log.Logger, strategyStore strategystore.Store, orderStore orderstore.Store) (*lambdaruntime.Manager, error) {
	manager, err := lambdaruntime.NewManager(appCfg, bus, poolMgr, providers, logger, registrar,
		lambdaruntime.WithStrategyStore(strategyStore),
		lambdaruntime.WithOrderStore(orderStore),
	)
	if err != nil {
		return nil, fmt.Errorf("init lambda manager: %w", err)
	}
	manager.SetLifecycleContext(ctx)
	restoreStrategySnapshots(ctx, logger, strategyStore, manager)
	return manager, nil
}

func buildAPIServer(appCfg config.AppConfig, lambdaManager *lambdaruntime.Manager, providerManager *provider.Manager, orderStore orderstore.Store, outboxStore outboxstore.Store) *http.Server {
	handler := httpserver.NewHandler(appCfg, lambdaManager, providerManager, orderStore, outboxStore)

	return &http.Server{
		Addr:                         appCfg.APIServer.Addr,
		Handler:                      handler,
		DisableGeneralOptionsHandler: false,
		TLSConfig:                    nil,
		ReadTimeout:                  0,
		WriteTimeout:                 0,
		IdleTimeout:                  0,
		MaxHeaderBytes:               0,
		TLSNextProto:                 nil,
		ConnState:                    nil,
		ErrorLog:                     nil,
		BaseContext:                  nil,
		ConnContext:                  nil,
		HTTP2:                        nil,
		Protocols:                    nil,
		ReadHeaderTimeout:            controlReadHeaderTimeout,
	}
}

func startAPIServer(lifecycle *conc.WaitGroup, logger *log.Logger, server *http.Server) {
	lifecycle.Go(func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Printf("control server: %v", err)
		}
	})
}

type gracefulShutdownConfig struct {
	server     *http.Server
	mainCancel context.CancelFunc
	lifecycle  *conc.WaitGroup
	dataBus    eventbus.Bus
	poolMgr    *pool.PoolManager
	telemetry  *telemetry.Provider
	dbPool     *pgxpool.Pool
}

func performGracefulShutdown(ctx context.Context, logger *log.Logger, cfg gracefulShutdownConfig) {
	shutdownStep := func(name string, timeout time.Duration, fn func(context.Context) error) {
		stepCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		logger.Printf("shutdown: %s...", name)
		if err := fn(stepCtx); err != nil {
			logger.Printf("shutdown: %s failed: %v", name, err)
		} else {
			logger.Printf("shutdown: %s completed", name)
		}
	}

	if cfg.server != nil {
		shutdownStep("stopping control server", controlServerShutdownTimeout, func(stepCtx context.Context) error {
			return cfg.server.Shutdown(stepCtx)
		})
	}

	logger.Print("shutdown: cancelling main context")
	if cfg.mainCancel != nil {
		cfg.mainCancel()
	}

	if cfg.lifecycle != nil {
		shutdownStep("waiting for lifecycle goroutines", lifecycleShutdownTimeout, func(stepCtx context.Context) error {
			done := make(chan struct{})
			go func() {
				cfg.lifecycle.Wait()
				close(done)
			}()
			select {
			case <-done:
				return nil
			case <-stepCtx.Done():
				return fmt.Errorf("timeout waiting for goroutines: %w", stepCtx.Err())
			}
		})
	}

	if cfg.dataBus != nil {
		shutdownStep("closing data bus", dataBusShutdownTimeout, func(stepCtx context.Context) error {
			done := make(chan struct{})
			go func() {
				cfg.dataBus.Close()
				close(done)
			}()
			select {
			case <-done:
				return nil
			case <-stepCtx.Done():
				return stepCtx.Err()
			}
		})
	}

	if cfg.poolMgr != nil {
		shutdownStep("shutting down pool manager", poolManagerShutdownTimeout, func(stepCtx context.Context) error {
			return cfg.poolMgr.Shutdown(stepCtx)
		})
	}

	if cfg.telemetry != nil {
		shutdownStep("shutting down telemetry", telemetryShutdownTimeout, func(stepCtx context.Context) error {
			return cfg.telemetry.Shutdown(stepCtx)
		})
	}

	if cfg.dbPool != nil {
		shutdownStep("closing database pool", databaseShutdownTimeout, func(context.Context) error {
			cfg.dbPool.Close()
			return nil
		})
	}
}

func resolveConfigPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}

	return filepath.Clean(defaultConfigPath)
}
