package errs

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	err := New("test-op", CodeInvalid, WithMessage("test message"))
	
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	
	errStr := err.Error()
	if errStr == "" {
		t.Error("expected non-empty error string")
	}
}

func TestErrorString(t *testing.T) {
	err := New("dispatch", CodeNotFound, WithMessage("route not found"))
	
	str := err.Error()
	if str == "" {
		t.Error("expected non-empty error string")
	}
	
	if !strings.Contains(str, "dispatch") && !strings.Contains(str, "route not found") {
		t.Error("expected operation or message in error string")
	}
}

func TestWithMessage(t *testing.T) {
	err := New("test", CodeInvalid, WithMessage("custom message"))
	
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	
	errStr := err.Error()
	if !strings.Contains(errStr, "custom message") {
		t.Error("expected custom message in error string")
	}
}

func TestErrorCodes(t *testing.T) {
	codes := []Code{
		CodeInvalid,
		CodeNotFound,
		CodeConflict,
		CodeUnavailable,
	}
	
	for _, code := range codes {
		if string(code) == "" {
			t.Errorf("expected non-empty code string for %v", code)
		}
	}
}

func TestNewWithOptions(t *testing.T) {
	err := New(
		"test-operation",
		CodeInvalid,
		WithMessage("test failed"),
	)
	
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	
	errStr := err.Error()
	if errStr == "" {
		t.Error("expected non-empty error string")
	}
}

func TestWithRemediation(t *testing.T) {
	err := New("test", CodeInvalid, WithRemediation("fix your input"))
	
	if err.Remediation != "fix your input" {
		t.Errorf("expected remediation to be set, got %q", err.Remediation)
	}
}

func TestWithHTTP(t *testing.T) {
	err := New("test", CodeInvalid, WithHTTP(400))
	
	if err.HTTP != 400 {
		t.Errorf("expected HTTP status 400, got %d", err.HTTP)
	}
}

func TestWithRawCode(t *testing.T) {
	err := New("test", CodeInvalid, WithRawCode("RAW_ERR_123"))
	
	if err.RawCode != "RAW_ERR_123" {
		t.Errorf("expected raw code to be set, got %q", err.RawCode)
	}
}

func TestWithRawMessage(t *testing.T) {
	err := New("test", CodeInvalid, WithRawMessage("raw error message"))
	
	if err.RawMsg != "raw error message" {
		t.Errorf("expected raw message to be set, got %q", err.RawMsg)
	}
}

func TestWithCause(t *testing.T) {
	cause := New("original", CodeNetwork, WithMessage("network error"))
	err := New("wrapper", CodeInvalid, WithCause(cause))
	
	unwrapped := err.Unwrap()
	if unwrapped == nil {
		t.Error("expected non-nil unwrapped error")
	}
}

func TestWithCanonicalCode(t *testing.T) {
	err := New("test", CodeNotFound, WithCanonicalCode(CanonicalOrderNotFound))
	
	if err.Canonical != CanonicalOrderNotFound {
		t.Errorf("expected canonical code %s, got %s", CanonicalOrderNotFound, err.Canonical)
	}
}

func TestWithCanonicalCodeEmpty(t *testing.T) {
	err := New("test", CodeInvalid, WithCanonicalCode(CanonicalCode("")))
	
	if err.Canonical != CanonicalUnknown {
		t.Errorf("expected canonical code to be unknown for empty input, got %s", err.Canonical)
	}
}

func TestWithVenueMetadata(t *testing.T) {
	meta := map[string]string{
		"orderId": "12345",
		"symbol":  "BTC-USD",
	}
	err := New("test", CodeInvalid, WithVenueMetadata(meta))
	
	if err.VenueMetadata == nil {
		t.Fatal("expected venue metadata to be set")
	}
	if err.VenueMetadata["orderId"] != "12345" {
		t.Error("expected orderId in venue metadata")
	}
	if err.VenueMetadata["symbol"] != "BTC-USD" {
		t.Error("expected symbol in venue metadata")
	}
}

func TestWithVenueMetadataEmpty(t *testing.T) {
	err := New("test", CodeInvalid, WithVenueMetadata(nil))
	
	if err.VenueMetadata != nil {
		t.Error("expected nil venue metadata for empty input")
	}
}

func TestWithVenueField(t *testing.T) {
	err := New("test", CodeInvalid, WithVenueField("requestId", "req-123"))
	
	if err.VenueMetadata == nil {
		t.Fatal("expected venue metadata to be initialized")
	}
	if err.VenueMetadata["requestId"] != "req-123" {
		t.Errorf("expected requestId in venue metadata, got %q", err.VenueMetadata["requestId"])
	}
}

func TestWithVenueFieldEmptyKey(t *testing.T) {
	err := New("test", CodeInvalid, WithVenueField("  ", "value"))
	
	if err.VenueMetadata != nil {
		t.Error("expected nil venue metadata for empty key")
	}
}

func TestUnwrap(t *testing.T) {
	cause := New("cause", CodeNetwork, WithMessage("network error"))
	err := New("wrapper", CodeInvalid, WithCause(cause))
	
	unwrapped := err.Unwrap()
	if unwrapped != cause {
		t.Error("expected unwrapped error to match cause")
	}
}

func TestUnwrapNil(t *testing.T) {
	err := New("test", CodeInvalid)
	
	unwrapped := err.Unwrap()
	if unwrapped != nil {
		t.Error("expected nil for no cause")
	}
}

func TestNotSupported(t *testing.T) {
	err := NotSupported("feature-x not supported")
	
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Canonical != CanonicalCapabilityMissing {
		t.Errorf("expected canonical code %s, got %s", CanonicalCapabilityMissing, err.Canonical)
	}
	if err.Message == "" {
		t.Error("expected non-empty message")
	}
}
