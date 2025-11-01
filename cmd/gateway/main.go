// Command gateway launches the Meltica runtime entrypoint.
package main

import (
	"context"
	"errors"
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
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/adapters"
	"github.com/coachpo/meltica/internal/infra/bus/eventbus"
	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
	httpserver "github.com/coachpo/meltica/internal/infra/server/http"
	"github.com/coachpo/meltica/internal/infra/telemetry"
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
)

func main() {
	cfgPathFlag := parseFlags()
	ctx, cancel := newSignalContext()
	defer cancel()

	logger := newGatewayLogger()

	configPath := resolveConfigPath(cfgPathFlag)

	appCfg, loadedFromFile, err := config.LoadOrDefault(ctx, configPath)
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}
	if !loadedFromFile {
		logger.Printf("configuration file not found, using defaults")
	}
	logger.Printf("configuration initialised: env=%s, providers=%d",
		appCfg.Environment, len(appCfg.Providers))

	logger.Printf("lambda manifest loaded: lambdas=%d", len(appCfg.LambdaManifest.Lambdas))
	logger.Printf("providers configured: %d", len(appCfg.Providers))

	appStore, err := config.NewAppConfigStore(appCfg, func(cfg config.AppConfig) error {
		return config.SaveAppConfig(configPath, cfg)
	})
	if err != nil {
		logger.Fatalf("initialise app config store: %v", err)
	}

	runtimeSnapshotPath := deriveRuntimeSnapshotPath(configPath)
	runtimeCfg := appCfg.Runtime
	if snapshot, err := config.LoadRuntimeSnapshot(runtimeSnapshotPath); err == nil {
		runtimeCfg = snapshot
		logger.Printf("runtime snapshot loaded from %s", runtimeSnapshotPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		logger.Fatalf("load runtime snapshot: %v", err)
	}

	runtimeStore, err := config.NewRuntimeStore(runtimeCfg)
	if err != nil {
		logger.Fatalf("initialise runtime config: %v", err)
	}
	runtimeSnapshot := runtimeStore.Snapshot()
	if err := appStore.SetRuntime(runtimeSnapshot); err != nil {
		logger.Fatalf("sync runtime config store: %v", err)
	}

	telemetryProvider, err := initTelemetry(ctx, logger, appCfg.Environment, runtimeSnapshot.Telemetry)
	if err != nil {
		logger.Fatalf("initialize telemetry: %v", err)
	}

	poolMgr, err := buildPoolManager(runtimeSnapshot.Pools)
	if err != nil {
		logger.Fatalf("initialise pools: %v", err)
	}

	var lifecycle conc.WaitGroup

	bus := newEventBus(runtimeSnapshot.Eventbus, poolMgr)

	table := dispatcher.NewTable()
	providerManager, err := initProviders(ctx, logger, appCfg, poolMgr, table, bus)
	if err != nil {
		logger.Fatalf("initialise providers: %v", err)
	}

	registrar := dispatcher.NewRegistrar(table, providerManager)

	lambdaManager, err := startLambdaManager(ctx, appCfg, runtimeStore, bus, poolMgr, providerManager, registrar, logger)
	if err != nil {
		logger.Fatalf("initialise lambdas: %v", err)
	}
	logger.Printf("strategy instances registered: %d", len(lambdaManager.Instances()))

	apiServer := buildAPIServer(runtimeSnapshot.APIServer, appCfg.Environment, appCfg.Meta, runtimeStore, lambdaManager, providerManager, appStore)
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

func initTelemetry(ctx context.Context, logger *log.Logger, env config.Environment, cfg config.TelemetryConfig) (*telemetry.Provider, error) {
	telemetryCfg := telemetry.DefaultConfig()
	if cfg.OTLPEndpoint != "" {
		telemetryCfg.OTLPEndpoint = cfg.OTLPEndpoint
	}
	if cfg.ServiceName != "" {
		telemetryCfg.ServiceName = cfg.ServiceName
	}
	telemetryCfg.Environment = string(env)
	telemetryCfg.OTLPInsecure = cfg.OTLPInsecure
	telemetryCfg.EnableMetrics = cfg.EnableMetrics

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

func newEventBus(cfg config.EventbusConfig, pools *pool.PoolManager) eventbus.Bus {
	return eventbus.NewMemoryBus(eventbus.MemoryConfig{
		BufferSize:    cfg.BufferSize,
		FanoutWorkers: cfg.FanoutWorkerCount(),
		Pools:         pools,
	})
}

func initProviders(ctx context.Context, logger *log.Logger, appCfg config.AppConfig, poolMgr *pool.PoolManager, table *dispatcher.Table, bus eventbus.Bus) (*provider.Manager, error) {
	registry := provider.NewRegistry()
	adapters.RegisterAll(registry)

	manager := provider.NewManager(registry, poolMgr, bus, table, logger)
	manager.SetLifecycleContext(ctx)
	specs, err := config.BuildProviderSpecs(appCfg.Providers)
	if err != nil {
		return nil, fmt.Errorf("build provider specs: %w", err)
	}
	if len(specs) > 0 {
		if _, err := manager.Start(ctx, specs); err != nil {
			return nil, fmt.Errorf("start providers: %w", err)
		}
		logger.Printf("providers started: %d", len(manager.Providers()))
	} else {
		logger.Print("no providers configured; skipping provider bootstrap")
	}

	return manager, nil
}

func startLambdaManager(ctx context.Context, appCfg config.AppConfig, runtimeStore *config.RuntimeStore, bus eventbus.Bus, poolMgr *pool.PoolManager, providers *provider.Manager, registrar lambdaruntime.RouteRegistrar, logger *log.Logger) (*lambdaruntime.Manager, error) {
	manager := lambdaruntime.NewManager(appCfg, runtimeStore, bus, poolMgr, providers, logger, registrar)
	manager.SetLifecycleContext(ctx)
	if err := manager.StartFromManifest(ctx, appCfg.LambdaManifest); err != nil {
		return nil, fmt.Errorf("start manifest lambdas: %w", err)
	}
	return manager, nil
}

func buildAPIServer(cfg config.APIServerConfig, env config.Environment, meta config.MetaConfig, runtimeStore *config.RuntimeStore, lambdaManager *lambdaruntime.Manager, providerManager *provider.Manager, appStore *config.AppConfigStore) *http.Server {
	handler := httpserver.NewHandler(env, meta, lambdaManager, providerManager, runtimeStore, appStore)

	return &http.Server{
		Addr:                         cfg.Addr,
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
}

func resolveConfigPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}

	return filepath.Clean(defaultConfigPath)
}

func deriveRuntimeSnapshotPath(configPath string) string {
	cleaned := filepath.Clean(configPath)
	dir := filepath.Dir(cleaned)
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "runtime.snapshot.json")
}
