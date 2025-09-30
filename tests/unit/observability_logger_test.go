package unit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/observability"
)

type recordingLogger struct {
	debugs int
	infos  int
	errors int
}

func (r *recordingLogger) Debug(string, ...observability.Field) { r.debugs++ }
func (r *recordingLogger) Info(string, ...observability.Field)  { r.infos++ }
func (r *recordingLogger) Error(string, ...observability.Field) { r.errors++ }

func TestSetLoggerOverridesGlobal(t *testing.T) {
	recorder := new(recordingLogger)
	observability.SetLogger(recorder)

	observability.Log().Debug("test")
	require.Equal(t, 1, recorder.debugs)

	observability.SetLogger(nil)
	observability.Log().Info("noop")
	require.Equal(t, 0, recorder.infos)
}
