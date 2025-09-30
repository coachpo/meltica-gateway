package unit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/adapters/binance"
	"github.com/coachpo/meltica/internal/schema"
)

func TestParserConvertsDepthUpdateToCanonicalEvent(t *testing.T) {
	parser := binance.NewParser()
	ingest := time.UnixMilli(1700000000000).UTC()

	frame := map[string]any{
		"stream": "btcusdt@depth@100ms",
		"data": map[string]any{
			"e": "depthUpdate",
			"E": ingest.UnixMilli(),
			"s": "BTCUSDT",
			"u": float64(42),
			"b": [][]string{{"50000.10", "1.5"}},
			"a": [][]string{{"50001.00", "2.0"}},
		},
	}
	payload, err := json.Marshal(frame)
	require.NoError(t, err)

	events, err := parser.Parse(context.Background(), payload, ingest)
	require.NoError(t, err)
	require.Len(t, events, 1)

	evt := events[0]
	require.NotNil(t, evt)
	require.Equal(t, "binance:BTC-USDT:BookUpdate:42", evt.EventID)
	require.Equal(t, schema.EventTypeBookUpdate, evt.Type)
	require.Equal(t, uint64(42), evt.SeqProvider)
	require.Equal(t, "binance", evt.Provider)
	require.Equal(t, "BTC-USDT", evt.Symbol)
	require.True(t, evt.IngestTS.Equal(ingest))
	require.IsType(t, schema.BookUpdatePayload{}, evt.Payload)
	book := evt.Payload.(schema.BookUpdatePayload)
	require.Equal(t, schema.BookUpdateTypeDelta, book.UpdateType)
	require.Equal(t, "50000.10", book.Bids[0].Price)
	require.Equal(t, "1.5", book.Bids[0].Quantity)
}

func TestParserConvertsAggTradeToTradeEvent(t *testing.T) {
	parser := binance.NewParser()
	ingest := time.UnixMilli(1700000005000).UTC()

	frame := map[string]any{
		"stream": "btcusdt@aggTrade",
		"data": map[string]any{
			"e": "aggTrade",
			"E": ingest.UnixMilli(),
			"s": "BTCUSDT",
			"t": float64(1234567),
			"p": "50010.45",
			"q": "0.01",
			"m": true,
		},
	}
	payload, err := json.Marshal(frame)
	require.NoError(t, err)

	events, err := parser.Parse(context.Background(), payload, ingest)
	require.NoError(t, err)
	require.Len(t, events, 1)

	evt := events[0]
	require.NotNil(t, evt)
	require.Equal(t, schema.EventTypeTrade, evt.Type)
	require.Equal(t, "BTC-USDT", evt.Symbol)
	require.Equal(t, uint64(1234567), evt.SeqProvider)
	require.IsType(t, schema.TradePayload{}, evt.Payload)
	trade := evt.Payload.(schema.TradePayload)
	require.Equal(t, "50010.45", trade.Price)
	require.Equal(t, "0.01", trade.Quantity)
	require.Equal(t, schema.TradeSideSell, trade.Side)
}

func TestParserConvertsSnapshotToCanonicalEvent(t *testing.T) {
	parser := binance.NewParser()
	ingest := time.UnixMilli(1700000010000).UTC()

	body := map[string]any{
		"lastUpdateId": 555,
		"bids":         [][]string{{"50000.00", "1.0"}},
		"asks":         [][]string{{"50005.00", "0.8"}},
		"s":            "BTCUSDT",
	}
	encoded, err := json.Marshal(body)
	require.NoError(t, err)

	events, err := parser.ParseSnapshot(context.Background(), "orderbook", encoded, ingest)
	require.NoError(t, err)
	require.Len(t, events, 1)

	evt := events[0]
	require.NotNil(t, evt)
	require.Equal(t, schema.EventTypeBookSnapshot, evt.Type)
	require.Equal(t, "binance", evt.Provider)
	require.Equal(t, "BTC-USDT", evt.Symbol)
	require.IsType(t, schema.BookSnapshotPayload{}, evt.Payload)
	snap := evt.Payload.(schema.BookSnapshotPayload)
	require.Len(t, snap.Bids, 1)
	require.Equal(t, "50000.00", snap.Bids[0].Price)
	require.Equal(t, "1.0", snap.Bids[0].Quantity)
}
