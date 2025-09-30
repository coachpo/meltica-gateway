package unit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/observability"
)

func TestInMemoryTelemetryBusPublishSubscribe(t *testing.T) {
	bus := observability.NewInMemoryTelemetryBus(1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	ch, err := bus.Subscribe(ctx)
	require.NoError(t, err)

	event := observability.TelemetryEvent{EventID: "evt-1", Metadata: map[string]any{"k": "v"}}
	require.NoError(t, bus.Publish(ctx, event))

	select {
	case got := <-ch:
		require.Equal(t, event.EventID, got.EventID)
		event.Metadata["k"] = "changed"
		require.Equal(t, "v", got.Metadata["k"])
	case <-ctx.Done():
		t.Fatal("did not receive telemetry event")
	}

	bus.Close()
	require.NoError(t, bus.Publish(ctx, event))
}
