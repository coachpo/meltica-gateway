package core

import "testing"

func TestSelectProviderDeterministic(t *testing.T) {
	cfg := Config{
		Providers: []string{"alpha", "beta", "gamma"},
		ProviderSymbols: map[string][]string{
			"alpha": []string{"BTC-USDT"},
			"beta":  []string{"BTC-USDT"},
			"gamma": []string{"BTC-USDT"},
		},
	}
	base := NewBaseLambda("lambda-select", cfg, nil, nil, nil, nil, nil, nil)

	testCases := []struct {
		seed uint64
		want string
	}{
		{seed: 0, want: "alpha"},
		{seed: 1, want: "beta"},
		{seed: 2, want: "gamma"},
		{seed: 3, want: "alpha"},
		{seed: 4, want: "beta"},
	}

	for _, tc := range testCases {
		got, err := base.SelectProvider(tc.seed)
		if err != nil {
			t.Fatalf("SelectProvider(%d) unexpected error: %v", tc.seed, err)
		}
		if got != tc.want {
			t.Fatalf("SelectProvider(%d) = %s, want %s", tc.seed, got, tc.want)
		}
	}
}
