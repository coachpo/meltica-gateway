package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/config"
	"github.com/coachpo/meltica/internal/adapters/binance"
	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/conductor"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/schema"

	json "github.com/goccy/go-json"
)

func TestCanonicalDeliveryFromBinanceSources(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	streamingCfg := config.StreamingConfig{
		Adapters: config.AdapterSet{},
		Dispatcher: config.DispatcherConfig{
			Routes: map[string]config.RouteConfig{
				"TICKER": {
					WSTopics: []string{"ticker.BTCUSDT"},
					Filters:  []config.FilterRuleConfig{{Field: "instrument", Op: "eq", Value: "BTC-USDT"}},
				},
				"ORDERBOOK.SNAPSHOT": {
					RestFns: []config.RestFnConfig{{
						Name:     "orderbookSnapshot",
						Endpoint: "/api/v3/depth",
						Interval: 10 * time.Millisecond,
						Parser:   "orderbook",
					}},
					Filters: []config.FilterRuleConfig{{Field: "instrument", Op: "eq", Value: "BTC-USDT"}},
				},
			},
		},
		Databus: databusConfig(),
	}

	parser := binance.NewParser()
	rawCh := make(chan schema.RawInstance, 4)
	tickerRoute := routeFromCfg("TICKER", streamingCfg.Dispatcher.Routes["TICKER"])
	orderRoute := routeFromCfg("ORDERBOOK.SNAPSHOT", streamingCfg.Dispatcher.Routes["ORDERBOOK.SNAPSHOT"])
	t.Logf("ticker route filters: %#v", tickerRoute.Filters)
	t.Logf("order route filters: %#v", orderRoute.Filters)
	if events, err := parser.Parse(context.Background(), mustJSON(binanceTickerFrame()), time.Now().UTC()); err == nil {
		for _, evt := range events {
			require.NotNil(t, evt)
			raw := canonicalEventToRaw(evt)
			require.True(t, tickerRoute.Match(raw))
			rawCh <- raw
		}
	} else {
		t.Fatalf("parse ws frame: %v", err)
	}
	if snap, err := parser.ParseSnapshot(context.Background(), "orderbook", mustJSON(binanceOrderbookSnapshot()), time.Now().UTC()); err == nil {
		for _, evt := range snap {
			require.NotNil(t, evt)
			raw := canonicalEventToRaw(evt)
			require.True(t, orderRoute.Match(raw))
			rawCh <- raw
		}
	} else {
		t.Fatalf("parse rest snapshot: %v", err)
	}
	close(rawCh)

	table := dispatcher.NewTable()
	require.NoError(t, table.Upsert(routeFromCfg("TICKER", streamingCfg.Dispatcher.Routes["TICKER"])))
	require.NoError(t, table.Upsert(routeFromCfg("ORDERBOOK.SNAPSHOT", streamingCfg.Dispatcher.Routes["ORDERBOOK.SNAPSHOT"])))

	ingestor := dispatcher.NewIngestor(table)
	canonicalCh, ingestErrs := ingestor.Run(ctx, rawCh)

	bus := databus.NewMemoryBus(databus.MemoryConfig{BufferSize: streamingCfg.Databus.BufferSize})
	_, eventCh, err := bus.Subscribe(ctx, schema.EventTypeTicker)
	require.NoError(t, err)
	_, bookCh, err := bus.Subscribe(ctx, schema.EventTypeBookSnapshot)
	require.NoError(t, err)

	forwarder := conductor.NewForwarder(bus)
	forwardErrs := forwarder.Run(ctx, canonicalCh)

	tickerEvt := waitForEvent(t, eventCh, ingestErrs, forwardErrs)
	bookEvt := waitForEvent(t, bookCh, ingestErrs, forwardErrs)

	require.Equal(t, "BTC-USDT", tickerEvt.Symbol)
	require.Equal(t, uint64(1), tickerEvt.SeqProvider)
	require.Equal(t, "BTC-USDT", bookEvt.Symbol)
	require.Equal(t, uint64(1), bookEvt.SeqProvider)
}

func databusConfig() config.DatabusConfig {
	return config.DatabusConfig{BufferSize: 8, PerInstrument: 4}
}

func routeFromCfg(name string, cfg config.RouteConfig) dispatcher.Route {
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

func waitForEvent(t *testing.T, ch <-chan *schema.Event, ingestErrs <-chan error, forwardErrs <-chan error) *schema.Event {
	t.Helper()
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed before event")
			}
			return evt
		case err, ok := <-ingestErrs:
			if ok && err != nil {
				t.Fatalf("ingestor error: %v", err)
			}
		case err, ok := <-forwardErrs:
			if ok && err != nil {
				t.Fatalf("forwarder error: %v", err)
			}
		case <-timeout:
			t.Fatalf("timeout waiting for event")
			return nil
		}
	}
}

func canonicalEventToRaw(evt *schema.Event) schema.RawInstance {
	if evt == nil {
		return nil
	}
	canonicalType := eventTypeToCanonical(evt.Type)
	if canonicalType == "" {
		return nil
	}
	return schema.RawInstance{
		"canonicalType": canonicalType,
		"instrument":    evt.Symbol,
		"market":        strings.ToUpper(evt.Provider),
		"source":        evt.Provider,
		"ts":            evt.EmitTS,
		"ingestedAt":    evt.IngestTS,
		"payload":       evt.Payload,
	}
}

func eventTypeToCanonical(t schema.EventType) schema.CanonicalType {
	switch t {
	case schema.EventTypeBookSnapshot:
		return schema.CanonicalType("ORDERBOOK.SNAPSHOT")
	case schema.EventTypeBookUpdate:
		return schema.CanonicalType("ORDERBOOK.DELTA")
	case schema.EventTypeTrade:
		return schema.CanonicalType("TRADE")
	case schema.EventTypeTicker:
		return schema.CanonicalType("TICKER")
	case schema.EventTypeExecReport:
		return schema.CanonicalType("EXECUTION.REPORT")
	case schema.EventTypeKlineSummary:
		return schema.CanonicalType("KLINE.SUMMARY")
	default:
		return ""
	}
}

func binanceTickerFrame() any {
	return map[string]any{
		"stream": "ticker.BTCUSDT",
		"data": map[string]any{
			"s": "BTCUSDT",
			"E": time.Now().Add(-10 * time.Millisecond).UnixMilli(),
			"e": "24hrTicker",
			"c": "68005.00",
			"b": "68000.10",
			"a": "68010.50",
			"v": "1200.5",
		},
	}
}

func binanceOrderbookSnapshot() any {
	return map[string]any{
		"lastUpdateId": 123456,
		"bids":         [][]any{{"68000.10", "0.5"}},
		"asks":         [][]any{{"68010.50", "0.4"}},
		"s":            "BTCUSDT",
	}
}

func mustJSON(payload any) []byte {
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return data
}
