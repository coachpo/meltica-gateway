package config

import (
	"reflect"
	"sync"
)

// AppConfigStore holds the canonical application configuration and persists changes via a callback.
type AppConfigStore struct {
	mu      sync.RWMutex
	cfg     AppConfig
	persist func(AppConfig) error
}

// NewAppConfigStore constructs a configuration store seeded with the supplied configuration snapshot.
func NewAppConfigStore(initial AppConfig, persist func(AppConfig) error) (*AppConfigStore, error) {
	clone := initial.Clone()
	if err := clone.normalise(); err != nil {
		return nil, err
	}
	if err := clone.Validate(); err != nil {
		return nil, err
	}
	return &AppConfigStore{mu: sync.RWMutex{}, cfg: clone, persist: persist}, nil
}

// Snapshot returns a deep copy of the current application configuration.
func (s *AppConfigStore) Snapshot() AppConfig {
	if s == nil {
		return DefaultAppConfig()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Clone()
}

// SetRuntime replaces the runtime configuration section.
func (s *AppConfigStore) SetRuntime(cfg RuntimeConfig) error {
	if s == nil {
		return nil
	}
	normalized := cfg.Clone()
	normalized.Normalise()

	s.mu.Lock()
	defer s.mu.Unlock()

	if reflect.DeepEqual(s.cfg.Runtime, normalized) {
		s.cfg.Runtime = normalized
		return nil
	}

	updated := s.cfg.Clone()
	updated.Runtime = normalized
	if err := updated.Validate(); err != nil {
		return err
	}

	if s.persist != nil {
		if err := s.persist(updated.Clone()); err != nil {
			return err
		}
	}

	s.cfg = updated
	return nil
}

// SetProviders replaces the configured provider specifications.
func (s *AppConfigStore) SetProviders(specs []ProviderSpec) error {
	if s == nil {
		return nil
	}
	providerMap := ProviderSpecsToConfigMap(specs)

	s.mu.Lock()
	defer s.mu.Unlock()

	if reflect.DeepEqual(s.cfg.Providers, providerMap) {
		s.cfg.Providers = providerMap
		return nil
	}

	updated := s.cfg.Clone()
	updated.Providers = providerMap
	if err := updated.Validate(); err != nil {
		return err
	}

	if s.persist != nil {
		if err := s.persist(updated.Clone()); err != nil {
			return err
		}
	}

	s.cfg = updated
	return nil
}

// SetLambdaManifest replaces the lambda manifest configuration.
func (s *AppConfigStore) SetLambdaManifest(manifest LambdaManifest) error {
	if s == nil {
		return nil
	}
	sanitized := manifest.Clone()
	if err := sanitized.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if reflect.DeepEqual(s.cfg.LambdaManifest, sanitized) {
		s.cfg.LambdaManifest = sanitized
		return nil
	}

	updated := s.cfg.Clone()
	updated.LambdaManifest = sanitized
	if err := updated.Validate(); err != nil {
		return err
	}

	if s.persist != nil {
		if err := s.persist(updated.Clone()); err != nil {
			return err
		}
	}

	s.cfg = updated
	return nil
}

// Replace swaps the entire application configuration snapshot.
func (s *AppConfigStore) Replace(cfg AppConfig) error {
	if s == nil {
		return nil
	}
	updated := cfg.Clone()
	if err := updated.normalise(); err != nil {
		return err
	}
	if err := updated.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if reflect.DeepEqual(s.cfg, updated) {
		s.cfg = updated
		return nil
	}

	if s.persist != nil {
		if err := s.persist(updated.Clone()); err != nil {
			return err
		}
	}

	s.cfg = updated
	return nil
}
