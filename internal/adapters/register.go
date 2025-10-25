// Package adapters wires built-in adapters into the provider registry.
package adapters

import (
	"github.com/coachpo/meltica/internal/adapters/fake"
	"github.com/coachpo/meltica/internal/provider"
)

// RegisterAll installs every built-in adapter into the provided registry.
func RegisterAll(reg *provider.Registry) {
	if reg == nil {
		return
	}
	fake.RegisterFactory(reg)
}
