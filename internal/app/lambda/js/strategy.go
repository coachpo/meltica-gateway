package js

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"

	"github.com/coachpo/meltica/internal/app/lambda/core"
	"github.com/coachpo/meltica/internal/app/lambda/strategies"
	"github.com/coachpo/meltica/internal/domain/schema"
)

// Strategy wraps a JavaScript strategy instance to satisfy core.TradingStrategy.
type Strategy struct {
	instance            *Instance
	handler             *goja.Object
	metadata            strategies.Metadata
	logger              *log.Logger
	runtime             *lambdaBridge
	crossProviderEvents atomic.Bool
}

type envConfig struct {
	Config   map[string]any      `json:"config"`
	Metadata strategies.Metadata `json:"metadata"`
	Events   []schema.EventType  `json:"events"`
	Helpers  map[string]any      `json:"helpers,omitempty"`
	Runtime  map[string]any      `json:"runtime,omitempty"`
}

// NewStrategy instantiates a JavaScript strategy from the supplied module.
func NewStrategy(module *Module, cfg map[string]any, logger *log.Logger) (*Strategy, error) {
	if module == nil {
		return nil, fmt.Errorf("js strategy: module required")
	}
	baseLogger := defaultStrategyLogger(logger)

	instance, err := NewInstance(module)
	if err != nil {
		return nil, err
	}

	bridge := newLambdaBridge()

	env := envConfig{
		Config:   cloneConfig(cfg),
		Metadata: strategies.CloneMetadata(module.Metadata),
		Events:   append([]schema.EventType(nil), module.Metadata.Events...),
		Helpers:  map[string]any{},
		Runtime:  bridge.helpers(),
	}

	env.Helpers["log"] = makeLogHelper(baseLogger)
	env.Helpers["sleep"] = makeSleepHelper()

	value, err := instance.Call("create", env)
	if err != nil {
		instance.Close()
		return nil, fmt.Errorf("js strategy %s: create failed: %w", module.Name, err)
	}
	rawObj, err := instance.Execute(func(rt *goja.Runtime, _ *goja.Object) (goja.Value, error) {
		obj := value.ToObject(rt)
		if obj == nil {
			return nil, fmt.Errorf("create returned non-object value")
		}
		return obj, nil
	})
	if err != nil {
		instance.Close()
		return nil, fmt.Errorf("js strategy %s: create result invalid: %w", module.Name, err)
	}
	object, ok := rawObj.(*goja.Object)
	if !ok {
		instance.Close()
		return nil, fmt.Errorf("js strategy %s: create result not object", module.Name)
	}

	strategy := &Strategy{
		instance:            instance,
		handler:             object,
		metadata:            strategies.CloneMetadata(module.Metadata),
		logger:              baseLogger,
		runtime:             bridge,
		crossProviderEvents: atomic.Bool{},
	}
	strategy.crossProviderEvents.Store(strategy.detectCrossProviderPreference())
	return strategy, nil
}

func cloneConfig(cfg map[string]any) map[string]any {
	if len(cfg) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	return out
}

func (s *Strategy) detectCrossProviderPreference() bool {
	value, err := s.instance.Execute(func(_ *goja.Runtime, _ *goja.Object) (goja.Value, error) {
		return s.handler.Get("wantsCrossProviderEvents"), nil
	})
	if err != nil || value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return false
	}
	if callable, ok := goja.AssertFunction(value); ok {
		result, err := callable(s.handler)
		if err != nil {
			s.logError("wantsCrossProviderEvents", err)
			return false
		}
		return result.ToBoolean()
	}
	return value.ToBoolean()
}

// Close releases the underlying VM resources.
func (s *Strategy) Close() {
	if s == nil {
		return
	}
	s.instance.Close()
}

// SubscribedEvents reports the static events declared by metadata.
func (s *Strategy) SubscribedEvents() []schema.EventType {
	return append([]schema.EventType(nil), s.metadata.Events...)
}

// WantsCrossProviderEvents reports whether the strategy declared cross-provider support.
func (s *Strategy) WantsCrossProviderEvents() bool {
	return s.crossProviderEvents.Load()
}

// Attach binds the Go runtime context to the strategy helpers.
func (s *Strategy) Attach(base *core.BaseLambda) {
	if s == nil {
		return
	}
	if s.runtime != nil {
		s.runtime.attach(base)
	}
}

// OnTrade dispatches trade updates to the JavaScript handler.
func (s *Strategy) OnTrade(ctx context.Context, evt *schema.Event, payload schema.TradePayload, price float64) {
	s.invoke("onTrade", ctx, evt, payload, price)
}

