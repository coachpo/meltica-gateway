package unit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

func TestWsFrameResetZeroesFields(t *testing.T) {
	frame := &schema.WsFrame{
		Provider:    "binance",
		ConnID:      "conn-1",
		ReceivedAt:  time.Now().UnixNano(),
		MessageType: 1,
		Data:        []byte("payload"),
	}
	frame.SetReturned(true)

	frame.Reset()

	require.Equal(t, "", frame.Provider)
	require.Equal(t, "", frame.ConnID)
	require.Equal(t, int64(0), frame.ReceivedAt)
	require.Equal(t, 0, frame.MessageType)
	require.Nil(t, frame.Data)
	require.False(t, frame.IsReturned())
}

func TestProviderRawResetZeroesFields(t *testing.T) {
	raw := &schema.ProviderRaw{
		Provider:   "binance",
		StreamName: "btcusdt@depth",
		ReceivedAt: 12345,
		Payload:    []byte(`{"e":"depth"}`),
	}
	raw.SetReturned(true)

	raw.Reset()

	require.Equal(t, "", raw.Provider)
	require.Equal(t, "", raw.StreamName)
	require.Equal(t, int64(0), raw.ReceivedAt)
	require.Nil(t, raw.Payload)
	require.False(t, raw.IsReturned())
}

func TestCanonicalEventResetZeroesFields(t *testing.T) {
	mergeID := "merge"
	evt := &schema.CanonicalEvent{
		EventID:        "binance:BTC-USDT:Trade:99",
		MergeID:        &mergeID,
		RoutingVersion: 3,
		Provider:       "binance",
		Symbol:         "BTC-USDT",
		Type:           schema.EventTypeTrade,
		SeqProvider:    99,
		IngestTS:       time.UnixMilli(123),
		EmitTS:         time.UnixMilli(456),
		Payload:        map[string]any{"key": "value"},
		TraceID:        "trace",
	}
	evt.SetReturned(true)

	evt.Reset()

	require.Equal(t, "", evt.EventID)
	require.Nil(t, evt.MergeID)
	require.Equal(t, 0, evt.RoutingVersion)
	require.Equal(t, "", evt.Provider)
	require.Equal(t, "", evt.Symbol)
	require.Equal(t, schema.EventType(""), evt.Type)
	require.Equal(t, uint64(0), evt.SeqProvider)
	require.True(t, evt.IngestTS.IsZero())
	require.True(t, evt.EmitTS.IsZero())
	require.Nil(t, evt.Payload)
	require.Equal(t, "", evt.TraceID)
	require.False(t, evt.IsReturned())
}

func TestMergedEventResetZeroesFields(t *testing.T) {
	merged := &schema.MergedEvent{
		MergeID:     "merge",
		Symbol:      "BTC-USDT",
		EventType:   schema.EventTypeBookUpdate,
		WindowOpen:  111,
		WindowClose: 222,
		Fragments:   []schema.CanonicalEvent{{Provider: "binance"}},
		IsComplete:  true,
		TraceID:     "trace",
	}
	merged.SetReturned(true)

	merged.Reset()

	require.Equal(t, "", merged.MergeID)
	require.Equal(t, "", merged.Symbol)
	require.Equal(t, schema.EventType(""), merged.EventType)
	require.Equal(t, int64(0), merged.WindowOpen)
	require.Equal(t, int64(0), merged.WindowClose)
	require.Nil(t, merged.Fragments)
	require.False(t, merged.IsComplete)
	require.Equal(t, "", merged.TraceID)
	require.False(t, merged.IsReturned())
}

