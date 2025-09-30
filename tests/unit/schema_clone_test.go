package unit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/schema"
)

func TestCloneEventDeepCopiesComplexPayload(t *testing.T) {
	payload := map[string]any{
		"bytes": []byte("hello"),
		"nested": map[string]any{
			"list": []any{[]byte("a"), map[string]any{"value": 1}},
		},
	}
	mergeID := "merge-1"
	original := &schema.Event{Payload: payload, MergeID: &mergeID}
	clone := schema.CloneEvent(original)
	require.NotSame(t, original, clone)

	clonedPayload := clone.Payload.(map[string]any)
	require.NotSame(t, &payload, &clonedPayload)
	nestedOriginal := payload["nested"].(map[string]any)
	nestedClone := clonedPayload["nested"].(map[string]any)
	require.NotSame(t, &nestedOriginal, &nestedClone)

	bytesOrig := payload["bytes"].([]byte)
	bytesClone := clonedPayload["bytes"].([]byte)
	bytesOrig[0] = 'H'
	require.Equal(t, byte('h'), bytesClone[0])

	require.NotSame(t, original.MergeID, clone.MergeID)
}
