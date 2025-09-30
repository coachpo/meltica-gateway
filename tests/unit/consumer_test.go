package unit

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/consumer"
	"github.com/coachpo/meltica/internal/schema"
)

func TestConsumerStartAndShutdown(t *testing.T) {
	bus := databus.NewMemoryBus(databus.MemoryConfig{BufferSize: 4})
	t.Cleanup(bus.Close)

	logger := testLogger(t)
	cons := consumer.NewConsumer("unit", bus, logger)

	ctx, cancel := context.WithCancel(context.Background())
	events, errs := cons.Start(ctx, []schema.EventType{schema.EventTypeTrade})

	// allow consumer subscription to register
	time.Sleep(10 * time.Millisecond)

	raised := &schema.Event{Provider: "binance", Symbol: "BTC-USDT", Type: schema.EventTypeTrade}
	require.NoError(t, bus.Publish(ctx, raised))

	select {
	case evt := <-events:
		require.Equal(t, raised.Symbol, evt.Symbol)
	case err := <-errs:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("consumer did not deliver event")
	}

	cancel()
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("consumer events channel not closed")
	}
	select {
	case <-errs:
	case <-time.After(time.Second):
		t.Fatal("consumer errors channel not closed")
	}
}

func testLogger(t *testing.T) *log.Logger {
	t.Helper()
	return log.New(io.Discard, "test", 0)
}
