// Package telemetry provides semantic conventions for Meltica observability.
package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
)

// Semantic convention attribute keys for Meltica-specific telemetry.
// Following OpenTelemetry naming conventions: namespace.attribute_name

const (
	// AttrEventType is the attribute key for event type labels.
	AttrEventType = attribute.Key("event.type")
	// AttrProvider is the attribute key for provider identifiers.
	AttrProvider = attribute.Key("provider")
	// AttrSymbol is the attribute key for instrument symbols.
	AttrSymbol = attribute.Key("symbol")
	// AttrMessageType is the attribute key for provider message kinds.
	AttrMessageType = attribute.Key("message.type")
	// AttrCurrency is the attribute key for currency codes.
	AttrCurrency = attribute.Key("currency")
	// AttrOrderSide is the attribute key for order side labels.
	AttrOrderSide = attribute.Key("order.side")
	// AttrOrderType is the attribute key for order type labels.
	AttrOrderType = attribute.Key("order.type")
	// AttrOrderTIF is the attribute key for time-in-force descriptors.
	AttrOrderTIF = attribute.Key("order.tif")
	// AttrOrderState is the attribute key for order state labels.
	AttrOrderState = attribute.Key("order.state")
	// AttrPoolName is the attribute key for pool identifiers.
	AttrPoolName = attribute.Key("pool.name")
	// AttrObjectType is the attribute key for pooled object types.
	AttrObjectType = attribute.Key("object.type")
	// AttrOperation is the attribute key for operation labels.
	AttrOperation = attribute.Key("operation")
	// AttrResult is the attribute key for operation result labels.
	AttrResult = attribute.Key("result")
	// AttrEnvironment is the attribute key for environment identifiers.
	AttrEnvironment = attribute.Key("environment")
	// AttrErrorType is the attribute key for error type labels.
	AttrErrorType = attribute.Key("error.type")
	// AttrReason is the attribute key for error reasons.
	AttrReason = attribute.Key("reason")
	// AttrCommandType is the attribute key for control bus command types.
	AttrCommandType = attribute.Key("command.type")
	// AttrStatus is the attribute key for command status values.
	AttrStatus = attribute.Key("status")
	// AttrConnectionState is the attribute key for connection state labels.
	AttrConnectionState = attribute.Key("connection.state")
)

// Event type values
const (
	EventTypeBookSnapshot  = "book_snapshot"
	EventTypeTrade         = "trade"
	EventTypeTicker        = "ticker"
	EventTypeKline         = "kline"
	EventTypeExecReport    = "exec_report"
	EventTypeBalanceUpdate = "balance_update"
)

// Provider values
const (
	ProviderBinance = "binance"
	ProviderFake    = "fake"
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
