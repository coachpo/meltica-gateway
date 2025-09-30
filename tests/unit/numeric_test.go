package unit

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/numeric"
)

func TestNumericFormatParseAndScale(t *testing.T) {
	rat := big.NewRat(12345, 100)
	formatted := numeric.Format(rat, 2)
	require.Equal(t, "123.45", formatted)

	round := big.NewRat(-12345, 100)
	require.Equal(t, "-123.45", numeric.Format(round, 2))

	parsed, ok := numeric.Parse(formatted)
	require.True(t, ok)
	require.True(t, new(big.Rat).Sub(parsed, rat).Sign() == 0)

	scale := numeric.ScaleFromStep("0.0010")
	require.Equal(t, 3, scale)
}
