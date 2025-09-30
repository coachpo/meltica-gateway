package unit

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/config"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/schema"
)

func TestStreamOrderingReordersSequentialEvents(t *testing.T) {
	clock := newStubClock(time.UnixMilli(1700000000000))
	ordering := dispatcher.NewStreamOrdering(config.StreamOrderingConfig{
		LatenessTolerance: 150 * time.Millisecond,
		FlushInterval:     50 * time.Millisecond,
		MaxBufferSize:     8,
	}, clock.Now)

	seq2 := newEvent("binance", "BTC-USDT", schema.EventTypeTrade, 2)
	out, buffered := ordering.OnEvent(seq2)
	require.True(t, buffered)
	require.Empty(t, out)

	seq1 := newEvent("binance", "BTC-USDT", schema.EventTypeTrade, 1)
	out, buffered = ordering.OnEvent(seq1)
	require.True(t, buffered)
	require.Len(t, out, 2)
	require.Equal(t, uint64(1), out[0].SeqProvider)
	require.Equal(t, uint64(2), out[1].SeqProvider)
}

func TestStreamOrderingFlushesOnLateness(t *testing.T) {
	start := time.UnixMilli(1700000005000)
	clock := newStubClock(start)
	ordering := dispatcher.NewStreamOrdering(config.StreamOrderingConfig{
		LatenessTolerance: 100 * time.Millisecond,
		FlushInterval:     50 * time.Millisecond,
		MaxBufferSize:     4,
	}, clock.Now)

	late := newEvent("binance", "ETH-USDT", schema.EventTypeBookUpdate, 10)
	_, buffered := ordering.OnEvent(late)
	require.True(t, buffered)

	// Advance time beyond lateness tolerance to force flush.
	clock.Advance(150 * time.Millisecond)
	flush := ordering.Flush(clock.Now())
	require.Len(t, flush, 1)
	require.Equal(t, uint64(10), flush[0].SeqProvider)
}

func newEvent(provider, symbol string, typ schema.EventType, seq uint64) *schema.Event {
	return &schema.Event{
		EventID:     provider + ":" + symbol + ":" + string(typ) + ":" + fmt.Sprintf("%d", seq),
		Provider:    provider,
		Symbol:      symbol,
		Type:        typ,
		SeqProvider: seq,
		IngestTS:    time.UnixMilli(0),
		EmitTS:      time.UnixMilli(0),
		Payload:     map[string]any{"seq": seq},
	}
}
