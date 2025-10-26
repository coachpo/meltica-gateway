package backtest

import (
	"encoding/csv"
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
		if err == io.EOF {
			return nil, err
		}
		return nil, fmt.Errorf("read csv record: %w", err)
	}

	timestamp, err := strconv.ParseInt(record[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse timestamp: %w", err)
	}

	return &schema.Event{
		Payload: schema.TradePayload{
			Timestamp: time.Unix(0, timestamp),
			Price:     record[1],
			Quantity:  record[2],
		},
	}, nil
}
