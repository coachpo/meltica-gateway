package shared

import (
	"context"
	"fmt"
	"sync"

	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/schema"
)

// RouteSubscriber defines the subset of provider capabilities required to manage subscriptions.
type RouteSubscriber interface {
	SubscribeRoute(route dispatcher.Route) error
	UnsubscribeRoute(typ schema.CanonicalType) error
}

// SubscriptionManager coordinates native adapter subscription updates.
type SubscriptionManager struct {
	mu         sync.Mutex
	active     map[schema.CanonicalType]dispatcher.Route
	subscriber RouteSubscriber
}

// NewSubscriptionManager creates a new manager instance.
func NewSubscriptionManager(subscriber RouteSubscriber) *SubscriptionManager {
	manager := new(SubscriptionManager)
	manager.active = make(map[schema.CanonicalType]dispatcher.Route)
	manager.subscriber = subscriber
	return manager
}

// Activate registers the given route and notifies the provider.
func (m *SubscriptionManager) Activate(ctx context.Context, route dispatcher.Route) error {
	_ = ctx
	if m.subscriber != nil {
		if err := m.subscriber.SubscribeRoute(route); err != nil {
			return fmt.Errorf("subscribe route: %w", err)
		}
	}
	m.mu.Lock()
	m.active[route.Type] = route
	m.mu.Unlock()
	return nil
}

// Deactivate removes the route from the active set and notifies the provider.
func (m *SubscriptionManager) Deactivate(ctx context.Context, typ schema.CanonicalType) error {
	_ = ctx
	if m.subscriber != nil {
		if err := m.subscriber.UnsubscribeRoute(typ); err != nil {
			return fmt.Errorf("unsubscribe route: %w", err)
		}
	}
	m.mu.Lock()
	delete(m.active, typ)
	m.mu.Unlock()
	return nil
}

// ActiveRoutes exposes currently active dispatcher routes.
func (m *SubscriptionManager) ActiveRoutes() map[schema.CanonicalType]dispatcher.Route {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[schema.CanonicalType]dispatcher.Route, len(m.active))
	for k, v := range m.active {
		out[k] = v
	}
	return out
}
