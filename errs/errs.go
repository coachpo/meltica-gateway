// Package errs provides structured error types and helpers for Meltica services.
package errs

import (
	"sort"
	"strconv"
	"strings"
)

// Code identifies an exchange-specific error category.
type Code string

const (
	// CodeRateLimited indicates that the request exceeded rate limits.
	CodeRateLimited Code = "rate_limited"
	// CodeAuth indicates authentication or authorization errors.
	CodeAuth Code = "auth"
	// CodeInvalid indicates invalid input provided by the caller.
	CodeInvalid Code = "invalid_request"
	// CodeExchange indicates an exchange-side failure.
	CodeExchange Code = "exchange_error"
	// CodeNetwork indicates a network transport failure.
	CodeNetwork Code = "network"
	// CodeNotFound indicates a missing resource.
	CodeNotFound Code = "not_found"
	// CodeConflict indicates a concurrent mutation conflict.
	CodeConflict Code = "conflict"
	// CodeUnavailable indicates the service is temporarily unavailable.
	CodeUnavailable Code = "unavailable"
)

// CanonicalCode captures exchange-agnostic error categories.
type CanonicalCode string

const (
	// CanonicalUnknown captures uncategorized failures.
	CanonicalUnknown CanonicalCode = "unknown"
	// CanonicalCapabilityMissing indicates the adapter lacks the required capability.
	CanonicalCapabilityMissing CanonicalCode = "capability_missing"
	// CanonicalOrderNotFound indicates that the referenced order does not exist.
	CanonicalOrderNotFound CanonicalCode = "order_not_found"
	// CanonicalInsufficientBalance indicates insufficient balance for the requested operation.
	CanonicalInsufficientBalance CanonicalCode = "insufficient_balance"
	// CanonicalInvalidSymbol indicates an unsupported or malformed symbol.
	CanonicalInvalidSymbol CanonicalCode = "invalid_symbol"
	// CanonicalRateLimited indicates the request was rate limited.
	CanonicalRateLimited CanonicalCode = "rate_limited"
)

// E captures structured error information produced across the Meltica stack.
type E struct {
	Exchange      string
	Code          Code
	HTTP          int
	RawCode       string
	RawMsg        string
	Message       string
	Canonical     CanonicalCode
	VenueMetadata map[string]string
	Remediation   string

	cause error
}

// Option configures an error envelope.
type Option func(*E)

// New constructs an error envelope for the exchange and error code.
func New(exchange string, code Code, opts ...Option) *E {
	e := &E{
		Exchange:      strings.TrimSpace(exchange),
		Code:          code,
		HTTP:          0,
		RawCode:       "",
		RawMsg:        "",
		Message:       "",
		Canonical:     CanonicalUnknown,
		VenueMetadata: nil,
		Remediation:   "",
		cause:         nil,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	return e
}

// WithMessage attaches a human-readable message to the error.
func WithMessage(message string) Option {
	trimmed := strings.TrimSpace(message)
	return func(e *E) {
		e.Message = trimmed
	}
}

// WithRemediation attaches remediation guidance to the error.
func WithRemediation(remediation string) Option {
	trimmed := strings.TrimSpace(remediation)
	return func(e *E) {
		e.Remediation = trimmed
	}
}

// WithHTTP records the associated HTTP status code.
func WithHTTP(status int) Option {
	return func(e *E) {
		e.HTTP = status
	}
}

// WithRawCode captures the raw exchange error code.
func WithRawCode(code string) Option {
	trimmed := strings.TrimSpace(code)
	return func(e *E) {
		e.RawCode = trimmed
	}
}

// WithRawMessage captures the raw exchange error message.
func WithRawMessage(msg string) Option {
	return func(e *E) {
		e.RawMsg = msg
	}
}

// WithCause sets the underlying cause error.
func WithCause(err error) Option {
	return func(e *E) {
		e.cause = err
	}
}

// WithCanonicalCode sets the canonical error code describing the failure category.
func WithCanonicalCode(code CanonicalCode) Option {
	trimmed := strings.TrimSpace(string(code))
	return func(e *E) {
		if trimmed == "" {
			e.Canonical = CanonicalUnknown
			return
		}
		e.Canonical = CanonicalCode(trimmed)
	}
}

// WithVenueMetadata merges the provided venue metadata into the error envelope.
func WithVenueMetadata(meta map[string]string) Option {
	return func(e *E) {
		if len(meta) == 0 {
			return
		}
		if e.VenueMetadata == nil {
			e.VenueMetadata = make(map[string]string, len(meta))
		}
		for k, v := range meta {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			value := strings.TrimSpace(v)
			e.VenueMetadata[key] = value
		}
	}
}

// WithVenueField appends a single venue metadata key/value pair.
func WithVenueField(key, value string) Option {
	return func(e *E) {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			return
		}
		if e.VenueMetadata == nil {
			e.VenueMetadata = make(map[string]string, 1)
		}
		e.VenueMetadata[trimmedKey] = strings.TrimSpace(value)
	}
}

func (e *E) Error() string {
	if e == nil {
		return "<nil>"
	}
	var parts []string

	exchange := strings.TrimSpace(e.Exchange)
	if exchange == "" {
		exchange = "unknown"
	}
	parts = append(parts, "exchange="+exchange)

	code := strings.TrimSpace(string(e.Code))
	if code == "" {
		code = "unknown"
	}
	parts = append(parts, "code="+code)

	if cc := strings.TrimSpace(string(e.Canonical)); cc != "" && cc != string(CanonicalUnknown) {
		parts = append(parts, "canonical="+cc)
	}

	if e.HTTP > 0 {
		parts = append(parts, "http="+strconv.Itoa(e.HTTP))
	}
	if e.Message != "" {
		parts = append(parts, "message="+strconv.Quote(e.Message))
	}
	if e.Remediation != "" {
		parts = append(parts, "remediation="+strconv.Quote(e.Remediation))
	}
	if e.RawCode != "" {
		parts = append(parts, "raw_code="+strconv.Quote(e.RawCode))
	}
	if e.RawMsg != "" {
		parts = append(parts, "raw_msg="+strconv.Quote(e.RawMsg))
	}
	if len(e.VenueMetadata) > 0 {
		keys := make([]string, 0, len(e.VenueMetadata))
		for k := range e.VenueMetadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			v := e.VenueMetadata[k]
			pairs = append(pairs, k+"="+strconv.Quote(v))
		}
		parts = append(parts, "venue="+strings.Join(pairs, ","))
	}
	if e.cause != nil {
		parts = append(parts, "cause="+strconv.Quote(e.cause.Error()))
	}

	return strings.Join(parts, " ")
}

func (e *E) Unwrap() error { return e.cause }

// NotSupported returns a standardized error for unsupported capabilities.
func NotSupported(msg string) *E {
	return New("", CodeExchange, WithMessage(strings.TrimSpace(msg)), WithCanonicalCode(CanonicalCapabilityMissing))
}
