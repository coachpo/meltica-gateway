package binance

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/telemetry"
)

type providerMetrics struct {
	environment string
	provider    string

	ordersReceived   metric.Int64Counter
	ordersRejected   metric.Int64Counter
	orderLatency     metric.Float64Histogram
	eventsEmitted    metric.Int64Counter
	balanceUpdates   metric.Int64Counter
	venueErrors      metric.Int64Counter
	venueDisruptions metric.Int64Counter
	balanceTotal     metric.Float64ObservableGauge
	balanceAvailable metric.Float64ObservableGauge
}

func newProviderMetrics(p *Provider) *providerMetrics {
	if p == nil {
		return nil
	}

	meter := otel.Meter("adapter.binance")
	env := telemetry.Environment()
	providerName := strings.TrimSpace(p.name)
	if providerName == "" {
		providerName = binanceMetadata.identifier
	}

	pm := &providerMetrics{
		environment:      env,
		provider:         providerName,
		ordersReceived:   nil,
		ordersRejected:   nil,
		orderLatency:     nil,
		eventsEmitted:    nil,
		balanceUpdates:   nil,
		venueErrors:      nil,
		venueDisruptions: nil,
		balanceTotal:     nil,
		balanceAvailable: nil,
	}

	pm.ordersReceived, _ = meter.Int64Counter("meltica_provider_binance_orders_received",
		metric.WithDescription("Total Binance execution reports received by Meltica"),
		metric.WithUnit("{report}"))

	pm.ordersRejected, _ = meter.Int64Counter("meltica_provider_binance_orders_rejected",
		metric.WithDescription("Total Binance order rejections observed by Meltica"),
		metric.WithUnit("{reject}"))

	pm.orderLatency, _ = meter.Float64Histogram("meltica_provider_binance_order_latency",
		metric.WithDescription("Latency between Binance execution report timestamp and ingestion"),
		metric.WithUnit("ms"))

	pm.eventsEmitted, _ = meter.Int64Counter("meltica_provider_binance_events_emitted",
		metric.WithDescription("Canonical Meltica events emitted by the Binance adapter"),
		metric.WithUnit("{event}"))

	pm.balanceUpdates, _ = meter.Int64Counter("meltica_provider_binance_balance_updates",
		metric.WithDescription("Binance balance updates emitted by the adapter"),
		metric.WithUnit("{update}"))

	pm.venueErrors, _ = meter.Int64Counter("meltica_provider_binance_venue_errors",
		metric.WithDescription("Errors reported by the Binance adapter"),
		metric.WithUnit("{error}"))

	pm.venueDisruptions, _ = meter.Int64Counter("meltica_provider_binance_venue_disruptions",
		metric.WithDescription("Binance venue disruptions detected by the adapter"),
		metric.WithUnit("{disruption}"))

	pm.balanceTotal, _ = meter.Float64ObservableGauge("meltica_provider_binance_balance_total",
		metric.WithDescription("Total balances tracked for Binance account"),
		metric.WithUnit("USD"),
		metric.WithFloat64Callback(func(_ context.Context, observer metric.Float64Observer) error {
			p.balanceMu.Lock()
			defer p.balanceMu.Unlock()
			for currency, snapshot := range p.balances {
				total, _ := snapshot.total().Float64()
				attrs := telemetry.BalanceAttributes(pm.environment, pm.provider, currency)
				observer.Observe(total, metric.WithAttributes(attrs...))
			}
			return nil
		}))

	pm.balanceAvailable, _ = meter.Float64ObservableGauge("meltica_provider_binance_balance_available",
		metric.WithDescription("Available balances tracked for Binance account"),
		metric.WithUnit("USD"),
		metric.WithFloat64Callback(func(_ context.Context, observer metric.Float64Observer) error {
			p.balanceMu.Lock()
			defer p.balanceMu.Unlock()
			for currency, snapshot := range p.balances {
				available, _ := snapshot.free.Float64()
				attrs := telemetry.BalanceAttributes(pm.environment, pm.provider, currency)
				observer.Observe(available, metric.WithAttributes(attrs...))
			}
			return nil
		}))

	return pm
}