// OnTicker dispatches ticker updates.
func (s *Strategy) OnTicker(ctx context.Context, evt *schema.Event, payload schema.TickerPayload) {
	s.invoke("onTicker", ctx, evt, payload)
}

// OnBookSnapshot handles book snapshots.
func (s *Strategy) OnBookSnapshot(ctx context.Context, evt *schema.Event, payload schema.BookSnapshotPayload) {
	s.invoke("onBookSnapshot", ctx, evt, payload)
}

// OnKlineSummary handles kline summary events.
func (s *Strategy) OnKlineSummary(ctx context.Context, evt *schema.Event, payload schema.KlineSummaryPayload) {
	s.invoke("onKlineSummary", ctx, evt, payload)
}

// OnInstrumentUpdate handles instrument updates.
func (s *Strategy) OnInstrumentUpdate(ctx context.Context, evt *schema.Event, payload schema.InstrumentUpdatePayload) {
	s.invoke("onInstrumentUpdate", ctx, evt, payload)
}

// OnBalanceUpdate handles balance updates.
func (s *Strategy) OnBalanceUpdate(ctx context.Context, evt *schema.Event, payload schema.BalanceUpdatePayload) {
	s.invoke("onBalanceUpdate", ctx, evt, payload)
}

// OnOrderFilled handles filled orders.
func (s *Strategy) OnOrderFilled(ctx context.Context, evt *schema.Event, payload schema.ExecReportPayload) {
	s.invoke("onOrderFilled", ctx, evt, payload)
}

// OnOrderRejected handles rejected orders.
func (s *Strategy) OnOrderRejected(ctx context.Context, evt *schema.Event, payload schema.ExecReportPayload, reason string) {
	s.invoke("onOrderRejected", ctx, evt, payload, reason)
}

// OnOrderPartialFill handles partial fills.
func (s *Strategy) OnOrderPartialFill(ctx context.Context, evt *schema.Event, payload schema.ExecReportPayload) {
	s.invoke("onOrderPartialFill", ctx, evt, payload)
}

// OnOrderCancelled handles cancellations.
func (s *Strategy) OnOrderCancelled(ctx context.Context, evt *schema.Event, payload schema.ExecReportPayload) {
	s.invoke("onOrderCancelled", ctx, evt, payload)
}

// OnOrderAcknowledged handles acknowledgements.
func (s *Strategy) OnOrderAcknowledged(ctx context.Context, evt *schema.Event, payload schema.ExecReportPayload) {
	s.invoke("onOrderAcknowledged", ctx, evt, payload)
}

// OnOrderExpired handles expirations.
func (s *Strategy) OnOrderExpired(ctx context.Context, evt *schema.Event, payload schema.ExecReportPayload) {
	s.invoke("onOrderExpired", ctx, evt, payload)
}

// OnRiskControl handles risk notifications.
func (s *Strategy) OnRiskControl(ctx context.Context, evt *schema.Event, payload schema.RiskControlPayload) {
	s.invoke("onRiskControl", ctx, evt, payload)
}

// OnExtensionEvent dispatches custom extension payloads when the strategy subscribes to them.
func (s *Strategy) OnExtensionEvent(ctx context.Context, evt *schema.Event, payload any) {
	s.invoke("onExtensionEvent", ctx, evt, payload)
}

func (s *Strategy) invoke(method string, args ...any) {
	if s == nil {
		return
	}
	if _, err := s.instance.CallMethod(s.handler, method, args...); err != nil {
		if errors.Is(err, ErrFunctionMissing) {
			return
		}
		s.logError(method, err)
	}
}

func (s *Strategy) logError(method string, err error) {
	if err == nil {
		return
	}
	if s.logger != nil {
		s.logger.Printf("js strategy %s.%s: %v", strings.ToLower(strings.TrimSpace(s.metadata.Name)), method, err)
	}
}

func defaultStrategyLogger(logger *log.Logger) *log.Logger {
	if logger != nil {
		return logger
	}
	return log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
}

func makeLogHelper(logger *log.Logger) func(args ...any) {
	return func(args ...any) {
		if logger == nil {
			return
		}
		msg := stringifyLogArgs(args...)
		if msg == "" {
			return
		}
		logger.Print(msg)
	}
}

func stringifyLogArgs(args ...any) string {
	if len(args) == 0 {
		return ""
	}
	var builder strings.Builder
	for i, arg := range args {
		if i > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(fmt.Sprint(arg))
	}
	return builder.String()
}

