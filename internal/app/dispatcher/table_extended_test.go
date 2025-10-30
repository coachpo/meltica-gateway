package dispatcher

import (
	"testing"
	"time"

	"github.com/coachpo/meltica/internal/domain/schema"
)

// Test Table helper functions

func TestTableSetAndGetVersion(t *testing.T) {
	table := NewTable()

	// Initial version should be 0
	if version := table.Version(); version != 0 {
		t.Errorf("expected initial version 0, got %d", version)
	}

	// Set version
	table.SetVersion(5)

	if version := table.Version(); version != 5 {
		t.Errorf("expected version 5, got %d", version)
	}

	// Set another version
	table.SetVersion(10)

	if version := table.Version(); version != 10 {
		t.Errorf("expected version 10, got %d", version)
	}
}

func TestTableRemoveRoute(t *testing.T) {
	table := NewTable()

	// Add a route
	route := Route{
		Provider: "fake",
		Type:     schema.RouteTypeTrade,
		WSTopics: []string{"trade@btcusd"},
	}

	err := table.Upsert(route)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Verify it exists
	_, ok := table.Lookup("fake", schema.RouteTypeTrade)
	if !ok {
		t.Fatal("expected route to exist")
	}

	// Remove it
	table.Remove("fake", schema.RouteTypeTrade)

	// Verify it's gone
	_, ok = table.Lookup("fake", schema.RouteTypeTrade)
	if ok {
		t.Error("expected route to be removed")
	}
}

func TestTableRemoveNonExistent(t *testing.T) {
	table := NewTable()

	// Removing non-existent route should not panic
	table.Remove("fake", schema.RouteType("NON_EXISTENT"))
}

func TestValidateRestFn(t *testing.T) {
	tests := []struct {
		name    string
		fn      RestFn
		wantErr bool
	}{
		{
			name: "valid rest function",
			fn: RestFn{
				Name:     "poll_trades",
				Endpoint: "https://api.example.com/trades",
				Interval: 5 * time.Second,
				Parser:   "json",
			},
			wantErr: false,
		},
		{
			name: "empty name",
			fn: RestFn{
				Name:     "",
				Endpoint: "https://api.example.com/trades",
				Interval: 5 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "empty endpoint",
			fn: RestFn{
				Name:     "poll_trades",
				Endpoint: "",
				Interval: 5 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "zero interval",
			fn: RestFn{
				Name:     "poll_trades",
				Endpoint: "https://api.example.com/trades",
				Interval: 0,
			},
			wantErr: true,
		},
		{
			name: "negative interval",
			fn: RestFn{
				Name:     "poll_trades",
				Endpoint: "https://api.example.com/trades",
				Interval: -5 * time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRestFn(tt.fn)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRestFn() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRouteWithRestFns(t *testing.T) {
	table := NewTable()

	route := Route{
		Provider: "fake",
		Type:     schema.RouteTypeTicker,
		WSTopics: []string{"ticker@btcusd"},
		RestFns: []RestFn{
			{
				Name:     "poll_ticker",
				Endpoint: "https://api.example.com/ticker",
				Interval: 10 * time.Second,
				Parser:   "json",
			},
		},
	}

	err := table.Upsert(route)
	if err != nil {
		t.Fatalf("Upsert with RestFns failed: %v", err)
	}

	retrieved, ok := table.Lookup("fake", schema.RouteTypeTicker)
	if !ok {
		t.Fatal("expected to find route")
	}

	if len(retrieved.RestFns) != 1 {
		t.Errorf("expected 1 RestFn, got %d", len(retrieved.RestFns))
	}

	if retrieved.RestFns[0].Name != "poll_ticker" {
		t.Errorf("expected RestFn name 'poll_ticker', got %s", retrieved.RestFns[0].Name)
	}
}

func TestRouteWithInvalidRestFn(t *testing.T) {
	table := NewTable()

	route := Route{
		Provider: "fake",
		Type:     schema.RouteTypeTicker,
		WSTopics: []string{"ticker@btcusd"},
		RestFns: []RestFn{
			{
				Name:     "", // Invalid: empty name
				Endpoint: "https://api.example.com/ticker",
				Interval: 10 * time.Second,
			},
		},
	}

	err := table.Upsert(route)
	if err == nil {
		t.Error("expected error for route with invalid RestFn")
	}
}

func TestFilterRuleValidation(t *testing.T) {
	tests := []struct {
		name    string
		filter  FilterRule
		wantErr bool
	}{
		{
			name: "valid eq filter",
			filter: FilterRule{
				Field: "symbol",
				Op:    "eq",
				Value: "BTC-USD",
			},
			wantErr: false,
		},
		{
			name: "valid contains filter",
			filter: FilterRule{
				Field: "symbol",
				Op:    "contains",
				Value: "BTC",
			},
			wantErr: false,
		},
		{
			name: "empty field",
			filter: FilterRule{
				Field: "",
				Op:    "eq",
				Value: "test",
			},
			wantErr: true,
		},
		{
			name: "empty operator",
			filter: FilterRule{
				Field: "symbol",
				Op:    "",
				Value: "test",
			},
			wantErr: true,
		},
		// Note: nil value is actually allowed by FilterRule.Validate()
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("FilterRule.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRouteWithInvalidFilter(t *testing.T) {
	table := NewTable()

	route := Route{
		Provider: "fake",
		Type:     schema.RouteTypeTicker,
		WSTopics: []string{"ticker@btcusd"},
		Filters: []FilterRule{
			{
				Field: "", // Invalid: empty field
				Op:    "eq",
				Value: "BTC-USD",
			},
		},
	}

	err := table.Upsert(route)
	if err == nil {
		t.Error("expected error for route with invalid filter")
	}
}

// Note: Table.Match is tested in table_test.go

// Note: contains function is for FilterRule matching, not string slices - tested indirectly through Route.Match

// Note: resolvePath is an internal helper function tested indirectly through Match
