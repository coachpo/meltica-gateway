// Package telemetry provides semantic conventions for Meltica observability.
package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
)

// Semantic convention attribute keys for Meltica-specific telemetry.
// Following OpenTelemetry naming conventions: namespace.attribute_name

const (
	// AttrEventType annotates counters/histograms with the canonical Meltica event classification (e.g. Trade, Ticker).
	AttrEventType = attribute.Key("event.type")
	// AttrProvider identifies which upstream venue or adapter produced the signal.
	AttrProvider = attribute.Key("provider")
	// AttrSymbol captures the tradable instrument symbol (e.g. BTC-USDT).
	AttrSymbol = attribute.Key("symbol")
	// AttrMessageType differentiates provider-specific payload classes inside a single transport stream.
	AttrMessageType = attribute.Key("message.type")
	// AttrCurrency stores ISO-like currency codes for balance metrics.
	AttrCurrency = attribute.Key("currency")
	// AttrOrderSide labels order telemetry with Buy/Sell intent.
	AttrOrderSide = attribute.Key("order.side")
	// AttrOrderType distinguishes limit vs market orders in execution metrics.
	AttrOrderType = attribute.Key("order.type")
	// AttrOrderTIF records time-in-force hints (GTC, IOC, etc.) for order analytics.
	AttrOrderTIF = attribute.Key("order.tif")
	// AttrOrderState captures the execution lifecycle state reported (ACK, FILLED, REJECTED, ...).
	AttrOrderState = attribute.Key("order.state")
	// AttrPoolName labels pooled object metrics by logical pool (Event, OrderRequest, ...).
	AttrPoolName = attribute.Key("pool.name")
	// AttrObjectType captures the Go type being managed inside a pool.
	AttrObjectType = attribute.Key("object.type")
	// AttrOperation differentiates specific provider operations (e.g. venue_link, venue_error).
	AttrOperation = attribute.Key("operation")
	// AttrResult records the outcome of an operation (success, error class, etc.).
	AttrResult = attribute.Key("result")
	// AttrEnvironment specifies the deployment environment (dev/staging/prod) for every metric.
	AttrEnvironment = attribute.Key("environment")
	// AttrErrorType categorizes failures by canonical error family.
	AttrErrorType = attribute.Key("error.type")
	// AttrReason provides additional free-form context for errors/rejections.
	AttrReason = attribute.Key("reason")
	// AttrCommandType indicates which control-plane command (SUBSCRIBE/UNSUBSCRIBE/etc.) was processed.
	AttrCommandType = attribute.Key("command.type")
	// AttrStatus communicates the success/failure state of a control command.
	AttrStatus = attribute.Key("status")
	// AttrConnectionState labels connection lifecycle signals (connected, reconnecting, ...).
	AttrConnectionState = attribute.Key("connection.state")
)

// Event type values
const (
	EventTypeBookSnapshot     = "book_snapshot"
	EventTypeTrade            = "trade"
	EventTypeTicker           = "ticker"
	EventTypeInstrumentUpdate = "instrument_update"
	EventTypeKline            = "kline"
	EventTypeExecReport       = "exec_report"
	EventTypeBalanceUpdate    = "balance_update"
)

// Helper functions for creating common attribute sets

// EventAttributes returns common attributes for event metrics.
func EventAttributes(environment, eventType, provider, symbol string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrEnvironment.String(environment),
		AttrEventType.String(eventType),
		AttrProvider.String(provider),
		AttrSymbol.String(symbol),
	}
}

// OrderAttributes returns attributes for order-related metrics.
func OrderAttributes(environment, provider, symbol, side, orderType, tif string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		AttrEnvironment.String(environment),
		AttrProvider.String(provider),
	}
	if symbol != "" {
		attrs = append(attrs, AttrSymbol.String(symbol))
	}
	if side != "" {
		attrs = append(attrs, AttrOrderSide.String(side))
	}
	if orderType != "" {
		attrs = append(attrs, AttrOrderType.String(orderType))
	}
	if tif != "" {
		attrs = append(attrs, AttrOrderTIF.String(tif))
	}
	return attrs
}

// BalanceAttributes returns attributes for balance telemetry.
func BalanceAttributes(environment, provider, currency string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		AttrEnvironment.String(environment),
		AttrProvider.String(provider),
	}
	if currency != "" {
		attrs = append(attrs, AttrCurrency.String(currency))
	}
	return attrs
}

// PoolAttributes returns common attributes for pool metrics.
func PoolAttributes(environment, poolName, objectType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrEnvironment.String(environment),
		AttrPoolName.String(poolName),
		AttrObjectType.String(objectType),
	}
}

// ErrorAttributes returns attributes for error metrics.
func ErrorAttributes(environment, errorType, reason string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrEnvironment.String(environment),
		AttrErrorType.String(errorType),
		AttrReason.String(reason),
	}
}

// CommandAttributes returns attributes for control bus command metrics.
func CommandAttributes(environment, commandType, status string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrEnvironment.String(environment),
		AttrCommandType.String(commandType),
		AttrStatus.String(status),
	}
}

// ConnectionAttributes returns attributes for connection state metrics.
func ConnectionAttributes(environment, provider, state string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrEnvironment.String(environment),
		AttrProvider.String(provider),
		AttrConnectionState.String(state),
	}
}

// MessageAttributes returns attributes for provider message metrics.
func MessageAttributes(environment, provider, messageType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrEnvironment.String(environment),
		AttrProvider.String(provider),
		AttrMessageType.String(messageType),
	}
}

// OperationResultAttributes returns attributes for operation metrics with result classification.
func OperationResultAttributes(environment, provider, operation, result string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrEnvironment.String(environment),
		AttrProvider.String(provider),
		AttrOperation.String(operation),
		AttrResult.String(result),
	}
}
