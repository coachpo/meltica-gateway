package backtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/coachpo/meltica/internal/domain/schema"
)

func TestCSVFeeder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")
	csvData := `timestamp,price,quantity
1672531200000000000,20000,1
1672531201000000000,20001,2
`
	if err := os.WriteFile(path, []byte(csvData), 0o600); err != nil {
		t.Fatalf("write temp csv: %v", err)
	}

	feeder, err := NewCSVFeeder(path)
	if err != nil {
		t.Fatalf("NewCSVFeeder failed: %v", err)
	}

	// First event
	event1, err := feeder.Next()
	if err != nil {
		t.Fatalf("Next() failed: %v", err)
	}
	payload1, ok := event1.Payload.(schema.TradePayload)
	if !ok {
		t.Fatalf("unexpected payload type")
	}
	if payload1.Timestamp.UnixNano() != 1672531200000000000 {
		t.Errorf("unexpected timestamp: %d", payload1.Timestamp.UnixNano())
	}

	// Second event
	event2, err := feeder.Next()
	if err != nil {
		t.Fatalf("Next() failed: %v", err)
	}
	payload2, ok := event2.Payload.(schema.TradePayload)
	if !ok {
		t.Fatalf("unexpected payload type")
	}
	if payload2.Timestamp.UnixNano() != 1672531201000000000 {
		t.Errorf("unexpected timestamp: %d", payload2.Timestamp.UnixNano())
	}

	// End of file
	_, err = feeder.Next()
	if err == nil {
		t.Fatal("expected EOF, but got nil")
	}
}
