// Package numeric provides helpers for decimal conversions used across services.
package numeric

import (
	"math/big"
	"strings"
)

// Format converts r into a fixed-scale decimal string rounded toward zero.
// When r is nil the empty string is returned.
func Format(r *big.Rat, scale int) string {
	if r == nil {
		return ""
	}
	pow10 := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt(pow10))
	i := new(big.Int)
	if scaled.Sign() >= 0 {
		i.Div(scaled.Num(), scaled.Denom())
	} else {
		tmp := new(big.Int).Div(new(big.Int).Abs(scaled.Num()), scaled.Denom())
		i.Neg(tmp)
	}
	s := i.String()
	if scale == 0 {
		return s
	}
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	if len(s) <= scale {
		s = strings.Repeat("0", scale-len(s)+1) + s
	}
	dot := len(s) - scale
	out := s[:dot] + "." + s[dot:]
	if neg {
		out = "-" + out
	}
	return out
}

// Parse converts a decimal string into a rational number.
// On failure, it returns (nil, false).
func Parse(s string) (*big.Rat, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok {
		return nil, false
	}
	return r, true
}

// ScaleFromStep derives the effective fractional precision from a decimal "step" string.
func ScaleFromStep(step string) int {
	step = strings.TrimSpace(step)
	if step == "" {
		return 0
	}
	idx := strings.IndexByte(step, '.')
	if idx < 0 {
		return 0
	}
	frac := strings.TrimRight(step[idx+1:], "0")
	return len(frac)
}
