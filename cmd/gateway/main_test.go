package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/config"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/schema"
)

func TestCanonicalToEventTypeMappings(t *testing.T) {
	cases := map[schema.CanonicalType]schema.EventType{
		schema.CanonicalType("ORDERBOOK.SNAPSHOT"): schema.EventTypeBookSnapshot,
		schema.CanonicalType("ORDERBOOK.DELTA"):    schema.EventTypeBookUpdate,
		schema.CanonicalType("ORDERBOOK.UPDATE"):   schema.EventTypeBookUpdate,
		schema.CanonicalType("TRADE"):              schema.EventTypeTrade,
		schema.CanonicalType("TICKER"):             schema.EventTypeTicker,
		schema.CanonicalType("EXECUTION.REPORT"):   schema.EventTypeExecReport,
		schema.CanonicalType("KLINE.SUMMARY"):      schema.EventTypeKlineSummary,
	}

	for input, expected := range cases {
		actual, ok := canonicalToEventType(input)
		require.True(t, ok, input)
		require.Equal(t, expected, actual)
	}

	_, ok := canonicalToEventType(schema.CanonicalType("UNKNOWN"))
	require.False(t, ok)
}

func TestRouteFromConfigCopiesFields(t *testing.T) {
	cfg := config.RouteConfig{
		WSTopics: []string{"trade"},
		RestFns:  []config.RestFnConfig{{Name: "snapshot", Endpoint: "/rest", Interval: time.Second, Parser: "json"}},
		Filters:  []config.FilterRuleConfig{{Field: "symbol", Op: "eq", Value: "BTC-USDT"}},
	}
	route := routeFromConfig("TRADE", cfg)
	require.Equal(t, schema.CanonicalType("TRADE"), route.Type)
	require.Len(t, route.RestFns, 1)
	require.Len(t, route.Filters, 1)
}

func TestCollectEventTypesDeduplicates(t *testing.T) {
	routes := map[schema.CanonicalType]dispatcher.Route{
		schema.CanonicalType("TRADE"):            {Type: schema.CanonicalType("TRADE")},
		schema.CanonicalType("ORDERBOOK.UPDATE"): {Type: schema.CanonicalType("ORDERBOOK.UPDATE")},
		schema.CanonicalType("ORDERBOOK.DELTA"):  {Type: schema.CanonicalType("ORDERBOOK.DELTA")},
		schema.CanonicalType("UNKNOWN"):          {Type: schema.CanonicalType("UNKNOWN")},
	}
	events := collectEventTypes(routes)
	require.ElementsMatch(t, []schema.EventType{schema.EventTypeTrade, schema.EventTypeBookUpdate}, events)
}

func TestResolveConfigPathDefaults(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { require.NoError(t, os.Chdir(wd)) })

	path := resolveConfigPath("")
	require.Equal(t, "config/streaming.example.yaml", path)

	require.NoError(t, os.MkdirAll("config", 0o750))
	require.NoError(t, os.WriteFile(filepath.Join("config", "streaming.yaml"), []byte("{}"), 0o600))

	path = resolveConfigPath("")
	require.Equal(t, "config/streaming.yaml", path)
}

func TestDrainConsumerAndLogErrors(t *testing.T) {
	events := make(chan *schema.Event, 1)
	errs := make(chan error, 2)
	loggerBuf := new(bytes.Buffer)
	logger := log.New(loggerBuf, "", 0)

	events <- &schema.Event{EventID: "evt"}
	close(events)

	errs <- nil
	errs <- context.Canceled
	close(errs)

	drainConsumer(logger, events, errs)
	require.Contains(t, loggerBuf.String(), "consumer")

	buf := new(bytes.Buffer)
	logErrs := log.New(buf, "", 0)
	errCh := make(chan error, 1)
	errCh <- context.DeadlineExceeded
	close(errCh)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		logErrors(logErrs, "stage", errCh)
	}()
	wg.Wait()
	require.Contains(t, buf.String(), "stage")
}
