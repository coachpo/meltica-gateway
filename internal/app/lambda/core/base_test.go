package core

import (
	"context"
	"testing"
	"time"

	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/bus/eventbus"
	"github.com/coachpo/meltica/internal/infra/pool"
)

func TestSelectProviderDeterministic(t *testing.T) {
	cfg := Config{
		Providers: []string{"alpha", "beta", "gamma"},
		ProviderSymbols: map[string][]string{
			"alpha": []string{"BTC-USDT"},
			"beta":  []string{"BTC-USDT"},
			"gamma": []string{"BTC-USDT"},
		},
	}
	base := NewBaseLambda("lambda-select", cfg, nil, nil, nil, nil, nil, nil)

	testCases := []struct {
		seed uint64
		want string
	}{
		{seed: 0, want: "alpha"},
		{seed: 1, want: "beta"},
		{seed: 2, want: "gamma"},
		{seed: 3, want: "alpha"},
		{seed: 4, want: "beta"},
	}

	for _, tc := range testCases {
		got, err := base.SelectProvider(tc.seed)
		if err != nil {
			t.Fatalf("SelectProvider(%d) unexpected error: %v", tc.seed, err)
		}
		if got != tc.want {
			t.Fatalf("SelectProvider(%d) = %s, want %s", tc.seed, got, tc.want)
		}
	}
}

func TestBaseLambdaHandlesExtensionEvents(t *testing.T) {
	poolMgr := pool.NewPoolManager()
	if err := poolMgr.RegisterPool("Event", 8, 0, func() any { return new(schema.Event) }); err != nil {
		t.Fatalf("register pool: %v", err)
	}
	bus := eventbus.NewMemoryBus(eventbus.MemoryConfig{
		BufferSize:    8,
		FanoutWorkers: 1,
		Pools:         poolMgr,
	})
	strategy := &testExtensionStrategy{}
	cfg := Config{
		Providers:       []string{"okx"},
		ProviderSymbols: map[string][]string{"okx": []string{"BTC-USDT"}},
	}
	lambda := NewBaseLambda("lambda-ext", cfg, bus, nil, poolMgr, strategy, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh, err := lambda.Start(ctx)
	if err != nil {
		t.Fatalf("start lambda: %v", err)
	}
	evt, err := poolMgr.BorrowEventInst(ctx)
	if err != nil {
		t.Fatalf("borrow event: %v", err)
	}
	evt.EventID = "ext-evt"
	evt.Provider = "okx"
	evt.Symbol = "BTC-USDT"
	evt.Type = schema.ExtensionEventType
	evt.Payload = map[string]any{"foo": "bar"}

	if err := bus.Publish(ctx, evt); err != nil {
		t.Fatalf("publish extension event: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	cancel()
	for range errCh {
	}
	if len(strategy.received) != 1 {
		t.Fatalf("expected strategy to receive extension payload, got %d", len(strategy.received))
	}
	if payload, ok := strategy.received[0].(map[string]any); !ok || payload["foo"] != "bar" {
		t.Fatalf("unexpected payload: %v", strategy.received[0])
	}
}

type testExtensionStrategy struct {
	received []any
}

func (s *testExtensionStrategy) OnTrade(context.Context, *schema.Event, schema.TradePayload, float64) {}
func (s *testExtensionStrategy) OnTicker(context.Context, *schema.Event, schema.TickerPayload) {}
func (s *testExtensionStrategy) OnBookSnapshot(context.Context, *schema.Event, schema.BookSnapshotPayload) {}
func (s *testExtensionStrategy) OnKlineSummary(context.Context, *schema.Event, schema.KlineSummaryPayload) {}
func (s *testExtensionStrategy) OnInstrumentUpdate(context.Context, *schema.Event, schema.InstrumentUpdatePayload) {}
func (s *testExtensionStrategy) OnBalanceUpdate(context.Context, *schema.Event, schema.BalanceUpdatePayload) {}
func (s *testExtensionStrategy) OnOrderFilled(context.Context, *schema.Event, schema.ExecReportPayload) {}
func (s *testExtensionStrategy) OnOrderRejected(context.Context, *schema.Event, schema.ExecReportPayload, string) {}
func (s *testExtensionStrategy) OnOrderPartialFill(context.Context, *schema.Event, schema.ExecReportPayload) {}
func (s *testExtensionStrategy) OnOrderCancelled(context.Context, *schema.Event, schema.ExecReportPayload) {}
func (s *testExtensionStrategy) OnOrderAcknowledged(context.Context, *schema.Event, schema.ExecReportPayload) {}
func (s *testExtensionStrategy) OnOrderExpired(context.Context, *schema.Event, schema.ExecReportPayload) {}
func (s *testExtensionStrategy) OnRiskControl(context.Context, *schema.Event, schema.RiskControlPayload) {}
func (s *testExtensionStrategy) OnExtensionEvent(_ context.Context, _ *schema.Event, payload any) {
	s.received = append(s.received, payload)
}
func (s *testExtensionStrategy) SubscribedEvents() []schema.EventType {
	return []schema.EventType{schema.ExtensionEventType}
}
func (s *testExtensionStrategy) WantsCrossProviderEvents() bool { return false }
