// Package dispatcher provides dynamic routing capabilities for event-driven systems.
package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/coachpo/meltica/internal/schema"
)

// ProviderRouter defines the capabilities required to activate or deactivate provider routes.
type ProviderRouter interface {
	ActivateRoute(ctx context.Context, route Route) error
	DeactivateRoute(ctx context.Context, route Route) error
}

// RouteDeclaration captures a lambda's routing requirement.
type RouteDeclaration struct {
	Type    schema.RouteType
	Filters map[string]any
}

type lambdaRegistration struct {
	Providers []string
	Routes    []RouteDeclaration
}

// Registrar coordinates dynamic routing updates based on active lambdas.
type Registrar struct {
	mu      sync.Mutex
	table   *Table
	router  ProviderRouter
	lambdas map[string]lambdaRegistration

	updates      chan struct{}
	workerOnce   sync.Once
	workerCtx    context.Context
	workerCancel context.CancelFunc
	wg           sync.WaitGroup

	errMu   sync.RWMutex
	onError func(error)
}

// NewRegistrar constructs a dynamic route registrar.
func NewRegistrar(table *Table, router ProviderRouter) *Registrar {
	if table == nil {
		table = NewTable()
	}
	ctx, cancel := context.WithCancel(context.Background())

	return &Registrar{
		mu:           sync.Mutex{},
		table:        table,
		router:       router,
		lambdas:      make(map[string]lambdaRegistration),
		updates:      make(chan struct{}, 1),
		workerOnce:   sync.Once{},
		workerCtx:    ctx,
		workerCancel: cancel,
		wg:           sync.WaitGroup{},
		errMu:        sync.RWMutex{},
		onError: func(err error) {
			if err != nil {
				log.Printf("dispatcher registrar: %v", err)
			}
		},
	}
}

// RegisterLambda declares or updates the routing requirements for a lambda instance.
func (r *Registrar) RegisterLambda(ctx context.Context, lambdaID string, providers []string, routes []RouteDeclaration) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("register lambda context: %w", err)
	}
	lambdaID = strings.TrimSpace(lambdaID)
	if lambdaID == "" {
		return fmt.Errorf("lambda id required")
	}
	normalizedProviders := normalizeProviders(providers)
	if len(normalizedProviders) == 0 {
		return fmt.Errorf("providers required")
	}

	copied := make([]RouteDeclaration, len(routes))
	for i, route := range routes {
		if err := route.Type.Validate(); err != nil {
			return fmt.Errorf("lambda route[%d]: %w", i, err)
		}
		normalized := schema.NormalizeRouteType(route.Type)
		copied[i] = RouteDeclaration{
			Type:    normalized,
			Filters: cloneFilterMap(route.Filters),
		}
	}

	r.mu.Lock()
	r.lambdas[lambdaID] = lambdaRegistration{
		Providers: normalizedProviders,
		Routes:    copied,
	}
	r.mu.Unlock()

	r.scheduleRebuild()
	return nil
}

// UnregisterLambda removes routing requirements for a lambda instance.
func (r *Registrar) UnregisterLambda(ctx context.Context, lambdaID string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("unregister lambda context: %w", err)
	}
	lambdaID = strings.TrimSpace(lambdaID)
	if lambdaID == "" {
		return nil
	}
	r.mu.Lock()
	delete(r.lambdas, lambdaID)
	r.mu.Unlock()

	r.scheduleRebuild()
	return nil
}

func (r *Registrar) scheduleRebuild() {
	r.startWorker()
	select {
	case r.updates <- struct{}{}:
	default:
	}
}

func (r *Registrar) startWorker() {
	r.workerOnce.Do(func() {
		r.wg.Add(1)
		go r.run()
	})
}

func (r *Registrar) run() {
	defer r.wg.Done()

	for {
		select {
		case <-r.workerCtx.Done():
			return
		case <-r.updates:
			// coalesce bursty signals so we only rebuild once per batch.
			for {
				select {
				case <-r.updates:
					continue
				default:
				}
				break
			}
			snapshot := r.snapshot()
			if err := r.applyRoutes(r.workerCtx, snapshot); err != nil {
				r.reportError(err)
			}
		}
	}
}

func (r *Registrar) snapshot() map[string]lambdaRegistration {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.lambdas) == 0 {
		return nil
	}

	out := make(map[string]lambdaRegistration, len(r.lambdas))
	for id, reg := range r.lambdas {
		providers := append([]string(nil), reg.Providers...)
		copiedRoutes := make([]RouteDeclaration, len(reg.Routes))
		for i, route := range reg.Routes {
			copiedRoutes[i] = RouteDeclaration{
				Type:    route.Type,
				Filters: cloneFilterMap(route.Filters),
			}
		}
		out[id] = lambdaRegistration{
			Providers: providers,
			Routes:    copiedRoutes,
		}
	}
	return out
}

