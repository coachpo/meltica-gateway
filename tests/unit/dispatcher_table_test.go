package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/schema"
)

func TestTableUpsertLookupAndRoutes(t *testing.T) {
	table := dispatcher.NewTable()

	route := dispatcher.Route{
		Type:     schema.CanonicalType("TRADE"),
		WSTopics: []string{"trade"},
		RestFns: []dispatcher.RestFn{{
			Name:     "snapshot",
			Endpoint: "/snapshot",
			Interval: time.Second,
		}},
		Filters: []dispatcher.FilterRule{{Field: "symbol", Op: "eq", Value: "BTC-USDT"}},
	}

	require.NoError(t, table.Upsert(route))

	lookup, ok := table.Lookup(schema.CanonicalType("TRADE"))
	require.True(t, ok)
	require.Equal(t, "trade", lookup.WSTopics[0])

	all := table.Routes()
	require.Len(t, all, 1)
	require.Equal(t, route.Type, all[route.Type].Type)

	table.SetVersion(3)
	require.Equal(t, int64(3), table.Version())

	table.Remove(route.Type)
	_, ok = table.Lookup(route.Type)
	require.False(t, ok)
}

func TestTableRejectsInvalidRestFn(t *testing.T) {
	table := dispatcher.NewTable()
	route := dispatcher.Route{Type: schema.CanonicalType("TRADE"), RestFns: []dispatcher.RestFn{{}}}
	require.Error(t, table.Upsert(route))
}

func TestFilterRuleMatchingModes(t *testing.T) {
	ruleEq := dispatcher.FilterRule{Field: "payload.symbol", Op: "eq", Value: "BTC-USDT"}
	ruleNeq := dispatcher.FilterRule{Field: "payload.symbol", Op: "neq", Value: "ETH-USDT"}
	ruleIn := dispatcher.FilterRule{Field: "payload.side", Op: "in", Value: []any{"BUY", "SELL"}}
	rulePrefix := dispatcher.FilterRule{Field: "payload.symbol", Op: "prefix", Value: "BTC"}

	raw := schema.RawInstance{
		"payload": map[string]any{
			"symbol": "BTC-USDT",
			"side":   "BUY",
		},
	}

	require.True(t, ruleEq.Match(raw))
	require.True(t, ruleNeq.Match(raw))
	require.True(t, ruleIn.Match(raw))
	require.True(t, rulePrefix.Match(raw))
}

func TestRouteMatchEvaluatesFilters(t *testing.T) {
	route := dispatcher.Route{Filters: []dispatcher.FilterRule{{Field: "payload.price", Op: "eq", Value: "100"}}}
	matching := schema.RawInstance{"payload": map[string]any{"price": "100"}}
	nonMatching := schema.RawInstance{"payload": map[string]any{"price": "200"}}

	require.True(t, route.Match(matching))
	require.False(t, route.Match(nonMatching))
}

func TestFilterContainsVariants(t *testing.T) {
	rule := dispatcher.FilterRule{Field: "payload.status", Op: "in", Value: map[string]any{"ACTIVE": true}}
	raw := schema.RawInstance{"payload": map[string]any{"status": "ACTIVE"}}
	require.True(t, rule.Match(raw))

	rule.Value = []string{"PENDING", "SETTLED"}
	require.False(t, rule.Match(raw))

	rule.Value = "SYMBOLS:BTC-USDT,ETH-USDT"
	require.True(t, rule.Match(schema.RawInstance{"payload": map[string]any{"status": "btc-usdt"}}))
}
