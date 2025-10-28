package shared

import (
	"context"
	"testing"

	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/schema"
)

type stubSubscriber struct {
	subscribed   []dispatcher.Route
	unsubscribed []dispatcher.Route
}

func (s *stubSubscriber) SubscribeRoute(route dispatcher.Route) error {
	s.subscribed = append(s.subscribed, route)
	return nil
}

func (s *stubSubscriber) UnsubscribeRoute(route dispatcher.Route) error {
	s.unsubscribed = append(s.unsubscribed, route)
	return nil
}

func TestSubscriptionManagerActivateAddsNewRoute(t *testing.T) {
	subscriber := &stubSubscriber{}
	manager := NewSubscriptionManager(subscriber)

	route := dispatcher.Route{
		Provider: "binance",
		Type:     schema.RouteTypeTrade,
		Filters: []dispatcher.FilterRule{
			{Field: "instrument", Op: "eq", Value: "BTCUSDT"},
		},
	}

	if err := manager.Activate(context.Background(), route); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}

	if len(subscriber.subscribed) != 1 {
		t.Fatalf("expected 1 subscribe call, got %d", len(subscriber.subscribed))
	}
	if !dispatcher.EqualRoutes(subscriber.subscribed[0], route) {
		t.Fatalf("expected subscribed route to equal original route")
	}

	if len(subscriber.unsubscribed) != 0 {
		t.Fatalf("expected no unsubscribe calls, got %d", len(subscriber.unsubscribed))
	}
}

func TestSubscriptionManagerActivateDiffsFilters(t *testing.T) {
	subscriber := &stubSubscriber{}
	manager := NewSubscriptionManager(subscriber)

	initial := dispatcher.Route{
		Provider: "binance",
		Type:     schema.RouteTypeTrade,
		Filters: []dispatcher.FilterRule{
			{Field: "instrument", Op: "eq", Value: "BTCUSDT"},
		},
	}

	if err := manager.Activate(context.Background(), initial); err != nil {
		t.Fatalf("initial Activate() error = %v", err)
	}

	updated := dispatcher.Route{
		Provider: "binance",
		Type:     schema.RouteTypeTrade,
		Filters: []dispatcher.FilterRule{
			{Field: "instrument", Op: "in", Value: []string{"BTCUSDT", "ETHUSDT"}},
		},
	}

	if err := manager.Activate(context.Background(), updated); err != nil {
		t.Fatalf("updated Activate() error = %v", err)
	}

	if len(subscriber.unsubscribed) != 0 {
		t.Fatalf("expected no unsubscribe calls during addition, got %d", len(subscriber.unsubscribed))
	}
	if len(subscriber.subscribed) != 2 {
		t.Fatalf("expected second subscribe call for additions, got %d total", len(subscriber.subscribed))
	}
	addition := subscriber.subscribed[1]
	if len(addition.Filters) != 1 {
		t.Fatalf("expected addition route to contain single filter, got %d", len(addition.Filters))
	}
	filter := addition.Filters[0]
	if !containsValue(filter.Value, "ETHUSDT") {
		t.Fatalf("expected addition filter to target ETHUSDT, got %v", filter.Value)
	}

	reduced := dispatcher.Route{
		Provider: "binance",
		Type:     schema.RouteTypeTrade,
		Filters: []dispatcher.FilterRule{
			{Field: "instrument", Op: "eq", Value: "ETHUSDT"},
		},
	}

	if err := manager.Activate(context.Background(), reduced); err != nil {
		t.Fatalf("reduced Activate() error = %v", err)
	}

	if len(subscriber.unsubscribed) != 1 {
		t.Fatalf("expected unsubscribe call for removed instrument, got %d", len(subscriber.unsubscribed))
	}
	removed := subscriber.unsubscribed[0]
	if len(removed.Filters) != 1 {
		t.Fatalf("expected removal route to contain single filter, got %d", len(removed.Filters))
	}
	if !containsValue(removed.Filters[0].Value, "BTCUSDT") {
		t.Fatalf("expected removed filter to target BTCUSDT, got %v", removed.Filters[0].Value)
	}

	if len(subscriber.subscribed) != 2 {
		t.Fatalf("expected no additional subscribe calls when removing, got %d", len(subscriber.subscribed))
	}
}

func TestSubscriptionManagerDeactivateRemovesRoute(t *testing.T) {
	subscriber := &stubSubscriber{}
	manager := NewSubscriptionManager(subscriber)

	route := dispatcher.Route{
		Provider: "binance",
		Type:     schema.RouteTypeTicker,
		Filters: []dispatcher.FilterRule{
			{Field: "instrument", Op: "eq", Value: "BTCUSDT"},
		},
	}

	if err := manager.Activate(context.Background(), route); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}

	if err := manager.Deactivate(context.Background(), route); err != nil {
		t.Fatalf("Deactivate() error = %v", err)
	}

	if len(subscriber.unsubscribed) != 1 {
		t.Fatalf("expected unsubscribe call on deactivate, got %d", len(subscriber.unsubscribed))
	}
	if !dispatcher.EqualRoutes(subscriber.unsubscribed[0], route) {
		t.Fatalf("expected deactivation to pass full route")
	}
}

func containsValue(value any, target string) bool {
	switch v := value.(type) {
	case string:
		return v == target
	case []string:
		for _, entry := range v {
			if entry == target {
				return true
			}
		}
	case []any:
		for _, entry := range v {
			if str, ok := entry.(string); ok && str == target {
				return true
			}
		}
	}
	return false
}