func (r *Registrar) applyRoutes(ctx context.Context, state map[string]lambdaRegistration) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("apply routes context: %w", err)
	}

	desired := make(map[RouteKey]Route)

	for _, reg := range state {
		if len(reg.Providers) == 0 {
			continue
		}
		for _, provider := range reg.Providers {
			for _, decl := range reg.Routes {
				if err := decl.Type.Validate(); err != nil {
					return fmt.Errorf("lambda route type: %w", err)
				}
				key := RouteKey{Provider: provider, Type: decl.Type}.normalize()
				route, ok := desired[key]
				if !ok {
					route = Route{
						Provider: provider,
						Type:     decl.Type,
						WSTopics: []string{},
						RestFns:  []RestFn{},
						Filters:  []FilterRule{},
					}
				}
				merged := mergeFilters(route.Filters, decl.Filters)
				route.Filters = merged
				desired[key] = route
			}
		}
	}

	current := r.table.Routes()

	var errs []error
	changed := false

	// Remove obsolete routes.
	for key, existing := range current {
		if _, ok := desired[key]; ok {
			continue
		}
		if r.router != nil {
			if err := r.router.DeactivateRoute(ctx, existing); err != nil {
				errs = append(errs, fmt.Errorf("deactivate %s/%s: %w", existing.Provider, existing.Type, err))
				continue
			}
		}
		r.table.Remove(existing.Provider, existing.Type)
		changed = true
	}

	// Add or update desired routes.
	for key, route := range desired {
		existing, ok := current[key]
		if ok && EqualRoutes(existing, route) {
			continue
		}
		if r.router != nil {
			if err := r.router.ActivateRoute(ctx, route); err != nil {
				errs = append(errs, fmt.Errorf("activate %s/%s: %w", route.Provider, route.Type, err))
				continue
			}
		}
		if err := r.table.Upsert(route); err != nil {
			errs = append(errs, err)
			continue
		}
		changed = true
	}

	if changed {
		r.table.NextVersion()
	}

	return errors.Join(errs...)
}

// Close terminates background workers and waits for in-flight updates to finish.
func (r *Registrar) Close() {
	r.workerCancel()
	r.wg.Wait()
}

// SetErrorHandler installs a callback invoked when asynchronous rebuilds fail.
func (r *Registrar) SetErrorHandler(handler func(error)) {
	r.errMu.Lock()
	r.onError = handler
	r.errMu.Unlock()
}

func (r *Registrar) reportError(err error) {
	if err == nil {
		return
	}
	r.errMu.RLock()
	handler := r.onError
	r.errMu.RUnlock()
	if handler != nil {
		handler(err)
	}
}

func normalizeProviders(providers []string) []string {
	if len(providers) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(providers))
	out := make([]string, 0, len(providers))
	for _, raw := range providers {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func cloneFilterMap(filters map[string]any) map[string]any {
	if len(filters) == 0 {
		return nil
	}
	out := make(map[string]any, len(filters))
	for k, v := range filters {
		out[k] = v
	}
	return out
}

func mergeFilters(existing []FilterRule, overrides map[string]any) []FilterRule {
	if len(overrides) == 0 {
		return normalizeFilters(existing)
	}

	fieldSets := make(map[string]map[string]struct{})

	accumulate := func(field string, value any) {
		field = strings.TrimSpace(field)
		if field == "" {
			return
		}
		set, ok := fieldSets[field]
		if !ok {
			set = make(map[string]struct{})
			fieldSets[field] = set
		}
		for _, entry := range flattenValues(value) {
			set[entry] = struct{}{}
		}
	}

	for _, rule := range existing {
		accumulate(rule.Field, rule.Value)
	}

	for field, value := range overrides {
		accumulate(field, value)
	}

	out := make([]FilterRule, 0, len(fieldSets))
	for field, values := range fieldSets {
		normValues := make([]string, 0, len(values))
		for value := range values {
			normValues = append(normValues, value)
		}
		sort.Strings(normValues)
		rule := FilterRule{
			Field: field,
			Op:    "",
			Value: nil,
		}
		if len(normValues) == 1 {
			rule.Op = "eq"
			rule.Value = normValues[0]
		} else {
			rule.Op = "in"
			rule.Value = normValues
		}
		out = append(out, rule)
	}

	return normalizeFilters(out)
}

func flattenValues(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return []string{strings.TrimSpace(v)}
	case []string:
		out := make([]string, 0, len(v))
		for _, entry := range v {
			out = append(out, strings.TrimSpace(entry))
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, entry := range v {
			out = append(out, strings.TrimSpace(fmt.Sprint(entry)))
		}
		return out
	default:
		return []string{strings.TrimSpace(fmt.Sprint(value))}
	}
}

func normalizeFilters(filters []FilterRule) []FilterRule {
	if len(filters) == 0 {
		return nil
	}
	out := make([]FilterRule, len(filters))
	for i, filter := range filters {
		filter.Field = strings.TrimSpace(filter.Field)
		filter.Op = strings.TrimSpace(strings.ToLower(filter.Op))
		if filter.Op == "" {
			if _, ok := filter.Value.([]string); ok {
				filter.Op = "in"
			} else {
				filter.Op = "eq"
			}
		}
		switch v := filter.Value.(type) {
		case []string:
			values := make([]string, 0, len(v))
			for _, entry := range v {
				values = append(values, strings.TrimSpace(entry))
			}
			sort.Strings(values)
			filter.Value = values
		case []any:
			values := make([]string, 0, len(v))
			for _, entry := range v {
				values = append(values, strings.TrimSpace(fmt.Sprint(entry)))
			}
			sort.Strings(values)
			filter.Value = values
			if filter.Op == "eq" {
				if len(values) == 1 {
					filter.Value = values[0]
				} else {
					filter.Op = "in"
				}
			}
		case string:
			filter.Value = strings.TrimSpace(v)
		}
		out[i] = filter
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Field == out[j].Field {
			return out[i].Op < out[j].Op
		}
		return out[i].Field < out[j].Field
	})
	return out
}
