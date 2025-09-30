package errs

import (
	"errors"
	"strings"
	"testing"
)

func TestErrorFormattingIncludesCanonicalAndVenue(t *testing.T) {
	err := New(
		"binance",
		CodeInvalid,
		WithHTTP(400),
		WithMessage("invalid order payload"),
		WithRawCode("-2013"),
		WithRawMessage("order does not exist"),
		WithCanonicalCode(CanonicalOrderNotFound),
		WithVenueMetadata(map[string]string{
			"symbol":   "BTCUSDT",
			"endpoint": "/api/v3/order",
		}),
		WithVenueField("request_id", "req-123"),
		WithRemediation("verify order id before retrying"),
		WithCause(errors.New("binance http 400")),
	)

	out := err.Error()
	if !strings.Contains(out, "exchange=binance") {
		t.Fatalf("expected exchange marker in error string: %s", out)
	}
	if !strings.Contains(out, "code=invalid_request") {
		t.Fatalf("expected canonical code in error string: %s", out)
	}
	if !strings.Contains(out, "canonical=order_not_found") {
		t.Fatalf("expected canonical classification in error string: %s", out)
	}
	expectedVenue := "venue=endpoint=\"/api/v3/order\",request_id=\"req-123\",symbol=\"BTCUSDT\""
	if !strings.Contains(out, expectedVenue) {
		t.Fatalf("expected venue metadata %q in error string: %s", expectedVenue, out)
	}
	if !strings.Contains(out, "remediation=\"verify order id before retrying\"") {
		t.Fatalf("expected remediation guidance in error string: %s", out)
	}
	if !strings.Contains(out, "cause=\"binance http 400\"") {
		t.Fatalf("expected wrapped cause in error string: %s", out)
	}
}

func TestWithCanonicalCodeEmptyDefaultsToUnknown(t *testing.T) {
	err := New("binance", CodeInvalid, WithCanonicalCode("   "))
	if err.Canonical != CanonicalUnknown {
		t.Fatalf("expected canonical code to default to unknown, got %q", err.Canonical)
	}
	if strings.Contains(err.Error(), "canonical=") {
		t.Fatalf("canonical marker should be omitted when code is unknown: %s", err.Error())
	}
}

func TestWithVenueMetadataMerge(t *testing.T) {
	err := New(
		"binance",
		CodeExchange,
		WithVenueMetadata(map[string]string{"symbol": "BTCUSDT"}),
		WithVenueMetadata(map[string]string{"symbol": "ETHUSDT", "endpoint": "/api"}),
	)

	if got := err.VenueMetadata["symbol"]; got != "ETHUSDT" {
		t.Fatalf("expected latest metadata to win, got %q", got)
	}
	if got := err.VenueMetadata["endpoint"]; got != "/api" {
		t.Fatalf("expected endpoint metadata to be present, got %q", got)
	}
}

func TestNilErrorString(t *testing.T) {
	var e *E
	if got := e.Error(); got != "<nil>" {
		t.Fatalf("expected <nil> string for nil error, got %q", got)
	}
}
