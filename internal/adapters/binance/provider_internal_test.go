package binance

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

func newTestProvider(t *testing.T) *Provider {
	pm := pool.NewPoolManager()
	t.Cleanup(func() {
		_ = pm.Shutdown(context.Background())
	})
	if err := pm.RegisterPool("Event", 16, 0, func() any { return &schema.Event{} }); err != nil {
		t.Fatalf("register pool: %v", err)
	}
	prov := NewProvider(Options{Pools: pm, APIKey: "key", APISecret: "secret"})
	prov.ctx = context.Background()
	return prov
}

func TestHandleAccountPositionPublishesBalances(t *testing.T) {
	prov := newTestProvider(t)
	now := time.Now().UTC()
	event := accountPositionEvent{
		EventTime: binanceTimestamp(now.UnixMilli()),
		Balances: []accountPositionBalance{{
			Asset:  "USDT",
			Free:   "10.5",
			Locked: "1.5",
		}},
	}
	prov.handleAccountPosition(event)
	select {
	case evt := <-prov.events:
		if evt.Type != schema.EventTypeBalanceUpdate {
			t.Fatalf("unexpected event type %s", evt.Type)
		}
		payload, ok := evt.Payload.(schema.BalanceUpdatePayload)
		if !ok {
			t.Fatalf("expected BalanceUpdatePayload, got %T", evt.Payload)
		}
		if payload.Currency != "USDT" {
			t.Fatalf("expected currency USDT, got %s", payload.Currency)
		}
		if avail := decimal.RequireFromString(payload.Available); !avail.Equal(decimal.RequireFromString("10.5")) {
			t.Fatalf("expected available 10.5, got %s", payload.Available)
		}
		if total := decimal.RequireFromString(payload.Total); !total.Equal(decimal.RequireFromString("12")) {
			t.Fatalf("expected total 12, got %s", payload.Total)
		}
		prov.pools.ReturnEventInst(evt)
	case <-time.After(2 * time.Second):
		t.Fatal("expected balance update event")
	}
	prov.balanceMu.Lock()
	snapshot, ok := prov.balances["USDT"]
	prov.balanceMu.Unlock()
	if !ok {
		t.Fatal("expected balance snapshot for USDT")
	}
	if !snapshot.free.Equal(decimal.RequireFromString("10.5")) {
		t.Fatalf("expected free 10.5, got %s", snapshot.free.String())
	}
	if !snapshot.locked.Equal(decimal.RequireFromString("1.5")) {
		t.Fatalf("expected locked 1.5, got %s", snapshot.locked.String())
	}
}

func TestHandleBalanceDeltaAdjustsBalance(t *testing.T) {
	prov := newTestProvider(t)
	now := time.Now().UTC()
	initial := accountPositionEvent{
		EventTime: binanceTimestamp(now.UnixMilli()),
		Balances: []accountPositionBalance{{
			Asset:  "BTC",
			Free:   "1",
			Locked: "0.2",
		}},
	}
	prov.handleAccountPosition(initial)
	// drain initial event
	select {
	case evt := <-prov.events:
		prov.pools.ReturnEventInst(evt)
	case <-time.After(2 * time.Second):
		t.Fatal("expected initial balance event")
	}
	delta := balanceDeltaEvent{EventTime: binanceTimestamp(now.Add(time.Second).UnixMilli()), Asset: "BTC", Delta: "0.5"}
	prov.handleBalanceDelta(delta)
	select {
	case evt := <-prov.events:
		payload, ok := evt.Payload.(schema.BalanceUpdatePayload)
		if !ok {
			t.Fatalf("expected BalanceUpdatePayload, got %T", evt.Payload)
		}
		if payload.Currency != "BTC" {
			t.Fatalf("expected BTC currency, got %s", payload.Currency)
		}
		if avail := decimal.RequireFromString(payload.Available); !avail.Equal(decimal.RequireFromString("1.5")) {
			t.Fatalf("expected available 1.5, got %s", payload.Available)
		}
		if total := decimal.RequireFromString(payload.Total); !total.Equal(decimal.RequireFromString("1.7")) {
			t.Fatalf("expected total 1.7, got %s", payload.Total)
		}
		prov.pools.ReturnEventInst(evt)
	case <-time.After(2 * time.Second):
		t.Fatal("expected balance delta event")
	}
}

func TestHandleExecutionReportPublishesExec(t *testing.T) {
	prov := newTestProvider(t)
	prov.symbols["BTC-USDT"] = symbolMeta{canonical: "BTC-USDT", rest: "BTCUSDT", stream: "btcusdt"}
	prov.restToCanon["BTCUSDT"] = "BTC-USDT"
	now := time.Now().UTC()
	commissionAsset := "BNB"
	event := executionReportEvent{
		EventTime:          binanceTimestamp(now.UnixMilli()),
		TransactionTime:    binanceTimestamp(now.UnixMilli()),
		Symbol:             "BTCUSDT",
		ClientOrderID:      "order-1",
		Side:               "BUY",
		OrderType:          "LIMIT",
		OrderStatus:        "PARTIALLY_FILLED",
		OrderID:            12345,
		OriginalQuantity:   "1.50000000",
		CumulativeQuantity: "0.50000000",
		Price:              "100.00",
		LastExecutedPrice:  "100.00",
		Commission:         "0.0005",
		CommissionAsset:    &commissionAsset,
		CumulativeQuoteQty: "50.00000000",
	}
	prov.handleExecutionReport(event)
	select {
	case evt := <-prov.events:
		if evt.Type != schema.EventTypeExecReport {
			t.Fatalf("unexpected event type %s", evt.Type)
		}
		payload, ok := evt.Payload.(schema.ExecReportPayload)
		if !ok {
			t.Fatalf("expected ExecReportPayload, got %T", evt.Payload)
		}
		if payload.Side != schema.TradeSideBuy {
			t.Fatalf("expected buy side, got %v", payload.Side)
		}
		if payload.OrderType != schema.OrderTypeLimit {
			t.Fatalf("expected limit order type, got %v", payload.OrderType)
		}
		if rem := decimal.RequireFromString(payload.RemainingQty); !rem.Equal(decimal.RequireFromString("1")) {
			t.Fatalf("expected remaining 1, got %s", payload.RemainingQty)
		}
		if avg := decimal.RequireFromString(payload.AvgFillPrice); !avg.Equal(decimal.RequireFromString("100")) {
			t.Fatalf("expected avg fill price 100, got %s", payload.AvgFillPrice)
		}
		if payload.CommissionAsset != "BNB" {
			t.Fatalf("expected commission asset BNB, got %s", payload.CommissionAsset)
		}
		prov.pools.ReturnEventInst(evt)
	case <-time.After(2 * time.Second):
		t.Fatal("expected execution report event")
	}
}
