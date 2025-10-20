package dispatcher

import (
	"testing"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

func TestNewTable(t *testing.T) {
	table := NewTable()

	if table == nil {
		t.Fatal("expected non-nil table")
	}

	routes := table.Routes()
	if len(routes) != 0 {
		t.Error("expected empty table")
	}
}

func TestTableUpsert(t *testing.T) {
	table := NewTable()

	route := Route{
		Provider: "fake",
		Type:     "TICKER",
		WSTopics: []string{"ticker@btcusd"},
		Filters:  []FilterRule{{Field: "instrument", Op: "eq", Value: "BTC-USD"}},
	}

	err := table.Upsert(route)
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	retrieved, ok := table.Lookup("fake", "TICKER")
	if !ok {
		t.Fatal("expected to find route")
	}

	if retrieved.Type != route.Type {
		t.Errorf("expected type %s, got %s", route.Type, retrieved.Type)
	}
}

func TestTableUpsertInvalidType(t *testing.T) {
	table := NewTable()

	route := Route{
		Provider: "fake",
		Type:     "", // Invalid
	}

	err := table.Upsert(route)
	if err == nil {
		t.Error("expected error for invalid type")
	}
}

func TestTableRemove(t *testing.T) {
	table := NewTable()

	route := Route{
		Provider: "fake",
		Type:     "TICKER",
	}

	_ = table.Upsert(route)

	table.Remove("fake", "TICKER")

	_, ok := table.Lookup("fake", "TICKER")
	if ok {
		t.Error("expected route to be removed")
	}
}

func TestTableLookup(t *testing.T) {
	table := NewTable()

	_, ok := table.Lookup("fake", "NONEXISTENT")
	if ok {
		t.Error("expected lookup to fail for nonexistent route")
	}
}

func TestTableVersion(t *testing.T) {
	table := NewTable()

	if table.Version() != 0 {
		t.Error("expected initial version to be 0")
	}

	table.SetVersion(5)

	if table.Version() != 5 {
		t.Errorf("expected version 5, got %d", table.Version())
	}
}

func TestTableRoutes(t *testing.T) {
	table := NewTable()

	route1 := Route{Provider: "fake", Type: "TICKER"}
	route2 := Route{Provider: "binance", Type: "TRADE"}

	_ = table.Upsert(route1)
	_ = table.Upsert(route2)

	routes := table.Routes()

	if len(routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(routes))
	}
}

func TestRouteMatch(t *testing.T) {
	route := Route{
		Type: "TICKER",
		Filters: []FilterRule{
			{Field: "instrument", Op: "eq", Value: "BTC-USD"},
		},
	}

	raw := schema.RawInstance{
		"instrument": "BTC-USD",
	}

	if !route.Match(raw) {
		t.Error("expected route to match")
	}

	raw["instrument"] = "ETH-USD"
	if route.Match(raw) {
		t.Error("expected route not to match")
	}
}

func TestFilterRuleValidate(t *testing.T) {
	tests := []struct {
		name    string
		rule    FilterRule
		wantErr bool
	}{
		{
			name:    "valid rule",
			rule:    FilterRule{Field: "instrument", Op: "eq", Value: "BTC-USD"},
			wantErr: false,
		},
		{
			name:    "empty field",
			rule:    FilterRule{Field: "", Op: "eq", Value: "BTC-USD"},
			wantErr: true,
		},
		{
			name:    "empty operator",
			rule:    FilterRule{Field: "instrument", Op: "", Value: "BTC-USD"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFilterRuleMatch(t *testing.T) {
	tests := []struct {
		name  string
		rule  FilterRule
		raw   schema.RawInstance
		match bool
	}{
		{
			name:  "eq match",
			rule:  FilterRule{Field: "instrument", Op: "eq", Value: "BTC-USD"},
			raw:   schema.RawInstance{"instrument": "BTC-USD"},
			match: true,
		},
		{
			name:  "eq no match",
			rule:  FilterRule{Field: "instrument", Op: "eq", Value: "BTC-USD"},
			raw:   schema.RawInstance{"instrument": "ETH-USD"},
			match: false,
		},
		{
			name:  "neq match",
			rule:  FilterRule{Field: "instrument", Op: "neq", Value: "BTC-USD"},
			raw:   schema.RawInstance{"instrument": "ETH-USD"},
			match: true,
		},
		{
			name:  "in match",
			rule:  FilterRule{Field: "instrument", Op: "in", Value: []string{"BTC-USD", "ETH-USD"}},
			raw:   schema.RawInstance{"instrument": "BTC-USD"},
			match: true,
		},
		{
			name:  "prefix match",
			rule:  FilterRule{Field: "instrument", Op: "prefix", Value: "BTC"},
			raw:   schema.RawInstance{"instrument": "BTC-USD"},
			match: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rule.Match(tt.raw); got != tt.match {
				t.Errorf("Match() = %v, want %v", got, tt.match)
			}
		})
	}
}

func TestRestFnValidation(t *testing.T) {
	table := NewTable()

	route := Route{
		Type: "TICKER",
		RestFns: []RestFn{
			{
				Name:     "",
				Endpoint: "https://api.example.com/ticker",
				Interval: time.Second,
			},
		},
	}

	err := table.Upsert(route)
	if err == nil {
		t.Error("expected error for empty REST function name")
	}

	route.RestFns[0].Name = "ticker"
	route.RestFns[0].Endpoint = ""
	err = table.Upsert(route)
	if err == nil {
		t.Error("expected error for empty endpoint")
	}

	route.RestFns[0].Endpoint = "https://api.example.com/ticker"
	route.RestFns[0].Interval = 0
	err = table.Upsert(route)
	if err == nil {
		t.Error("expected error for zero interval")
	}
}
