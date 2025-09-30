package unit

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/pool"
)

func TestJSONEncoderEncodeAndRelease(t *testing.T) {
	enc := pool.AcquireJSONEncoder()
	data, err := enc.Encode(map[string]any{"v": 1})
	require.NoError(t, err)
	require.Equal(t, `{"v":1}`, string(data))

	buf := new(bytes.Buffer)
	require.NoError(t, enc.WriteTo(buf, map[string]any{"v": 2}))
	require.Equal(t, `{"v":2}`, buf.String())

	pool.ReleaseJSONEncoder(enc)
}
