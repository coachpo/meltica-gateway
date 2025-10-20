// Package telemetry provides semantic conventions for Meltica observability.
package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
)

// Semantic convention attribute keys for Meltica-specific telemetry.
// Following OpenTelemetry naming conventions: namespace.attribute_name
const (
	// Event attributes
	AttrEventType   = attribute.Key("event.type")
	AttrProvider    = attribute.Key("provider")
	AttrSymbol      = attribute.Key("symbol")
	AttrMessageType = attribute.Key("message.type")

	// Pool attributes
	AttrPoolName   = attribute.Key("pool.name")
	AttrObjectType = attribute.Key("object.type")
	AttrOperation  = attribute.Key("operation")
	AttrResult     = attribute.Key("result")

	// Environment attribute
	AttrEnvironment = attribute.Key("environment")

	// Error attributes
	AttrErrorType = attribute.Key("error.type")
	AttrReason    = attribute.Key("reason")

	// Control bus attributes
	AttrCommandType = attribute.Key("command.type")
	AttrStatus      = attribute.Key("status")

	// Connection attributes
	AttrConnectionState = attribute.Key("connection.state")
)

// Event type values
const (
	EventTypeBookSnapshot = "book_snapshot"
	EventTypeTrade        = "trade"
	EventTypeTicker       = "ticker"
	EventTypeKline        = "kline"
	EventTypeExecReport   = "exec_report"
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
