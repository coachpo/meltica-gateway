// Command gateway launches the Meltica runtime entrypoint.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/coachpo/meltica/internal/adapters/fake"
	"github.com/coachpo/meltica/internal/adapters/shared"
	"github.com/coachpo/meltica/internal/bus/controlbus"
	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/config"
	"github.com/coachpo/meltica/internal/consumer"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
	"github.com/sourcegraph/conc"
	"github.com/sourcegraph/conc/iter"
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

	poolMgr := pool.NewPoolManager()
	registerPool := func(name string, capacity int, factory func() interface{}) {
		if err := poolMgr.RegisterPool(name, capacity, factory); err != nil {
			log.Fatalf("register pool %s: %v", name, err)
		}
	}
	registerPool("WsFrame", 200, func() interface{} { return new(schema.WsFrame) })
	registerPool("ProviderRaw", 200, func() interface{} { return new(schema.ProviderRaw) })
	registerPool("CanonicalEvent", 1000, func() interface{} { return new(schema.CanonicalEvent) })
	registerPool("OrderRequest", 20, func() interface{} { return new(schema.OrderRequest) })
	registerPool("ExecReport", 20, func() interface{} { return new(schema.ExecReport) })
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := poolMgr.Shutdown(shutdownCtx); err != nil {
			logger.Printf("pool shutdown: %v", err)
		}
	}()

	var lifecycle conc.WaitGroup
	defer lifecycle.Wait()

	bus := databus.NewMemoryBus(databus.MemoryConfig{
		BufferSize:    streamingCfg.Databus.BufferSize,
		FanoutWorkers: 8,
		Pools:         poolMgr,
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

	provider := fake.NewProvider(fake.Options{
		Name:               "fake",
		Instruments:        collectInstruments(table.Routes()),
		TickerInterval:     1000 * time.Microsecond,
		TradeInterval:      1000 * time.Microsecond,
		BookUpdateInterval: 1000 * time.Microsecond,
		Pools:              poolMgr,
	})
	if err := provider.Start(ctx); err != nil {
		logger.Fatalf("start provider: %v", err)
	}

	runtimeCfg := config.DispatcherRuntimeConfig{
		StreamOrdering: config.StreamOrderingConfig{
			LatenessTolerance: 150 * time.Millisecond,
			FlushInterval:     50 * time.Millisecond,
			MaxBufferSize:     1024,
		},
	}

	dispatcherRuntime := dispatcher.NewRuntime(bus, table, poolMgr, runtimeCfg, nil)
	dispatchErrs := dispatcherRuntime.Start(ctx, provider.Events())

	lifecycle.Go(func() {
		logErrors(logger, "provider", provider.Errors())
	})
	lifecycle.Go(func() {
		logErrors(logger, "dispatcher", dispatchErrs)
	})

	subscriptionManager := shared.NewSubscriptionManager(provider)
	tradingState := dispatcher.NewTradingState()
	for _, route := range table.Routes() {
		if err := subscriptionManager.Activate(ctx, route); err != nil {
			logger.Printf("subscribe route %s: %v", route.Type, err)
		}
	}

	// Create three specialized lambda consumers
	tradeLambda := consumer.NewTradeLambda("trade-consumer", bus, poolMgr, logger)
	tickerLambda := consumer.NewTickerLambda("ticker-consumer", bus, poolMgr, logger)
	orderbookLambda := consumer.NewOrderBookLambda("orderbook-consumer", bus, poolMgr, logger)

	// Start all three lambdas
	if tradeErrs, err := tradeLambda.Start(ctx); err != nil {
		logger.Printf("trade lambda: %v", err)
	} else {
		lifecycle.Go(func() {
			for err := range tradeErrs {
				if err != nil {
					logger.Printf("trade lambda: %v", err)
				}
			}
		})
	}

	if tickerErrs, err := tickerLambda.Start(ctx); err != nil {
		logger.Printf("ticker lambda: %v", err)
	} else {
		lifecycle.Go(func() {
			for err := range tickerErrs {
				if err != nil {
					logger.Printf("ticker lambda: %v", err)
				}
			}
		})
	}

	if orderbookErrs, err := orderbookLambda.Start(ctx); err != nil {
		logger.Printf("orderbook lambda: %v", err)
	} else {
		lifecycle.Go(func() {
			for err := range orderbookErrs {
				if err != nil {
					logger.Printf("orderbook lambda: %v", err)
				}
			}
		})
	}
	controller := dispatcher.NewController(
		table,
		controlBus,
		subscriptionManager,
		dispatcher.WithOrderSubmitter(provider),
		dispatcher.WithTradingState(tradingState),
		dispatcher.WithControlPublisher(bus, poolMgr),
	)
	lifecycle.Go(func() {
		if err := controller.Start(ctx); err != nil && err != context.Canceled {
			logger.Printf("controller: %v", err)
		}
	})

	controlAddr := ":8880"
	controlHandler := dispatcher.NewControlHTTPHandler(controlBus)
	controlServer := new(http.Server)
	controlServer.Addr = controlAddr
	controlServer.Handler = controlHandler
	controlServer.ReadHeaderTimeout = 5 * time.Second
	lifecycle.Go(func() {
		if err := controlServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Printf("control server: %v", err)
		}
	})
	logger.Printf("control API listening on %s", controlAddr)

	logger.Print("gateway started; awaiting shutdown signal")
	<-ctx.Done()
	logger.Print("shutdown requested")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := controlServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("control server shutdown: %v", err)
	}
	if err := shutdownCtx.Err(); err != nil {
		logger.Printf("shutdown deadline reached: %v", err)
	}
}

func collectInstruments(routes map[schema.CanonicalType]dispatcher.Route) []string {
	set := make(map[string]struct{})
	for _, route := range routes {
		for _, filter := range route.Filters {
			if strings.EqualFold(filter.Field, "instrument") {
				appendInstrument(filter.Value, set)
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for inst := range set {
		out = append(out, inst)
	}
	slices.Sort(out)
	return out
}

func appendInstrument(value any, set map[string]struct{}) {
	switch v := value.(type) {
	case string:
		instrument := strings.ToUpper(strings.TrimSpace(v))
		if instrument != "" {
			set[instrument] = struct{}{}
		}
	case []string:
		for _, entry := range v {
			appendInstrument(entry, set)
		}
	case []any:
		for _, entry := range v {
			appendInstrument(entry, set)
		}
	}
}


func routeFromConfig(name string, cfg config.RouteConfig) dispatcher.Route {
	filters := iter.Map(cfg.Filters, func(f *config.FilterRuleConfig) dispatcher.FilterRule {
		return dispatcher.FilterRule{Field: f.Field, Op: f.Op, Value: f.Value}
	})
	restFns := iter.Map(cfg.RestFns, func(rf *config.RestFnConfig) dispatcher.RestFn {
		return dispatcher.RestFn{Name: rf.Name, Endpoint: rf.Endpoint, Interval: rf.Interval, Parser: rf.Parser}
	})
	return dispatcher.Route{
		Type:     schema.CanonicalType(name),
		WSTopics: cfg.WSTopics,
		RestFns:  restFns,
		Filters:  filters,
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
		candidates := []string{
			"config/streaming.yaml",
			"internal/config/streaming.yaml",
			"config/streaming.example.yaml",
			"internal/config/streaming.example.yaml",
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
		return candidates[0]
	}
	return flagValue
}
