package backtest

import (
	"errors"

	"github.com/coachpo/meltica/internal/domain/schema"
)

// mockDataFeeder is a mock implementation of the DataFeeder interface for testing.
type mockDataFeeder struct {
	count int
}

func (m *mockDataFeeder) Next() (*schema.Event, error) {
	if m.count > 0 {
		return nil, errors.New("no more data")
	}
	m.count++
	return &schema.Event{Payload: schema.TradePayload{}}, nil
}