func makeSleepHelper() func(any) {
	return func(value any) {
		duration := parseSleepDuration(value)
		if duration <= 0 {
			return
		}
		time.Sleep(duration)
	}
}

func parseSleepDuration(value any) time.Duration {
	switch v := value.(type) {
	case time.Duration:
		if v < 0 {
			return 0
		}
		return v
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0
		}
		dur, err := time.ParseDuration(trimmed)
		if err != nil {
			return 0
		}
		if dur < 0 {
			return 0
		}
		return dur
	case int:
		if v <= 0 {
			return 0
		}
		return time.Duration(v) * time.Millisecond
	case int64:
		if v <= 0 {
			return 0
		}
		return time.Duration(v) * time.Millisecond
	case float64:
		if v <= 0 {
			return 0
		}
		return time.Duration(v * float64(time.Millisecond))
	case float32:
		if v <= 0 {
			return 0
		}
		return time.Duration(float64(v) * float64(time.Millisecond))
	default:
		return 0
	}
}

type lambdaBridge struct {
	base atomic.Pointer[core.BaseLambda]
}

func newLambdaBridge() *lambdaBridge {
	return &lambdaBridge{
		base: atomic.Pointer[core.BaseLambda]{},
	}
}

func (b *lambdaBridge) attach(base *core.BaseLambda) {
	b.base.Store(base)
}

func (b *lambdaBridge) helpers() map[string]any {
	return map[string]any{
		"isTradingActive":   b.isTradingActive,
		"providers":         b.providers,
		"selectProvider":    b.selectProvider,
		"submitMarketOrder": b.submitMarketOrder,
		"submitOrder":       b.submitOrder,
		"getMarketState":    b.marketState,
		"getBidPrice":       b.bidPrice,
		"getAskPrice":       b.askPrice,
		"isDryRun":          b.isDryRun,
		"getLastPrice":      b.getLastPrice,
	}
}

func (b *lambdaBridge) snapshot() *core.BaseLambda {
	return b.base.Load()
}

func (b *lambdaBridge) isTradingActive() bool {
	base := b.snapshot()
	if base == nil {
		return false
	}
	return base.IsTradingActive()
}

func (b *lambdaBridge) providers() []string {
	base := b.snapshot()
	if base == nil {
		return nil
	}
	return base.Providers()
}

func (b *lambdaBridge) isDryRun() bool {
	base := b.snapshot()
	if base == nil {
		return true
	}
	return base.IsDryRun()
}

func (b *lambdaBridge) getLastPrice() float64 {
	base := b.snapshot()
	if base == nil {
		return 0
	}
	return base.GetLastPrice()
}

func (b *lambdaBridge) bidPrice() float64 {
	base := b.snapshot()
	if base == nil {
		return 0
	}
	return base.GetBidPrice()
}

func (b *lambdaBridge) askPrice() float64 {
	base := b.snapshot()
	if base == nil {
		return 0
	}
	return base.GetAskPrice()
}

func (b *lambdaBridge) marketState() map[string]float64 {
	base := b.snapshot()
	if base == nil {
		return map[string]float64{}
	}
	state := base.GetMarketState()
	return map[string]float64{
		"last":      state.LastPrice,
		"bid":       state.BidPrice,
		"ask":       state.AskPrice,
		"spread":    state.Spread,
		"spreadPct": state.SpreadPct,
	}
}

func (b *lambdaBridge) selectProvider(seed any) (string, error) {
	base := b.snapshot()
	if base == nil {
		return "", fmt.Errorf("lambda unavailable")
	}
	value := convertSeed(seed)
	provider, err := base.SelectProvider(value)
	if err == nil && strings.TrimSpace(provider) != "" {
		return provider, nil
	}
	providers := base.Providers()
	if len(providers) == 0 {
		return "", fmt.Errorf("no providers configured")
	}
	return providers[0], nil
}

func (b *lambdaBridge) submitMarketOrder(provider string, side any, quantity string) error {
	base := b.snapshot()
	if base == nil {
		return fmt.Errorf("lambda unavailable")
	}
	sideValue, err := parseTradeSide(side)
	if err != nil {
		return err
	}
	if strings.TrimSpace(provider) == "" {
		providers := base.Providers()
		if len(providers) == 0 {
			return fmt.Errorf("provider required")
		}
		provider = providers[0]
	}
	if err := base.SubmitMarketOrder(context.Background(), provider, sideValue, quantity); err != nil {
		return fmt.Errorf("submit market order: %w", err)
	}
	return nil
}