func (pm *providerMetrics) recordEvent(ctx context.Context, eventType, symbol string) {
	if pm == nil || pm.eventsEmitted == nil {
		return
	}
	ctx = ensureContext(ctx)
	attrs := telemetry.EventAttributes(pm.environment, eventType, pm.provider, symbol)
	pm.eventsEmitted.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func (pm *providerMetrics) recordOrder(ctx context.Context, symbol string, side schema.TradeSide, orderType schema.OrderType, tif string, state schema.ExecReportState) {
	if pm == nil || pm.ordersReceived == nil {
		return
	}
	ctx = ensureContext(ctx)
	sideAttr := strings.ToLower(string(side))
	typeAttr := strings.ToLower(string(orderType))
	tifAttr := strings.ToUpper(strings.TrimSpace(tif))
	stateAttr := strings.ToUpper(strings.TrimSpace(string(state)))
	attrs := telemetry.OrderAttributes(pm.environment, pm.provider, symbol, sideAttr, typeAttr, tifAttr)
	if stateAttr != "" {
		attrs = append(attrs, telemetry.AttrOrderState.String(stateAttr))
	}
	pm.ordersReceived.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func (pm *providerMetrics) recordOrderRejection(ctx context.Context, symbol, state, reason string) {
	if pm == nil || pm.ordersRejected == nil {
		return
	}
	ctx = ensureContext(ctx)
	attrs := []attribute.KeyValue{
		telemetry.AttrEnvironment.String(pm.environment),
		telemetry.AttrProvider.String(pm.provider),
	}
	if symbol != "" {
		attrs = append(attrs, telemetry.AttrSymbol.String(symbol))
	}
	if state != "" {
		attrs = append(attrs, telemetry.AttrOrderState.String(strings.ToUpper(state)))
	}
	if reason != "" {
		attrs = append(attrs, telemetry.AttrReason.String(strings.ToLower(reason)))
	}
	pm.ordersRejected.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func (pm *providerMetrics) recordOrderLatency(ctx context.Context, symbol string, side schema.TradeSide, orderType schema.OrderType, tif string, state schema.ExecReportState, latency time.Duration) {
	if pm == nil || pm.orderLatency == nil {
		return
	}
	ctx = ensureContext(ctx)
	if latency < 0 {
		latency = 0
	}
	sideAttr := strings.ToLower(string(side))
	typeAttr := strings.ToLower(string(orderType))
	tifAttr := strings.ToUpper(strings.TrimSpace(tif))
	stateAttr := strings.ToUpper(strings.TrimSpace(string(state)))
	attrs := telemetry.OrderAttributes(pm.environment, pm.provider, symbol, sideAttr, typeAttr, tifAttr)
	if stateAttr != "" {
		attrs = append(attrs, telemetry.AttrOrderState.String(stateAttr))
	}
	pm.orderLatency.Record(ctx, float64(latency.Milliseconds()), metric.WithAttributes(attrs...))
}

func (pm *providerMetrics) recordBalanceUpdate(ctx context.Context, currency string) {
	if pm == nil || pm.balanceUpdates == nil {
		return
	}
	ctx = ensureContext(ctx)
	attrs := telemetry.BalanceAttributes(pm.environment, pm.provider, currency)
	pm.balanceUpdates.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func (pm *providerMetrics) recordVenueError(ctx context.Context, operation, result string) {
	if pm == nil || pm.venueErrors == nil {
		return
	}
	ctx = ensureContext(ctx)
	attrs := telemetry.OperationResultAttributes(pm.environment, pm.provider, operation, result)
	pm.venueErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func (pm *providerMetrics) recordVenueDisruption(ctx context.Context, reason string) {
	if pm == nil || pm.venueDisruptions == nil || strings.TrimSpace(reason) == "" {
		return
	}
	ctx = ensureContext(ctx)
	attrs := telemetry.ConnectionAttributes(pm.environment, pm.provider, reason)
	pm.venueDisruptions.Add(ctx, 1, metric.WithAttributes(attrs...))
}

type streamMetrics struct {
	environment string
	provider    string
	stream      string

	reconnects       metric.Int64Counter
	controlMessages  metric.Int64Counter
	messagesReceived metric.Int64Counter
	messageBytes     metric.Int64Histogram
	pingCount        metric.Int64Counter
	pingLatency      metric.Float64Histogram
	subscriptions    metric.Int64UpDownCounter
}

func newStreamMetrics(provider, stream string) *streamMetrics {
	meter := otel.Meter("adapter.binance")
	env := telemetry.Environment()
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = binanceMetadata.identifier
	}

	sm := &streamMetrics{
		environment:      env,
		provider:         provider,
		stream:           stream,
		reconnects:       nil,
		controlMessages:  nil,
		messagesReceived: nil,
		messageBytes:     nil,
		pingCount:        nil,
		pingLatency:      nil,
		subscriptions:    nil,
	}

	sm.reconnects, _ = meter.Int64Counter("meltica_provider_binance_ws_reconnects",
		metric.WithDescription("Number of Binance websocket reconnect attempts"),
		metric.WithUnit("{reconnect}"))

	sm.controlMessages, _ = meter.Int64Counter("meltica_provider_binance_ws_control_messages",
		metric.WithDescription("Control messages sent by Binance websocket stream managers"),
		metric.WithUnit("{message}"))

	sm.messagesReceived, _ = meter.Int64Counter("meltica_provider_binance_ws_messages",
		metric.WithDescription("Stream messages received from Binance websocket connections"),
		metric.WithUnit("{message}"))

	sm.messageBytes, _ = meter.Int64Histogram("meltica_provider_binance_ws_message_bytes",
		metric.WithDescription("Size of Binance websocket stream messages"),
		metric.WithUnit("By"))

	sm.pingCount, _ = meter.Int64Counter("meltica_provider_binance_ws_pings",
		metric.WithDescription("Ping frames sent by Binance websocket stream managers"),
		metric.WithUnit("{ping}"))

	sm.pingLatency, _ = meter.Float64Histogram("meltica_provider_binance_ws_ping_latency",
		metric.WithDescription("Latency of ping frames on Binance websocket connections"),
		metric.WithUnit("ms"))

	sm.subscriptions, _ = meter.Int64UpDownCounter("meltica_provider_binance_ws_active_subscriptions",
		metric.WithDescription("Active Binance websocket stream subscriptions tracked by the adapter"),
		metric.WithUnit("{stream}"))

	return sm
}

func (sm *streamMetrics) baseAttrs() []attribute.KeyValue {
	if sm == nil {
		return nil
	}
	return []attribute.KeyValue{
		telemetry.AttrEnvironment.String(sm.environment),
		telemetry.AttrProvider.String(sm.provider),
		telemetry.AttrMessageType.String(sm.stream),
	}
}

func (sm *streamMetrics) recordReconnect(ctx context.Context, result string) {
	if sm == nil || sm.reconnects == nil {
		return
	}
	ctx = ensureContext(ctx)
	attrs := sm.baseAttrs()
	if result != "" {
		attrs = append(attrs, telemetry.AttrResult.String(result))
	}
	sm.reconnects.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func (sm *streamMetrics) recordControl(ctx context.Context, method string, count int) {
	if sm == nil || sm.controlMessages == nil || count == 0 {
		return
	}
	ctx = ensureContext(ctx)
	attrs := sm.baseAttrs()
	if method != "" {
		attrs = append(attrs, telemetry.AttrCommandType.String(strings.ToUpper(method)))
	}
	sm.controlMessages.Add(ctx, int64(count), metric.WithAttributes(attrs...))
}

func (sm *streamMetrics) recordMessage(ctx context.Context, bytes int) {
	if sm == nil || sm.messagesReceived == nil || sm.messageBytes == nil || bytes <= 0 {
		return
	}
	ctx = ensureContext(ctx)
	attrs := sm.baseAttrs()
	sm.messagesReceived.Add(ctx, 1, metric.WithAttributes(attrs...))
	sm.messageBytes.Record(ctx, int64(bytes), metric.WithAttributes(attrs...))
}

func (sm *streamMetrics) recordPing(ctx context.Context, latency time.Duration, result string) {
	if sm == nil || sm.pingCount == nil || sm.pingLatency == nil {
		return
	}
	ctx = ensureContext(ctx)
	if latency < 0 {
		latency = 0
	}
	attrs := sm.baseAttrs()
	if result != "" {
		attrs = append(attrs, telemetry.AttrResult.String(result))
	}
	sm.pingCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	sm.pingLatency.Record(ctx, float64(latency.Milliseconds()), metric.WithAttributes(attrs...))
}

func (sm *streamMetrics) adjustSubscriptions(ctx context.Context, delta int) {
	if sm == nil || sm.subscriptions == nil || delta == 0 {
		return
	}
	ctx = ensureContext(ctx)
	attrs := sm.baseAttrs()
	sm.subscriptions.Add(ctx, int64(delta), metric.WithAttributes(attrs...))
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func classifyBinanceError(err error) (operation, result, disruption string) {
	if err == nil {
		return "", "", ""
	}
	if errors.Is(err, context.Canceled) {
		return "adapter.binance", "context_canceled", ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "adapter.binance", "timeout", "timeout"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "dial"):
		return "adapter.binance.websocket", "dial_error", "dial_error"
	case strings.Contains(msg, "websocket"):
		return "adapter.binance.websocket", "websocket_error", "websocket"
	case strings.Contains(msg, "out of sync"):
		return "adapter.binance", "orderbook_out_of_sync", "orderbook_resync"
	case strings.Contains(msg, "listen key"):
		return "adapter.binance.user_stream", "listen_key_error", "user_stream"
	case strings.Contains(msg, "user stream"):
		return "adapter.binance.user_stream", "user_stream_error", "user_stream"
	case strings.Contains(msg, "orderbook"):
		return "adapter.binance", "orderbook_error", ""
	case strings.Contains(msg, "decode"):
		return "adapter.binance", "decode_error", ""
	case strings.Contains(msg, "timeout"):
		return "adapter.binance", "timeout", "timeout"
	case strings.Contains(msg, "remote closed"):
		return "adapter.binance.websocket", "remote_closed", "connection_closed"
	case strings.Contains(msg, "seed orderbook"):
		return "adapter.binance", "orderbook_seed_error", "orderbook_seed"
	case strings.Contains(msg, "reconnect"):
		return "adapter.binance.websocket", "reconnect_error", "reconnect"
	default:
		return "adapter.binance", "error", ""
	}
}
