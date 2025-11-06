package persistence_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	json "github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/coachpo/meltica/internal/domain/orderstore"
	"github.com/coachpo/meltica/internal/domain/outboxstore"
	"github.com/coachpo/meltica/internal/domain/providerstore"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/domain/strategystore"
	pgstore "github.com/coachpo/meltica/internal/infra/persistence/postgres"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	testPool    *pgxpool.Pool
	pgContainer testcontainers.Container
	setupErr    error
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		Env:          map[string]string{"POSTGRES_PASSWORD": "secret", "POSTGRES_USER": "postgres", "POSTGRES_DB": "meltica"},
		ExposedPorts: []string{"5432/tcp"},
		WaitingFor:   wait.ForListeningPort("5432/tcp").WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start postgres container: %v\n", err)
		os.Exit(1)
	}
	pgContainer = container

	setupErr = initialiseDatabase(ctx)
	exitCode := 0
	if setupErr != nil {
		fmt.Fprintf(os.Stderr, "postgres contract tests skipped: %v\n", setupErr)
	} else {
		exitCode = m.Run()
	}

	if testPool != nil {
		testPool.Close()
	}
	if pgContainer != nil {
		_ = pgContainer.Terminate(ctx)
	}
	os.Exit(exitCode)
}

func initialiseDatabase(ctx context.Context) error {
	host, err := pgContainer.Host(ctx)
	if err != nil {
		return fmt.Errorf("container host: %w", err)
	}
	port, err := pgContainer.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return fmt.Errorf("container port: %w", err)
	}
	dsn := fmt.Sprintf("postgres://postgres:secret@%s:%s/meltica?sslmode=disable", host, port.Port())

	if err := applyMigrations(dsn); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("pgx pool: %w", err)
	}
	testPool = pool
	return nil
}

