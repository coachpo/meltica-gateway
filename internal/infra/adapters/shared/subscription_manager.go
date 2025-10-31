// Package shared provides common utilities for adapter implementations.
package shared

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	"github.com/coachpo/meltica/internal/domain/schema"
)

// RouteSubscriber defines the subset of provider capabilities required to manage subscriptions.
type RouteSubscriber interface {
	SubscribeRoute(route dispatcher.Route) error
	UnsubscribeRoute(route dispatcher.Route) error
}

// SubscriptionManager coordinates native adapter subscription updates.
type SubscriptionManager struct {
	mu         sync.Mutex
	active     map[routeKey]dispatcher.Route
	subscriber RouteSubscriber
}

// NewSubscriptionManager creates a new manager instance.
func NewSubscriptionManager(subscriber RouteSubscriber) *SubscriptionManager {
	return &SubscriptionManager{
		mu:         sync.Mutex{},
		active:     make(map[routeKey]dispatcher.Route),
		subscriber: subscriber,
	}
}

// Activate registers the given route and notifies the provider.
func (m *SubscriptionManager) Activate(ctx context.Context, route dispatcher.Route) error {
	_ = ctx
	key := makeRouteKey(route)

	m.mu.Lock()
	existing, ok := m.active[key]
	m.mu.Unlock()

	if ok && dispatcher.EqualRoutes(existing, route) {
		return nil
	}

	if !ok {
		if m.subscriber != nil {
			if err := m.subscriber.SubscribeRoute(route); err != nil {
				return fmt.Errorf("subscribe route: %w", err)
			}
		}
		m.mu.Lock()
		m.active[key] = route
		m.mu.Unlock()
		return nil
	}

	if !sameNonFilterConfig(existing, route) {
		if m.subscriber != nil {
			if err := m.subscriber.UnsubscribeRoute(existing); err != nil {
				return fmt.Errorf("unsubscribe route: %w", err)
			}
			if err := m.subscriber.SubscribeRoute(route); err != nil {
				// best-effort rollback
				_ = m.subscriber.SubscribeRoute(existing)
				return fmt.Errorf("subscribe route: %w", err)
			}
		}
		m.mu.Lock()
		m.active[key] = route
		m.mu.Unlock()
		return nil
	}

	var subscribeErr error
	if !sameNonFilterConfig(existing, route) {
		if m.subscriber != nil {
			subscribeErr = m.subscriber.SubscribeRoute(route)
		}
	} else {
		removals := buildDeltaRoute(existing, diffFilters(existing.Filters, route.Filters))
		if m.subscriber != nil && len(removals.Filters) > 0 {
			if err := m.subscriber.UnsubscribeRoute(removals); err != nil {
				return fmt.Errorf("unsubscribe route: %w", err)
			}
		}
		additions := buildDeltaRoute(route, diffFilters(route.Filters, existing.Filters))
		if m.subscriber != nil && len(additions.Filters) > 0 {
			subscribeErr = m.subscriber.SubscribeRoute(additions)
		}
	}
	if subscribeErr != nil {
		return fmt.Errorf("subscribe route: %w", subscribeErr)
	}

	// Merge route state
	merged := mergeRouteState(existing, route)

	m.mu.Lock()
	m.active[key] = merged
	m.mu.Unlock()
	return nil
}

// Deactivate removes the route from the active set and notifies the provider.
func (m *SubscriptionManager) Deactivate(ctx context.Context, route dispatcher.Route) error {
	_ = ctx
	key := makeRouteKey(route)

	m.mu.Lock()
	existing, ok := m.active[key]
	if ok {
		delete(m.active, key)
	}
	m.mu.Unlock()

	if !ok {
		return nil
	}

	if m.subscriber != nil {
		if err := m.subscriber.UnsubscribeRoute(existing); err != nil {
			return fmt.Errorf("unsubscribe route: %w", err)
		}
	}

	return nil
}

// Snapshot returns a copy of the currently active routes.
func (m *SubscriptionManager) Snapshot() []dispatcher.Route {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.active) == 0 {
		return nil
	}
	routes := make([]dispatcher.Route, 0, len(m.active))
	for _, route := range m.active {
		routes = append(routes, cloneDispatcherRoute(route))
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Provider == routes[j].Provider {
			return routes[i].Type < routes[j].Type
		}
		return routes[i].Provider < routes[j].Provider
	})
	return routes
}

