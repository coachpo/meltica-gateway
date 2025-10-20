// Package factories registers built-in provider implementations.
package factories

import (
	"github.com/coachpo/meltica/internal/adapters/fake"
	"github.com/coachpo/meltica/internal/provider"
)

// Register installs all built-in provider factories into the supplied registry.
func Register(reg *provider.Registry) {
	if reg == nil {
		return
	}

	// Register the synthetic provider used for development and testing.
	fake.RegisterFactory(reg)
}
