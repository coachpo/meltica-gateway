package backtest

import "github.com/coachpo/meltica/internal/domain/schema"

// DataFeeder is an interface for feeding historical market data to the backtest engine.
type DataFeeder interface {
	Next() (*schema.Event, error)
}
