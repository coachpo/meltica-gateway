package unit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/bus/controlbus"
	"github.com/coachpo/meltica/internal/schema"
)

func TestControlBusSendAndConsume(t *testing.T) {
	bus := controlbus.NewMemoryBus(controlbus.MemoryConfig{BufferSize: 2})
	t.Cleanup(bus.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	messages, err := bus.Consume(ctx)
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		for msg := range messages {
			ack := schema.ControlAcknowledgement{MessageID: msg.Command.MessageID, Success: true}
			msg.Reply <- ack
			close(done)
		}
	}()

	cmd := schema.ControlMessage{
		MessageID:  "1",
		ConsumerID: "tester",
		Type:       schema.ControlMessageSubscribe,
		Payload:    []byte(`{"type":"TRADE"}`),
		Timestamp:  time.Now().UTC(),
	}

	ack, err := bus.Send(ctx, cmd)
	require.NoError(t, err)
	require.True(t, ack.Success)

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("did not process control message")
	}
}
