// Command gateway launches the Meltica runtime entrypoint.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coachpo/meltica/config"
	"github.com/coachpo/meltica/internal/adapters/binance"
	"github.com/coachpo/meltica/internal/bus/controlbus"
	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/conductor"
	"github.com/coachpo/meltica/internal/consumer"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/observability"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
	"github.com/coachpo/meltica/lib/telemetry"
)

func main() {
	cfgPath := flag.String("config", "config/streaming.yaml", "Path to streaming configuration file")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	streamingCfg, err := config.LoadStreamingConfig(ctx, resolveConfigPath(*cfgPath))
	if err != nil {
		log.Fatalf("load streaming config: %v", err)
	}

	logger := log.New(os.Stdout, "gateway ", log.LstdFlags|log.Lmicroseconds)
	logger.Printf("configuration loaded: routes=%d", len(streamingCfg.Dispatcher.Routes))

	telemetryProviders, shutdownTelemetry, err := telemetry.Init(ctx, streamingCfg.Telemetry)
	if err != nil {
		log.Fatalf("init telemetry: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			logger.Printf("telemetry shutdown: %v", err)
		}
	}()
	_ = telemetryProviders

	poolMgr := pool.NewPoolManager()
	registerPool := func(name string, capacity int, factory func() interface{}) {
		if err := poolMgr.RegisterPool(name, capacity, factory); err != nil {
			log.Fatalf("register pool %s: %v", name, err)
		}
	}
	registerPool("WsFrame", 200, func() interface{} { return new(schema.WsFrame) })
	registerPool("ProviderRaw", 200, func() interface{} { return new(schema.ProviderRaw) })
	registerPool("CanonicalEvent", 300, func() interface{} { return new(schema.CanonicalEvent) })
	registerPool("MergedEvent", 50, func() interface{} { return new(schema.MergedEvent) })
	registerPool("OrderRequest", 20, func() interface{} { return new(schema.OrderRequest) })
	registerPool("ExecReport", 20, func() interface{} { return new(schema.ExecReport) })
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := poolMgr.Shutdown(shutdownCtx); err != nil {
			logger.Printf("pool shutdown: %v", err)
		}
	}()

	bus := databus.NewMemoryBus(databus.MemoryConfig{
		BufferSize: streamingCfg.Databus.BufferSize,
	})
	defer bus.Close()

	controlBus := controlbus.NewMemoryBus(controlbus.MemoryConfig{BufferSize: 16})
	defer controlBus.Close()

	table := dispatcher.NewTable()
	for name, cfg := range streamingCfg.Dispatcher.Routes {
		if err := table.Upsert(routeFromConfig(name, cfg)); err != nil {
			log.Fatalf("load route %s: %v", name, err)
		}
	}

	parser := binance.NewParserWithPool("binance", poolMgr)
	wsProvider := binance.NewDefaultFrameProvider(streamingCfg.Adapters.Binance.WS.PublicURL, streamingCfg.Adapters.Binance.WS.HandshakeTimeout)
	restBase := streamingCfg.Adapters.Binance.REST.BaseURL
	if restBase == "" {
		restBase = "https://api.binance.com"
	}
	restFetcher := binance.NewHTTPSnapshotFetcher(restBase, streamingCfg.Adapters.Binance.WS.HandshakeTimeout)

	wsClient := binance.NewWSClient("binance", wsProvider, parser, time.Now, poolMgr)
	restClient := binance.NewRESTClient(restFetcher, parser, time.Now)

	var providerOpts binance.ProviderOptions
	provider := binance.NewProvider("binance", wsClient, restClient, providerOpts)

	go func() {
		if err := provider.Start(ctx); err != nil && err != context.Canceled {
			logger.Printf("provider: %v", err)
		}
	}()

	eventOrchestrator := conductor.NewEventOrchestratorWithPool(poolMgr)
	eventOrchestrator.AddProvider("binance", provider.Events(), provider.Errors())
	go func() {
		if err := eventOrchestrator.Start(ctx); err != nil && err != context.Canceled {
			logger.Printf("orchestrator: %v", err)
		}
	}()

	runtimeCfg := config.DispatcherRuntimeConfig{
		StreamOrdering: config.StreamOrderingConfig{
			LatenessTolerance: 150 * time.Millisecond,
			FlushInterval:     50 * time.Millisecond,
			MaxBufferSize:     1024,
		},
		Backpressure: config.BackpressureConfig{
			TokenRatePerStream: 1000,
			TokenBurst:         100,
		},
		CoalescableTypes: []string{"Ticker", "BookUpdate", "KlineSummary"},
	}

	dispatcherRuntime := dispatcher.NewRuntime(bus, poolMgr, runtimeCfg, observability.NewRuntimeMetrics())
	dispatchErrs := dispatcherRuntime.Start(ctx, eventOrchestrator.Events())

	go logErrors(logger, "orchestrator", eventOrchestrator.Errors())
	go logErrors(logger, "dispatcher", dispatchErrs)

	subscriptionManager := binance.NewSubscriptionManager(provider)
	for _, route := range table.Routes() {
		if err := subscriptionManager.Activate(ctx, route); err != nil {
			logger.Printf("subscribe route %s: %v", route.Type, err)
		}
	}

	eventTypes := collectEventTypes(table.Routes())
	gatewayConsumer := consumer.NewConsumer("gateway", bus, logger)
	consumerEvents, consumerErrs := gatewayConsumer.Start(ctx, eventTypes)
	go drainConsumer(logger, consumerEvents, consumerErrs)
	controller := dispatcher.NewController(table, controlBus, subscriptionManager)
	go func() {
		if err := controller.Start(ctx); err != nil && err != context.Canceled {
			logger.Printf("controller: %v", err)
		}
	}()

	controlAddr := ":8080"
	controlHandler := dispatcher.NewControlHTTPHandler(controlBus)
	controlServer := new(http.Server)
	controlServer.Addr = controlAddr
	controlServer.Handler = controlHandler
	controlServer.ReadHeaderTimeout = 5 * time.Second
	go func() {
		if err := controlServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Printf("control server: %v", err)
		}
	}()
	logger.Printf("control API listening on %s", controlAddr)

	logger.Print("gateway started; awaiting shutdown signal")
	<-ctx.Done()
	logger.Print("shutdown requested")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := controlServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("control server shutdown: %v", err)
	}
	if err := shutdownCtx.Err(); err != nil {
		logger.Printf("shutdown deadline reached: %v", err)
	}
}