func TestOrderRequestResetZeroesFields(t *testing.T) {
	price := "100.00"
	req := &schema.OrderRequest{
		ClientOrderID: "order-1",
		ConsumerID:    "consumer",
		Provider:      "binance",
		Symbol:        "BTC-USDT",
		Side:          schema.TradeSideBuy,
		OrderType:     schema.OrderTypeLimit,
		Price:         &price,
		Quantity:      "1.5",
		Timestamp:     time.UnixMilli(1234),
	}
	req.SetReturned(true)

	req.Reset()

	require.Equal(t, "", req.ClientOrderID)
	require.Equal(t, "", req.ConsumerID)
	require.Equal(t, "", req.Provider)
	require.Equal(t, "", req.Symbol)
	require.Equal(t, schema.TradeSide(""), req.Side)
	require.Equal(t, schema.OrderType(""), req.OrderType)
	require.Nil(t, req.Price)
	require.Equal(t, "", req.Quantity)
	require.True(t, req.Timestamp.IsZero())
	require.False(t, req.IsReturned())
}

func TestExecReportResetZeroesFields(t *testing.T) {
	report := &schema.ExecReport{
		ClientOrderID:   "order-1",
		ExchangeOrderID: "ex-1",
		Provider:        "binance",
		Symbol:          "BTC-USDT",
		Status:          schema.ExecReportStateFILLED,
		FilledQty:       "1.5",
		RemainingQty:    "0",
		AvgPrice:        "100.00",
		TransactTime:    123456,
		ReceivedAt:      123789,
		TraceID:         "trace",
		DecisionID:      "decision",
	}
	report.SetReturned(true)

	report.Reset()

	require.Equal(t, "", report.ClientOrderID)
	require.Equal(t, "", report.ExchangeOrderID)
	require.Equal(t, "", report.Provider)
	require.Equal(t, "", report.Symbol)
	require.Equal(t, schema.ExecReportState(""), report.Status)
	require.Equal(t, "", report.FilledQty)
	require.Equal(t, "", report.RemainingQty)
	require.Equal(t, "", report.AvgPrice)
	require.Equal(t, int64(0), report.TransactTime)
	require.Equal(t, int64(0), report.ReceivedAt)
	require.Equal(t, "", report.TraceID)
	require.Equal(t, "", report.DecisionID)
	require.False(t, report.IsReturned())
}

func TestBoundedPoolDoublePutPanicsWithStack(t *testing.T) {
	bounded := pool.NewBoundedPool("CanonicalEvent", 1, func() interface{} {
		return &schema.CanonicalEvent{}
	})

	obj, err := bounded.Get(context.Background())
	require.NoError(t, err)

	event, ok := obj.(*schema.CanonicalEvent)
	require.True(t, ok)

	bounded.Put(event)

	defer func() {
		r := recover()
		require.NotNil(t, r)
		msg := r.(string)
		require.Contains(t, msg, "double-Put() detected")
		require.Contains(t, msg, "pool CanonicalEvent")
		require.Contains(t, msg, "pool_lifecycle_test.go")
		require.Contains(t, msg, "pool/lifecycle.go")
		require.Contains(t, msg, "\n")
	}()

	bounded.Put(event)
}

func TestBoundedPoolPreventsReturnedObjectsFromPut(t *testing.T) {
	bounded := pool.NewBoundedPool("ExecReport", 1, func() interface{} {
		return &schema.ExecReport{}
	})

	obj, err := bounded.Get(context.Background())
	require.NoError(t, err)
	report, ok := obj.(*schema.ExecReport)
	require.True(t, ok)
	report.SetReturned(true)

	defer func() {
		recovered := recover()
		require.NotNil(t, recovered)
		msg := fmt.Sprint(recovered)
		require.Contains(t, msg, "double-Put() detected")
		require.Contains(t, msg, "pool ExecReport")
	}()

	bounded.Put(report)
}

func TestBoundedPoolGetTimeoutAfter100ms(t *testing.T) {
	bounded := pool.NewBoundedPool("WsFrame", 1, func() interface{} {
		return &schema.WsFrame{}
	})

	obj, err := bounded.Get(context.Background())
	require.NoError(t, err)

	frame := obj.(*schema.WsFrame)

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	next, err := bounded.Get(timeoutCtx)
	duration := time.Since(start)

	require.Nil(t, next)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.GreaterOrEqual(t, duration, 90*time.Millisecond)
	require.Less(t, duration, 200*time.Millisecond)

	bounded.Put(frame)
}
