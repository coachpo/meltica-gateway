package postgres

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// numericFromString converts a decimal string into a pgtype.Numeric value.
func numericFromString(value string) (pgtype.Numeric, error) {
	var out pgtype.Numeric
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return out, fmt.Errorf("numeric value required")
	}
	if err := out.Scan(trimmed); err != nil {
		return out, fmt.Errorf("parse numeric %q: %w", trimmed, err)
	}
	return out, nil
}

// numericFromOptional converts an optional decimal string pointer into a pgtype.Numeric.
func numericFromOptional(ptr *string) (pgtype.Numeric, error) {
	var out pgtype.Numeric
	if ptr == nil {
		return out, nil
	}
	trimmed := strings.TrimSpace(*ptr)
	if trimmed == "" {
		return out, nil
	}
	if err := out.Scan(trimmed); err != nil {
		return out, fmt.Errorf("parse numeric %q: %w", trimmed, err)
	}
	return out, nil
}
