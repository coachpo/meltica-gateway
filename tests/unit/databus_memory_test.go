package unit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/schema"
)

func TestMemoryBusPublishAndUnsubscribe(t *testing.T) {
	bus := databus.NewMemoryBus(databus.MemoryConfig{BufferSize: 1})
	t.Cleanup(bus.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	id, ch, err := bus.Subscribe(ctx, schema.EventTypeTrade)
	require.NoError(t, err)

	evt := &schema.Event{Provider: "binance", Symbol: "BTC-USDT", Type: schema.EventTypeTrade}
	require.NoError(t, bus.Publish(ctx, evt))

	select {
	case clone := <-ch:
		require.NotNil(t, clone)
		require.Equal(t, evt.Symbol, clone.Symbol)
		require.NotSame(t, evt, clone)
	case <-ctx.Done():
		t.Fatal("event not delivered")
	}

	bus.Unsubscribe(id)
	require.NoError(t, bus.Publish(ctx, evt))
}
