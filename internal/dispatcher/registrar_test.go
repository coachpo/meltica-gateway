package dispatcher

import (
	"context"
	"testing"

	"github.com/coachpo/meltica/internal/schema"
)

func TestRegistrarRegisterLambdaMultipleProviders(t *testing.T) {
	ctx := context.Background()
	table := NewTable()
	registrar := NewRegistrar(table, nil)

	providers := []string{"alpha", "beta"}
	routes := []RouteDeclaration{{
		Type: schema.RouteTypeTrade,
		Filters: map[string]any{
			"instrument": "BTC-USDT",
		},
	}}

	if err := registrar.RegisterLambda(ctx, "lambda-multi", providers, routes); err != nil {
		t.Fatalf("register lambda: %v", err)
	}

	registered := table.Routes()
	if len(registered) != len(providers) {
		t.Fatalf("expected %d routes, got %d", len(providers), len(registered))
	}

	for _, provider := range providers {
		key := RouteKey{Provider: provider, Type: schema.RouteTypeTrade}.normalize()
		route, ok := registered[key]
		if !ok {
			t.Fatalf("expected route for provider %s", provider)
		}
		if route.Provider != provider {
			t.Fatalf("route provider mismatch: got %s want %s", route.Provider, provider)
		}
		if len(route.Filters) == 0 {
			t.Fatalf("expected filters for provider %s", provider)
		}
	}
}
