package unit

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/schema"
)

func TestControlMessageDecodesSubscribePayload(t *testing.T) {
	request := schema.Subscribe{
		Type:    schema.CanonicalType("TICKER"),
		Filters: map[string]any{"instrument": "BTC-USDT"},
	}
	data, err := json.Marshal(request)
	require.NoError(t, err)

	msg := schema.ControlMessage{
		MessageID:  "msg-1",
		ConsumerID: "consumer-1",
		Type:       schema.ControlMessageSubscribe,
		Payload:    data,
		Timestamp:  time.Now().UTC(),
	}

	var decoded schema.Subscribe
	require.NoError(t, msg.DecodePayload(&decoded))
	require.Equal(t, request.Type, decoded.Type)
	require.Equal(t, request.Filters, decoded.Filters)
}

func TestControlMessageDecodeRequiresPayload(t *testing.T) {
	msg := schema.ControlMessage{Type: schema.ControlMessageUnsubscribe}
	var decoded schema.Unsubscribe
	err := msg.DecodePayload(&decoded)
	require.Error(t, err)
}