func sameNonFilterConfig(a, b dispatcher.Route) bool {
	lhs := a
	rhs := b
	lhs.Filters = nil
	rhs.Filters = nil
	return dispatcher.EqualRoutes(lhs, rhs)
}

func buildDeltaRoute(base dispatcher.Route, filters []dispatcher.FilterRule) dispatcher.Route {
	if len(filters) == 0 {
		var empty dispatcher.Route
		return empty
	}
	route := &dispatcher.Route{
		Provider: base.Provider,
		Type:     base.Type,
		WSTopics: base.WSTopics,
		RestFns:  base.RestFns,
		Filters:  filters,
	}
	return *route
}

func mergeRouteState(existing, updated dispatcher.Route) dispatcher.Route {
	merged := updated
	if len(merged.WSTopics) == 0 {
		merged.WSTopics = existing.WSTopics
	}
	if len(merged.RestFns) == 0 {
		merged.RestFns = existing.RestFns
	}
	return merged
}

type routeKey struct {
	provider string
	typ      schema.RouteType
}

func makeRouteKey(route dispatcher.Route) routeKey {
	provider := strings.ToLower(strings.TrimSpace(route.Provider))
	typ := schema.NormalizeRouteType(route.Type)
	return routeKey{provider: provider, typ: typ}
}

func cloneDispatcherRoute(route dispatcher.Route) dispatcher.Route {
	cloned := route
	if len(route.WSTopics) > 0 {
		cloned.WSTopics = append([]string(nil), route.WSTopics...)
	}
	if len(route.RestFns) > 0 {
		cloned.RestFns = append([]dispatcher.RestFn(nil), route.RestFns...)
	}
	if len(route.Filters) > 0 {
		filters := make([]dispatcher.FilterRule, len(route.Filters))
		copy(filters, route.Filters)
		cloned.Filters = filters
	}
	return cloned
}

type fieldDelta struct {
	field  string
	values map[string]struct{}
}

func diffFilters(target, reference []dispatcher.FilterRule) []dispatcher.FilterRule {
	if len(target) == 0 {
		return nil
	}

	refSets := make(map[string]map[string]struct{}, len(reference))
	for _, filter := range reference {
		normField := strings.TrimSpace(strings.ToLower(filter.Field))
		if normField == "" {
			continue
		}
		set := refSets[normField]
		if set == nil {
			set = make(map[string]struct{})
			refSets[normField] = set
		}
		for _, value := range flattenFilterValues(filter.Value) {
			if value == "" {
				continue
			}
			set[value] = struct{}{}
		}
	}

	deltas := make(map[string]*fieldDelta)
	for _, filter := range target {
		normField := strings.TrimSpace(strings.ToLower(filter.Field))
		if normField == "" {
			continue
		}
		values := flattenFilterValues(filter.Value)
		if len(values) == 0 {
			continue
		}
		refSet := refSets[normField]
		for _, value := range values {
			if value == "" {
				continue
			}
			if refSet != nil {
				if _, exists := refSet[value]; exists {
					continue
				}
			}
			acc := deltas[normField]
			if acc == nil {
				acc = &fieldDelta{
					field:  strings.TrimSpace(filter.Field),
					values: make(map[string]struct{}),
				}
				deltas[normField] = acc
			}
			acc.values[value] = struct{}{}
		}
	}

	if len(deltas) == 0 {
		return nil
	}

	out := make([]dispatcher.FilterRule, 0, len(deltas))
	for _, acc := range deltas {
		list := make([]string, 0, len(acc.values))
		for value := range acc.values {
			list = append(list, value)
		}
		sort.Strings(list)
		var rule dispatcher.FilterRule
		rule.Field = acc.field
		if len(list) == 1 {
			rule.Op = "eq"
			rule.Value = list[0]
		} else {
			rule.Op = "in"
			rule.Value = list
		}
		out = append(out, rule)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Field == out[j].Field {
			return out[i].Op < out[j].Op
		}
		return out[i].Field < out[j].Field
	})

	return out
}

func flattenFilterValues(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		entry := strings.TrimSpace(v)
		if entry == "" {
			return nil
		}
		return []string{entry}
	case []string:
		out := make([]string, 0, len(v))
		for _, entry := range v {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			out = append(out, entry)
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, entry := range v {
			formatted := strings.TrimSpace(fmt.Sprint(entry))
			if formatted == "" {
				continue
			}
			out = append(out, formatted)
		}
		return out
	default:
		entry := strings.TrimSpace(fmt.Sprint(v))
		if entry == "" {
			return nil
		}
		return []string{entry}
	}
}
