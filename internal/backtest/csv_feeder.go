package backtest

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

// CSVFeeder reads historical market data from a CSV file.
type CSVFeeder struct {
	reader *csv.Reader
}

// NewCSVFeeder creates a new CSV data feeder.
func NewCSVFeeder(filePath string) (*CSVFeeder, error) {
	// #nosec G304 -- file path is operator provided via CLI flags.
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open csv file: %w", err)
	}

	reader := csv.NewReader(file)
	// Read the header row.
	_, err = reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read csv header: %w", err)
	}

	return &CSVFeeder{
		reader: reader,
	}, nil
}

// Next returns the next event from the CSV file.
func (f *CSVFeeder) Next() (*schema.Event, error) {
	record, err := f.reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("read csv record: %w", err)
	}

	timestamp, err := strconv.ParseInt(record[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse timestamp: %w", err)
	}

	symbol := ""
	if len(record) > 3 {
		symbol = record[3]
	}

	return &schema.Event{
		EventID:        "",
		RoutingVersion: 0,
		Provider:       "",
		Symbol:         symbol,
		Type:           schema.EventTypeTrade,
		SeqProvider:    0,
		IngestTS:       time.Unix(0, timestamp),
		EmitTS:         time.Unix(0, timestamp),
		Payload: schema.TradePayload{
			TradeID:   "",
			Side:      schema.TradeSideBuy,
			Price:     record[1],
			Quantity:  record[2],
			Timestamp: time.Unix(0, timestamp),
		},
	}, nil
}
