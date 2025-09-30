package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/schema"
)

func TestRoutingTableUpsertLookupAndRemove(t *testing.T) {
	table := dispatcher.NewTable()
	route := dispatcher.Route{
		Type:     schema.CanonicalType("TRADE"),
		WSTopics: []string{"btcusdt@aggTrade"},
		RestFns: []dispatcher.RestFn{{
			Name:     "snapshot",
			Endpoint: "/api/v3/depth",
			Interval: time.Minute,
			Parser:   "orderbook",
		}},
	}

	require.NoError(t, table.Upsert(route))

	stored, ok := table.Lookup(route.Type)
	require.True(t, ok)
	require.Equal(t, route.Type, stored.Type)
	require.ElementsMatch(t, route.WSTopics, stored.WSTopics)

	table.SetVersion(42)
	require.Equal(t, int64(42), table.Version())

	table.Remove(route.Type)
	_, ok = table.Lookup(route.Type)
	require.False(t, ok)
}

func TestRoutingTableRoutesReturnsCopy(t *testing.T) {
	table := dispatcher.NewTable()
	initial := dispatcher.Route{Type: schema.CanonicalType("TICKER")}
	require.NoError(t, table.Upsert(initial))

	copy := table.Routes()
	require.Len(t, copy, 1)
	copy[initial.Type] = dispatcher.Route{Type: schema.CanonicalType("CHANGED")}

	stored, ok := table.Lookup(initial.Type)
	require.True(t, ok)
	require.Equal(t, initial.Type, stored.Type)
}
