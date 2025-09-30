package unit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/observability"
)

func TestDeadLetterQueueOfferAndDrain(t *testing.T) {
	queue := observability.NewDeadLetterQueue(2)

	queue.Offer(observability.TelemetryEvent{EventID: "1"})
	queue.Offer(observability.TelemetryEvent{EventID: "2"})
	queue.Offer(observability.TelemetryEvent{EventID: "3"})

	require.Equal(t, 2, queue.Len())

	events := queue.Drain()
	require.Len(t, events, 2)
	require.Equal(t, "2", events[0].EventID)
	require.Equal(t, "3", events[1].EventID)
	require.Equal(t, 0, queue.Len())
}