func (b *lambdaBridge) submitOrder(provider string, side any, quantity string, price any) error {
	base := b.snapshot()
	if base == nil {
		return fmt.Errorf("lambda unavailable")
	}
	sideValue, err := parseTradeSide(side)
	if err != nil {
		return err
	}
	if strings.TrimSpace(provider) == "" {
		providers := base.Providers()
		if len(providers) == 0 {
			return fmt.Errorf("provider required")
		}
		provider = providers[0]
	}
	priceStr, err := parsePriceString(price)
	if err != nil {
		return err
	}
	if err := base.SubmitOrder(context.Background(), provider, sideValue, quantity, priceStr); err != nil {
		return fmt.Errorf("submit order: %w", err)
	}
	return nil
}

func convertSeed(seed any) uint64 {
	switch v := seed.(type) {
	case uint64:
		return v
	case uint32:
		return uint64(v)
	case uint:
		return uint64(v)
	case int64:
		if v < 0 {
			return uint64(-v)
		}
		return uint64(v)
	case int:
		if v < 0 {
			return uint64(-v)
		}
		return uint64(v)
	case int32:
		if v < 0 {
			return uint64(-v)
		}
		return uint64(v)
	case float64:
		if v < 0 {
			return uint64(-v)
		}
		return uint64(v)
	case float32:
		if v < 0 {
			return uint64(-v)
		}
		return uint64(v)
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return monotonicSeed()
		}
		if parsed, err := strconv.ParseUint(trimmed, 10, 64); err == nil {
			return parsed
		}
		if parsedFloat, err := strconv.ParseFloat(trimmed, 64); err == nil {
			if parsedFloat < 0 {
				return uint64(-parsedFloat)
			}
			return uint64(parsedFloat)
		}
		return monotonicSeed()
	default:
		return monotonicSeed()
	}
}

func monotonicSeed() uint64 {
	now := time.Now().UnixNano()
	if now < 0 {
		return uint64(-now)
	}
	return uint64(now)
}

func parseTradeSide(side any) (schema.TradeSide, error) {
	switch v := side.(type) {
	case schema.TradeSide:
		if v == schema.TradeSideBuy || v == schema.TradeSideSell {
			return v, nil
		}
	case string:
		trimmed := strings.ToLower(strings.TrimSpace(v))
		switch trimmed {
		case "buy", "long", "bid":
			return schema.TradeSideBuy, nil
		case "sell", "short", "ask":
			return schema.TradeSideSell, nil
		}
	case float64:
		if v >= 0 {
			return schema.TradeSideBuy, nil
		}
		return schema.TradeSideSell, nil
	case float32:
		if v >= 0 {
			return schema.TradeSideBuy, nil
		}
		return schema.TradeSideSell, nil
	case int:
		if v >= 0 {
			return schema.TradeSideBuy, nil
		}
		return schema.TradeSideSell, nil
	case int64:
		if v >= 0 {
			return schema.TradeSideBuy, nil
		}
		return schema.TradeSideSell, nil
	case int32:
		if v >= 0 {
			return schema.TradeSideBuy, nil
		}
		return schema.TradeSideSell, nil
	case uint, uint32, uint64:
		return schema.TradeSideBuy, nil
	}
	return "", fmt.Errorf("invalid trade side %v", side)
}

func parsePriceString(value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case *string:
		if v == nil {
			return nil, nil
		}
		trimmed := strings.TrimSpace(*v)
		if trimmed == "" {
			return nil, nil
		}
		return &trimmed, nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil, nil
		}
		return &trimmed, nil
	case float64:
		formatted := strconv.FormatFloat(v, 'f', -1, 64)
		return &formatted, nil
	case float32:
		formatted := strconv.FormatFloat(float64(v), 'f', -1, 64)
		return &formatted, nil
	case int:
		formatted := strconv.Itoa(v)
		return &formatted, nil
	case int64:
		formatted := strconv.FormatInt(v, 10)
		return &formatted, nil
	case int32:
		formatted := strconv.FormatInt(int64(v), 10)
		return &formatted, nil
	case uint:
		formatted := strconv.FormatUint(uint64(v), 10)
		return &formatted, nil
	case uint32:
		formatted := strconv.FormatUint(uint64(v), 10)
		return &formatted, nil
	case uint64:
		formatted := strconv.FormatUint(v, 10)
		return &formatted, nil
	default:
		return nil, fmt.Errorf("unsupported price type %T", value)
	}
}
