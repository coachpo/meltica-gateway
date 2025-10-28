package binance

import (
	"testing"

	json "github.com/goccy/go-json"
)

func TestTradeMessageDecodesTimestamps(t *testing.T) {
	payload := []byte(`{"e":"trade","E":1672515782136,"s":"BTCUSDT","t":12345,"p":"0.001","q":"100","T":1672515783136,"m":true}`)
	var msg tradeMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.EventTime.Int64() != 1672515782136 {
		t.Fatalf("unexpected event time: %d", msg.EventTime.Int64())
	}
	if msg.TradeTime.Int64() != 1672515783136 {
		t.Fatalf("unexpected trade time: %d", msg.TradeTime.Int64())
	}
}
