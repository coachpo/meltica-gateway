package fake

import (
	"context"
	"testing"
	"time"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

func TestParseTIF(t *testing.T) {
	cases := map[string]tifMode{
		"":          tifGTC,
		"ioc":       tifIOC,
		"FOK":       tifFOK,
		"post":      tifPostOnly,
		"POST_ONLY": tifPostOnly,
	}
	for input, expected := range cases {
		if got := parseTIF(input); got != expected {
			t.Fatalf("parseTIF(%q)=%v want %v", input, got, expected)
		}
	}
}

func TestInstrumentStateConsumeLiquidityFillsRestingOrders(t *testing.T) {
	cons := instrumentConstraints{
		priceIncrement:    0.1,
		quantityIncrement: 0.1,
		minQuantity:       0.1,
		pricePrecision:    2,
		quantityPrecision: 4,
	}
	state := newSymbolMarketState("TEST-USDT", 100, cons, 5)
	state.asks = make(map[priceTick]*bookDepth)
	state.bids = make(map[priceTick]*bookDepth)
	now := time.Now()
	order := &activeOrder{
		clientID:   "c1",
		exchangeID: "ex1",
		instrument: "TEST-USDT",
		side:       schema.TradeSideSell,
		orderType:  schema.OrderTypeLimit,
		tif:        tifGTC,
		price:      101,
		priceTick:  cons.tickForPrice(101),
		quantity:   1,
		remaining:  1,
	}
	state.restOrder(order)
	state.orderIndex[order.exchangeID] = order
	avgPrice, fills, filled := state.consumeLiquidity(schema.TradeSideBuy, 0.6, 0, now)
	if filled <= 0 {
		t.Fatalf("expected fill, got %f", filled)
	}
	if avgPrice <= 0 {
		t.Fatalf("expected avg price > 0")
	}
	if len(fills) != 1 {
		t.Fatalf("expected 1 fill, got %d", len(fills))
	}
	if order.remaining >= 1 {
		t.Fatalf("order should be partially filled")
	}
}

func TestKlineFinalizeProducesBucket(t *testing.T) {
	cons := instrumentConstraints{priceIncrement: 0.1, quantityIncrement: 0.1, pricePrecision: 2, quantityPrecision: 2}
	state := newSymbolMarketState("TEST-USDT", 50, cons, 3)
	interval := 2 * time.Second
	start := time.Unix(0, 0)
	state.updateKline(start, 50, 1, interval)
	state.updateKline(start.Add(time.Second), 52, 1, interval)
	completed := state.finalizeKlines(start.Add(3*time.Second), interval)
	if len(completed) == 0 {
		t.Fatal("expected completed bucket")
	}
	bucket := completed[0]
	if bucket.open != 50 || bucket.close != 52 {
		t.Fatalf("unexpected bucket values: %+v", bucket)
	}
}

func TestProviderPublishTradeEvent(t *testing.T) {
	pm := pool.NewPoolManager()
	err := pm.RegisterPool("Event", 32, 0, func() any { return &schema.Event{} })
	if err != nil {
		t.Fatalf("register event pool: %v", err)
	}
	t.Cleanup(func() {
		_ = pm.Shutdown(context.Background())
	})

	prov := NewProvider(Options{Pools: pm})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if startErr := prov.Start(ctx); startErr != nil {
		t.Fatalf("start provider: %v", startErr)
	}

	symbol := "BTC-USDT"
	evt := publishAndWait(t, prov, pm, func() error { return prov.PublishTradeEvent(symbol) }, schema.EventTypeTrade)
	defer pm.ReturnEventInst(evt)

	if evt.Symbol != symbol {
		t.Fatalf("expected symbol %s, got %s", symbol, evt.Symbol)
	}
	payload, ok := evt.Payload.(schema.TradePayload)
	if !ok {
		t.Fatalf("unexpected payload type %T", evt.Payload)
	}
	if payload.TradeID == "" {
		t.Fatal("expected non-empty trade id")
	}
}

func TestProviderPublishTickerEvent(t *testing.T) {
	pm := pool.NewPoolManager()
	err := pm.RegisterPool("Event", 32, 0, func() any { return &schema.Event{} })
	if err != nil {
		t.Fatalf("register event pool: %v", err)
	}
	t.Cleanup(func() {
		_ = pm.Shutdown(context.Background())
	})

	prov := NewProvider(Options{Pools: pm})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if startErr := prov.Start(ctx); startErr != nil {
		t.Fatalf("start provider: %v", startErr)
	}

	symbol := "ETH-USDT"
	evt := publishAndWait(t, prov, pm, func() error { return prov.PublishTickerEvent(symbol) }, schema.EventTypeTicker)
	defer pm.ReturnEventInst(evt)

	if evt.Symbol != symbol {
		t.Fatalf("expected symbol %s, got %s", symbol, evt.Symbol)
	}
	if _, ok := evt.Payload.(schema.TickerPayload); !ok {
		t.Fatalf("unexpected payload type %T", evt.Payload)
	}
}

func TestProviderPublishExecReport(t *testing.T) {
	pm := pool.NewPoolManager()
	err := pm.RegisterPool("Event", 32, 0, func() any { return &schema.Event{} })
	if err != nil {
		t.Fatalf("register event pool: %v", err)
	}
	t.Cleanup(func() {
		_ = pm.Shutdown(context.Background())
	})

	prov := NewProvider(Options{Pools: pm})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if startErr := prov.Start(ctx); startErr != nil {
		t.Fatalf("start provider: %v", startErr)
	}

	symbol := "SOL-USDT"
	payload := schema.ExecReportPayload{
		ClientOrderID:  "client-1",
		State:          schema.ExecReportStateACK,
		Side:           schema.TradeSideBuy,
		OrderType:      schema.OrderTypeLimit,
		Price:          "100.0",
		Quantity:       "1.0",
		FilledQuantity: "0",
		RemainingQty:   "1.0",
		AvgFillPrice:   "0",
	}

	evt := publishAndWait(t, prov, pm, func() error { return prov.PublishExecReport(symbol, payload) }, schema.EventTypeExecReport)
	defer pm.ReturnEventInst(evt)

	if evt.Symbol != symbol {
		t.Fatalf("expected symbol %s, got %s", symbol, evt.Symbol)
	}
	report, ok := evt.Payload.(schema.ExecReportPayload)
	if !ok {
		t.Fatalf("unexpected payload type %T", evt.Payload)
	}
	if report.ExchangeOrderID == "" {
		t.Fatal("expected generated exchange order id")
	}
	if report.Timestamp.IsZero() {
		t.Fatal("expected populated timestamp")
	}
}

func publishAndWait(t *testing.T, prov *Provider, pm *pool.PoolManager, publish func() error, expected schema.EventType) *schema.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := publish(); err != nil {
			t.Fatalf("publish error: %v", err)
		}
		end := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(end) {
			select {
			case evt, ok := <-prov.Events():
				if !ok {
					t.Fatal("events channel closed unexpectedly")
				}
				if evt == nil {
					continue
				}
				if evt.Type == expected {
					return evt
				}
				pm.ReturnEventInst(evt)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	t.Fatalf("timeout waiting for %s event", expected)
	return nil
}
