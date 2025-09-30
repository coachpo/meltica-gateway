package observability

import (
	"errors"
	"fmt"
)

// AggregateErrors joins multiple errors, emits a structured log entry, and returns an aggregated error.
func AggregateErrors(operation string, errs []error, fields ...Field) error {
	filtered := make([]error, 0, len(errs))
	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		filtered = append(filtered, err)
		messages = append(messages, err.Error())
	}
	if len(filtered) == 0 {
		return nil
	}
	logFields := append(fields,
		Field{Key: "operation", Value: operation},
		Field{Key: "error_count", Value: len(filtered)},
		Field{Key: "errors", Value: messages},
	)
	Log().Error("operation errors", logFields...)
	joined := errors.Join(filtered...)
	return fmt.Errorf("%s failed: %w", operation, joined)
}
