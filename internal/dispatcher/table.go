package dispatcher

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/errs"
	"github.com/coachpo/meltica/internal/schema"
)

// Route captures dispatcher routing metadata for a canonical type.
type Route struct {
	Type     schema.CanonicalType
	WSTopics []string
	RestFns  []RestFn
	Filters  []FilterRule
}

// RestFn configures a REST polling routine used by the dispatcher.
type RestFn struct {
	Name     string
	Endpoint string
	Interval time.Duration
	Parser   string
}

// FilterRule defines a predicate applied to raw instances before canonicalisation.
type FilterRule struct {
	Field string
	Op    string
	Value any
}

// Table stores canonical routes keyed by type.
type Table struct {
	mu      sync.RWMutex
	routes  map[schema.CanonicalType]Route
	version atomic.Int64
}

// NewTable constructs an empty dispatch table.
func NewTable() *Table {
	table := new(Table)
	table.routes = make(map[schema.CanonicalType]Route)
	return table
}

// Upsert inserts or replaces the provided route.
func (t *Table) Upsert(route Route) error {
	if err := route.Type.Validate(); err != nil {
		return fmt.Errorf("validate route type: %w", err)
	}
	for _, fn := range route.RestFns {
		if err := validateRestFn(fn); err != nil {
			return err
		}
	}
	for i, filter := range route.Filters {
		if err := filter.Validate(); err != nil {
			return errs.New("dispatcher/route", errs.CodeInvalid, errs.WithMessage(fmt.Sprintf("filter[%d]: %v", i, err)))
		}
	}

	t.mu.Lock()
	t.routes[route.Type] = route
	t.mu.Unlock()
	return nil
}

// Remove deletes the route if present.
func (t *Table) Remove(typ schema.CanonicalType) {
	t.mu.Lock()
	delete(t.routes, typ)
	t.mu.Unlock()
}

// Lookup returns the route if present.
func (t *Table) Lookup(typ schema.CanonicalType) (Route, bool) {
	t.mu.RLock()
	route, ok := t.routes[typ]
	t.mu.RUnlock()
	return route, ok
}

// Routes returns a shallow copy of all routes.
func (t *Table) Routes() map[schema.CanonicalType]Route {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[schema.CanonicalType]Route, len(t.routes))
	for k, v := range t.routes {
		out[k] = v
	}
	return out
}

// SetVersion updates the routing table version.
func (t *Table) SetVersion(version int64) {
	t.version.Store(version)
}

// Version returns the current routing table version.
func (t *Table) Version() int64 {
	return t.version.Load()
}

// Match reports whether the route accepts the provided raw instance.
func (r Route) Match(raw schema.RawInstance) bool {
	for _, filter := range r.Filters {
		if !filter.Match(raw) {
			return false
		}
	}
	return true
}

// Validate ensures the rule definition is well-formed.
func (rule FilterRule) Validate() error {
	if strings.TrimSpace(rule.Field) == "" {
		return fmt.Errorf("field required")
	}
	if strings.TrimSpace(rule.Op) == "" {
		return fmt.Errorf("operator required")
	}
	return nil
}

// Match evaluates the rule against a raw instance.
func (rule FilterRule) Match(raw schema.RawInstance) bool {
	path := strings.Split(rule.Field, ".")
	value, ok := resolvePath(path, raw)
	if !ok {
		return false
	}
	switch strings.ToLower(rule.Op) {
	case "eq":
		return compareEqual(value, rule.Value)
	case "neq":
		return !compareEqual(value, rule.Value)
	case "in":
		return contains(value, rule.Value)
	case "prefix":
		return hasPrefix(value, rule.Value)
	default:
		return false
	}
}

func validateRestFn(fn RestFn) error {
	if strings.TrimSpace(fn.Name) == "" {
		return errs.New("dispatcher/rest", errs.CodeInvalid, errs.WithMessage("name required"))
	}
	if strings.TrimSpace(fn.Endpoint) == "" {
		return errs.New("dispatcher/rest", errs.CodeInvalid, errs.WithMessage("endpoint required"))
	}
	if fn.Interval <= 0 {
		return errs.New("dispatcher/rest", errs.CodeInvalid, errs.WithMessage("interval must be >0"))
	}
	return nil
}

func resolvePath(path []string, raw any) (any, bool) {
	if len(path) == 0 {
		return raw, true
	}
	var current map[string]any
	switch v := raw.(type) {
	case map[string]any:
		current = v
	case schema.RawInstance:
		current = map[string]any(v)
	default:
		return nil, false
	}
	value, ok := current[path[0]]
	if !ok {
		return nil, false
	}
	return resolvePath(path[1:], value)
}

func compareEqual(lhs any, rhs any) bool {
	return fmt.Sprint(lhs) == fmt.Sprint(rhs)
}

func hasPrefix(lhs any, rhs any) bool {
	needle := fmt.Sprint(rhs)
	return strings.HasPrefix(strings.ToUpper(fmt.Sprint(lhs)), strings.ToUpper(needle))
}

func contains(value any, set any) bool {
	switch s := set.(type) {
	case []any:
		for _, item := range s {
			if compareEqual(value, item) {
				return true
			}
		}
	case []string:
		lhs := fmt.Sprint(value)
		for _, item := range s {
			if lhs == item {
				return true
			}
		}
	case string:
		return strings.Contains(strings.ToUpper(s), strings.ToUpper(fmt.Sprint(value)))
	case map[string]any:
		_, ok := s[fmt.Sprint(value)]
		return ok
	}
	return false
}
