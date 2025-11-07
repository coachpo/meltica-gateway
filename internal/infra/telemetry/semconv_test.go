package telemetry

import "testing"

func TestEventTypeExtensionConstant(t *testing.T) {
	if EventTypeExtension != "extension" {
		t.Fatalf("expected extension event type constant to be 'extension', got %q", EventTypeExtension)
	}
}
