// Package observability defines shared logging primitives.
package observability

// Logger captures structured logging behaviours shared across layers.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Error(msg string, fields ...Field)
}

// Field represents a key/value pair for structured logging.
type Field struct {
	Key   string
	Value any
}

var defaultLogger Logger = noopLogger{}

// SetLogger overrides the global logger used by the system.
func SetLogger(logger Logger) {
	if logger == nil {
		defaultLogger = noopLogger{}
		return
	}
	defaultLogger = logger
}

// Log returns the current global logger instance.
func Log() Logger {
	return defaultLogger
}

type noopLogger struct{}

func (noopLogger) Debug(string, ...Field) {}
func (noopLogger) Info(string, ...Field)  {}
func (noopLogger) Error(string, ...Field) {}
