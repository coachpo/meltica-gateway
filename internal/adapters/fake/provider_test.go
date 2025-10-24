package fake

import (
	"testing"
	"time"

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
	state := newInstrumentState("TEST-USDT", 100, cons, 5)
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
	state := newInstrumentState("TEST-USDT", 50, cons, 3)
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
