package dispatcher

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/schema"
)

func TestWriteErrorProducesJSONResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeError(recorder, 418, "teapot")

	resp := recorder.Result()
	require.Equal(t, 418, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	body := recorder.Body.String()
	require.Contains(t, body, "teapot")
}

func TestWriteAckConflictOnFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeAck(recorder, schema.ControlAcknowledgement{Success: false})

	resp := recorder.Result()
	require.Equal(t, 409, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
}