func applyMigrations(dsn string) error {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("runtime caller lookup failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	migrationsDir := filepath.Join(root, "db", "migrations")
	sourceURL := fmt.Sprintf("file://%s", migrationsDir)

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open sql connection: %w", err)
	}
	defer sqlDB.Close()

	driver, err := pgxmigrate.WithInstance(sqlDB, &pgxmigrate.Config{})
	if err != nil {
		return fmt.Errorf("postgres driver: %w", err)
	}
	m, err := migrate.NewWithDatabaseInstance(sourceURL, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate instance: %w", err)
	}
	defer m.Close()
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func TestPostgresPersistenceStores(t *testing.T) {
	if setupErr != nil {
		t.Skipf("postgres contract setup unavailable: %v", setupErr)
	}
	ctx := context.Background()

	providerStore := pgstore.NewProviderStore(testPool)
	strategyStore := pgstore.NewStrategyStore(testPool)
	orderStore := pgstore.NewOrderStore(testPool)
	outboxStore := pgstore.NewOutboxStore(testPool)

	providerSnapshot := providerstore.Snapshot{
		Name:        "binance",
		DisplayName: "Binance Futures",
		Adapter:     "binance.futures",
		Config: map[string]any{
			"apiKey":    "abc",
			"apiSecret": "xyz",
		},
		Status: "active",
		Metadata: map[string]any{
			"region": "sg",
		},
	}
	if err := providerStore.SaveProvider(ctx, providerSnapshot); err != nil {
		t.Fatalf("save provider: %v", err)
	}

	routes := []providerstore.RouteSnapshot{
		{
			Type:     schema.RouteTypeTrade,
			WSTopics: []string{"trades"},
			RestFns: []providerstore.RouteRestFn{
				{
					Name:     "ticker",
					Endpoint: "/api/v3/ticker",
					Interval: time.Second * 30,
					Parser:   "tickerParser",
				},
			},
			Filters: []providerstore.RouteFilter{
				{
					Field: "symbol",
					Op:    "eq",
					Value: "BTCUSDT",
				},
			},
		},
	}
	if err := providerStore.SaveRoutes(ctx, providerSnapshot.Name, routes); err != nil {
		t.Fatalf("save routes: %v", err)
	}

	strategySnapshot := strategystore.Snapshot{
		ID: "strat-" + uuid.NewString(),
		Strategy: strategystore.Strategy{
			Identifier: "momentum",
			Selector:   "momentum-v1",
			Tag:        "beta",
			Hash:       "hash-123",
			Version:    "1.0.0",
			Config: map[string]any{
				"window": "24h",
			},
		},
		Providers:       []string{providerSnapshot.Name},
		ProviderSymbols: map[string][]string{providerSnapshot.Name: {"BTCUSDT"}},
		Running:         true,
		Dynamic:         false,
		Baseline:        false,
		Metadata: map[string]any{
			"owner": "qa",
		},
	}
	if err := strategyStore.Save(ctx, strategySnapshot); err != nil {
		t.Fatalf("save strategy: %v", err)
	}

	orderID := uuid.NewString()
	clientOrderID := "cli-" + uuid.NewString()
	price := "27500.50"
	fee := "3.21"
	filledAt := time.Now().Add(2 * time.Minute).Unix()
	ackAt := time.Now().Add(30 * time.Second).Unix()
	doneAt := time.Now().Add(4 * time.Minute).Unix()

	err := orderStore.WithTransaction(ctx, func(ctx context.Context, tx orderstore.Tx) error {
		if err := tx.CreateOrder(ctx, orderstore.Order{
			ID:                orderID,
			Provider:          providerSnapshot.Name,
			StrategyInstance:  strategySnapshot.ID,
			ClientOrderID:     clientOrderID,
			Symbol:            "BTCUSDT",
			Side:              "BUY",
			Type:              "LIMIT",
			Quantity:          "1.25",
			Price:             &price,
			State:             "NEW",
			ExternalReference: "ext-1234",
			PlacedAt:          time.Now().Unix(),
			Metadata: map[string]any{
				"source": "integration-test",
			},
		}); err != nil {
			return fmt.Errorf("create order: %w", err)
		}
		if err := tx.UpdateOrder(ctx, orderstore.OrderUpdate{
			ID:             orderID,
			State:          "FILLED",
			AcknowledgedAt: &ackAt,
			CompletedAt:    &doneAt,
			Metadata: map[string]any{
				"status": "completed",
			},
		}); err != nil {
			return fmt.Errorf("update order: %w", err)
		}
		if err := tx.RecordExecution(ctx, orderstore.Execution{
			OrderID:     orderID,
			Provider:    providerSnapshot.Name,
			ExecutionID: "exec-" + uuid.NewString(),
			Quantity:    "1.25",
			Price:       "27499.99",
			Fee:         &fee,
			FeeAsset:    ptr("USDT"),
			Liquidity:   "MAKER",
			TradedAt:    filledAt,
			Metadata: map[string]any{
				"leg": "primary",
			},
		}); err != nil {
			return fmt.Errorf("record execution: %w", err)
		}
		if err := tx.UpsertBalance(ctx, orderstore.BalanceSnapshot{
			Provider:   providerSnapshot.Name,
			Asset:      "USDT",
			Total:      "1000.00",
			Available:  "750.50",
			SnapshotAt: time.Now().Unix(),
			Metadata: map[string]any{
				"account": "primary",
			},
		}); err != nil {
			return fmt.Errorf("upsert balance: %w", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("order transaction: %v", err)
	}

	orders, err := orderStore.ListOrders(ctx, orderstore.OrderQuery{
		StrategyInstance: strategySnapshot.ID,
		Provider:         providerSnapshot.Name,
	})
	if err != nil {
		t.Fatalf("list orders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orders))
	}
	gotOrder := orders[0]
	if gotOrder.Order.ID != orderID {
		t.Fatalf("unexpected order id %s", gotOrder.Order.ID)
	}
	if gotOrder.Order.Price == nil || !numericEqual(*gotOrder.Order.Price, price) {
		t.Fatalf("expected price %s, got %v", price, gotOrder.Order.Price)
	}
	if gotOrder.CompletedAt == nil || *gotOrder.CompletedAt != doneAt {
		t.Fatalf("expected completedAt %d, got %v", doneAt, gotOrder.CompletedAt)
	}

	executions, err := orderStore.ListExecutions(ctx, orderstore.ExecutionQuery{
		StrategyInstance: strategySnapshot.ID,
		Provider:         providerSnapshot.Name,
		OrderID:          orderID,
	})
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(executions))
	}
	if executions[0].Execution.Fee == nil || !numericEqual(*executions[0].Execution.Fee, fee) {
		t.Fatalf("expected fee %s, got %v", fee, executions[0].Execution.Fee)
	}

	balances, err := orderStore.ListBalances(ctx, orderstore.BalanceQuery{
		Provider: providerSnapshot.Name,
		Asset:    "USDT",
	})
	if err != nil {
		t.Fatalf("list balances: %v", err)
	}
	if len(balances) != 1 {
		t.Fatalf("expected 1 balance, got %d", len(balances))
	}
	if balances[0].BalanceSnapshot.Asset != "USDT" {
		t.Fatalf("unexpected balance asset %s", balances[0].BalanceSnapshot.Asset)
	}

	loadedProviders, err := providerStore.LoadProviders(ctx)
	if err != nil {
		t.Fatalf("load providers: %v", err)
	}
	if len(loadedProviders) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(loadedProviders))
	}
	if loadedProviders[0].Name != providerSnapshot.Name {
		t.Fatalf("unexpected provider name %s", loadedProviders[0].Name)
	}

	loadedRoutes, err := providerStore.LoadRoutes(ctx, providerSnapshot.Name)
	if err != nil {
		t.Fatalf("load routes: %v", err)
	}
	if len(loadedRoutes) != len(routes) {
		t.Fatalf("expected %d routes, got %d", len(routes), len(loadedRoutes))
	}
	if loadedRoutes[0].Type != schema.NormalizeRouteType(routes[0].Type) {
		t.Fatalf("unexpected route type %s", loadedRoutes[0].Type)
	}

	payload, err := json.Marshal(map[string]any{
		"orderId": orderID,
		"state":   "FILLED",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	eventRecord, err := outboxStore.Enqueue(ctx, outboxstore.Event{
		AggregateType: "order",
		AggregateID:   orderID,
		EventType:     "OrderFilled",
		Payload:       payload,
		Headers: map[string]any{
			"source": "integration-test",
		},
		AvailableAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("enqueue event: %v", err)
	}
	if eventRecord.ID == 0 {
		t.Fatalf("expected event id to be set")
	}

	pending := waitForPending(t, ctx, outboxStore, 10, 1)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending event, got %d", len(pending))
	}

	if err := outboxStore.MarkDelivered(ctx, eventRecord.ID); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}

	pendingAfterDelivery, err := outboxStore.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending after delivery: %v", err)
	}
	if len(pendingAfterDelivery) != 0 {
		t.Fatalf("expected 0 pending events after delivery, got %d", len(pendingAfterDelivery))
	}

	if err := outboxStore.Delete(ctx, eventRecord.ID); err != nil {
		t.Fatalf("delete event: %v", err)
	}
}

func ptr(value string) *string {
	return &value
}

func numericEqual(a, b string) bool {
	da, err := decimal.NewFromString(strings.TrimSpace(a))
	if err != nil {
		return false
	}
	db, err := decimal.NewFromString(strings.TrimSpace(b))
	if err != nil {
		return false
	}
	return da.Equal(db)
}

func waitForPending(t *testing.T, ctx context.Context, store outboxstore.Store, limit int, expected int) []outboxstore.EventRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		rows, err := store.ListPending(ctx, limit)
		if err != nil {
			t.Fatalf("list pending: %v", err)
		}
		if len(rows) >= expected {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d pending events, got %d", expected, len(rows))
		}
		time.Sleep(50 * time.Millisecond)
	}
}
