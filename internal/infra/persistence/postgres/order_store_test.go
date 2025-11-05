package postgres

import (
	"context"
	"testing"

	"github.com/coachpo/meltica/internal/domain/orderstore"
)

func TestOrderStoreNilPool(t *testing.T) {
	store := NewOrderStore(nil)
	ctx := context.Background()
	order := orderstore.Order{ID: "abc", Provider: "binance", StrategyInstance: "lambda-1", ClientOrderID: "client-1", Symbol: "BTC-USDT", Side: "BUY", Type: "Limit", Quantity: "1", State: "pending", PlacedAt: 0}
	if err := store.CreateOrder(ctx, order); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if err := store.UpdateOrder(ctx, orderstore.OrderUpdate{ID: "abc", State: "ack"}); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	exec := orderstore.Execution{OrderID: "abc", Provider: "binance", ExecutionID: "1", Quantity: "1", Price: "10", Liquidity: "Taker", TradedAt: 0}
	if err := store.RecordExecution(ctx, exec); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	bal := orderstore.BalanceSnapshot{Provider: "binance", Asset: "USDT", Total: "1", Available: "1", SnapshotAt: 0}
	if err := store.UpsertBalance(ctx, bal); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if err := store.WithTransaction(ctx, func(ctx context.Context, tx orderstore.Tx) error {
		return nil
	}); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if _, err := store.ListOrders(ctx, orderstore.OrderQuery{StrategyInstance: "lambda-1"}); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if _, err := store.ListExecutions(ctx, orderstore.ExecutionQuery{StrategyInstance: "lambda-1"}); err == nil {
		t.Fatalf("expected error when pool nil")
	}
	if _, err := store.ListBalances(ctx, orderstore.BalanceQuery{Provider: "binance"}); err == nil {
		t.Fatalf("expected error when pool nil")
	}
}
