package dispatcher

import (
	"fmt"
	"sort"
	"strings"
)

// EqualRoutes reports whether two routes are equivalent.
func EqualRoutes(a, b Route) bool {
	if strings.TrimSpace(strings.ToLower(a.Provider)) != strings.TrimSpace(strings.ToLower(b.Provider)) {
		return false
	}
	if a.Type != b.Type {
		return false
	}
	if !equalStringSets(a.WSTopics, b.WSTopics) {
		return false
	}
	if !equalRestFns(a.RestFns, b.RestFns) {
		return false
	}
	return equalFilters(a.Filters, b.Filters)
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	lhs := append([]string(nil), a...)
	rhs := append([]string(nil), b...)
	for i := range lhs {
		lhs[i] = strings.TrimSpace(lhs[i])
	}
	for i := range rhs {
		rhs[i] = strings.TrimSpace(rhs[i])
	}
	sort.Strings(lhs)
	sort.Strings(rhs)
	for i := range lhs {
		if lhs[i] != rhs[i] {
			return false
		}
	}
	return true
}

func equalRestFns(a, b []RestFn) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	lhs := make([]string, len(a))
	rhs := make([]string, len(b))
	for i, fn := range a {
		lhs[i] = restSignature(fn)
	}
	for i, fn := range b {
		rhs[i] = restSignature(fn)
	}
	sort.Strings(lhs)
	sort.Strings(rhs)
	for i := range lhs {
		if lhs[i] != rhs[i] {
			return false
		}
	}
	return true
}

func restSignature(fn RestFn) string {
	return fmt.Sprintf("%s|%s|%d|%s", strings.TrimSpace(fn.Name), strings.TrimSpace(fn.Endpoint), fn.Interval, strings.TrimSpace(fn.Parser))
}

func equalFilters(a, b []FilterRule) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	lhs := make([]string, len(a))
	rhs := make([]string, len(b))
	for i, filter := range a {
		lhs[i] = filterSignature(filter)
	}
	for i, filter := range b {
		rhs[i] = filterSignature(filter)
	}
	sort.Strings(lhs)
	sort.Strings(rhs)
	for i := range lhs {
		if lhs[i] != rhs[i] {
			return false
		}
	}
	return true
}

func filterSignature(filter FilterRule) string {
	values := flattenValues(filter.Value)
	sort.Strings(values)
	return fmt.Sprintf("%s|%s|%s", strings.TrimSpace(strings.ToLower(filter.Field)), strings.TrimSpace(strings.ToLower(filter.Op)), strings.Join(values, ","))
}