func collectEventTypes(routes map[schema.CanonicalType]dispatcher.Route) []schema.EventType {
	set := make(map[schema.EventType]struct{})
	for canonical := range routes {
		if evtType, ok := canonicalToEventType(canonical); ok {
			set[evtType] = struct{}{}
		}
	}
	out := make([]schema.EventType, 0, len(set))
	for evtType := range set {
		out = append(out, evtType)
	}
	return out
}

func canonicalToEventType(typ schema.CanonicalType) (schema.EventType, bool) {
	switch typ {
	case schema.CanonicalType("ORDERBOOK.SNAPSHOT"):
		return schema.EventTypeBookSnapshot, true
	case schema.CanonicalType("ORDERBOOK.DELTA"), schema.CanonicalType("ORDERBOOK.UPDATE"):
		return schema.EventTypeBookUpdate, true
	case schema.CanonicalType("TRADE"):
		return schema.EventTypeTrade, true
	case schema.CanonicalType("TICKER"):
		return schema.EventTypeTicker, true
	case schema.CanonicalType("EXECUTION.REPORT"):
		return schema.EventTypeExecReport, true
	case schema.CanonicalType("KLINE.SUMMARY"):
		return schema.EventTypeKlineSummary, true
	default:
		return "", false
	}
}

func routeFromConfig(name string, cfg config.RouteConfig) dispatcher.Route {
	filters := make([]dispatcher.FilterRule, 0, len(cfg.Filters))
	for _, f := range cfg.Filters {
		filters = append(filters, dispatcher.FilterRule{Field: f.Field, Op: f.Op, Value: f.Value})
	}
	restFns := make([]dispatcher.RestFn, 0, len(cfg.RestFns))
	for _, rf := range cfg.RestFns {
		restFns = append(restFns, dispatcher.RestFn{Name: rf.Name, Endpoint: rf.Endpoint, Interval: rf.Interval, Parser: rf.Parser})
	}
	return dispatcher.Route{
		Type:     schema.CanonicalType(name),
		WSTopics: cfg.WSTopics,
		RestFns:  restFns,
		Filters:  filters,
	}
}

func drainConsumer(logger *log.Logger, events <-chan *schema.Event, errs <-chan error) {
	for events != nil || errs != nil {
		select {
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			_ = evt
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil && logger != nil {
				logger.Printf("consumer: %v", err)
			}
		}
	}
}

func logErrors(logger *log.Logger, stage string, errs <-chan error) {
	for err := range errs {
		if err != nil {
			logger.Printf("%s: %v", stage, err)
		}
	}
}

func resolveConfigPath(flagValue string) string {
	if flagValue == "" {
		if _, err := os.Stat("config/streaming.yaml"); err == nil {
			return "config/streaming.yaml"
		}
		return "config/streaming.example.yaml"
	}
	return flagValue
}
