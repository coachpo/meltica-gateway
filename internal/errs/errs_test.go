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
