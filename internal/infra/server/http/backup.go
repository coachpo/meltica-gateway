package httpserver

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/app/lambda/runtime"
	"github.com/coachpo/meltica/internal/app/provider"
	"github.com/coachpo/meltica/internal/infra/config"
)

const backupVersion = "2"

// ConfigBackup encapsulates the complete application snapshot for export and restore.
type ConfigBackup struct {
	Version     string                `json:"version"`
	GeneratedAt time.Time             `json:"generatedAt"`
	Environment string                `json:"environment"`
	Meta        config.MetaConfig     `json:"meta"`
	Runtime     config.RuntimeConfig  `json:"runtime"`
	Providers   ProviderBackupSection `json:"providers"`
	Lambdas     LambdaBackupSection   `json:"lambdas"`
}

// ProviderBackupSection captures provider specifications and runtime metadata.
type ProviderBackupSection struct {
	Config  map[config.Provider]map[string]any `json:"config"`
	Runtime []provider.RuntimeMetadata         `json:"runtime"`
}

// LambdaBackupSection captures lambda manifest definitions and instance state.
type LambdaBackupSection struct {
	Manifest  config.LambdaManifest      `json:"manifest"`
	Instances []runtime.InstanceSnapshot `json:"instances"`
}

func buildBackupPayload(server *httpServer) (ConfigBackup, error) {
	if server == nil {
		return ConfigBackup{}, fmt.Errorf("http server required")
	}
	if server.runtimeStore == nil {
		return ConfigBackup{}, fmt.Errorf("runtime store unavailable")
	}

	runtimeSnapshot := server.runtimeStore.Snapshot()

	providerSpecs := map[config.Provider]map[string]any(nil)
	providerRuntime := []provider.RuntimeMetadata(nil)
	if server.providers != nil {
		specs := server.providers.ProviderSpecsSnapshot()
		sanitized := buildProviderConfigSnapshot(specs)
		if len(sanitized) > 0 {
			converted := make(map[config.Provider]map[string]any, len(sanitized))
			for name, entry := range sanitized {
				converted[config.Provider(name)] = entry
			}
			providerSpecs = converted
		}
		providerRuntime = server.providers.ProviderMetadataSnapshot()
	}

	manifest := config.LambdaManifest{Lambdas: nil}
	instances := []runtime.InstanceSnapshot(nil)
	if server.manager != nil {
		manifest = server.manager.ManifestSnapshot()
		instances = server.manager.InstanceSnapshots()
	}

	payload := ConfigBackup{
		Version:     backupVersion,
		GeneratedAt: time.Now().UTC(),
		Environment: string(server.environment),
		Meta:        server.meta,
		Runtime:     runtimeSnapshot,
		Providers: ProviderBackupSection{
			Config:  providerSpecs,
			Runtime: providerRuntime,
		},
		Lambdas: LambdaBackupSection{
			Manifest:  manifest,
			Instances: instances,
		},
	}
	return payload, nil
}

func (s *httpServer) applyBackup(ctx context.Context, payload ConfigBackup) error {
	if s.runtimeStore == nil {
		return fmt.Errorf("runtime store unavailable")
	}

	runtimeCfg := payload.Runtime.Clone()
	runtimeCfg.Normalise()
	if err := runtimeCfg.Validate(); err != nil {
		return fmt.Errorf("runtime: %w", err)
	}

	providerSpecs := []config.ProviderSpec(nil)
	if len(payload.Providers.Config) > 0 {
		specs, err := config.BuildProviderSpecs(payload.Providers.Config)
		if err != nil {
			return fmt.Errorf("providers: %w", err)
		}
		providerSpecs = specs
	}

	manifest := payload.Lambdas.Manifest.Clone()
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("lambdas: %w", err)
	}

	runtimeUpdated, err := s.runtimeStore.Replace(runtimeCfg)
	if err != nil {
		return fmt.Errorf("apply runtime: %w", err)
	}
	if s.manager != nil {
		if err := s.manager.ApplyRuntimeConfig(runtimeUpdated); err != nil {
			return fmt.Errorf("sync runtime: %w", err)
		}
	}

	if s.providers != nil {
		if err := restoreProviders(ctx, s.providers, providerSpecs, payload.Providers.Runtime); err != nil {
			return fmt.Errorf("providers: %w", err)
		}
	} else if len(providerSpecs) > 0 {
		return fmt.Errorf("provider manager unavailable")
	}

	if s.manager != nil {
		runningMap := lambdaRunningState(payload.Lambdas.Instances)
		if err := s.manager.Restore(ctx, manifest, runningMap); err != nil {
			return fmt.Errorf("lambdas: %w", err)
		}
		if err := s.manager.ApplyRuntimeConfig(runtimeUpdated); err != nil {
			return fmt.Errorf("sync lambda risk: %w", err)
		}
	}

	desiredAppCfg := config.AppConfig{
		Environment:    config.Environment(strings.TrimSpace(payload.Environment)),
		Meta:           payload.Meta,
		Runtime:        runtimeUpdated,
		Providers:      config.ProviderSpecsToConfigMap(providerSpecs),
		LambdaManifest: manifest,
	}
	if s.configStore != nil {
		if err := s.configStore.Replace(desiredAppCfg); err != nil {
			return fmt.Errorf("persist app config: %w", err)
		}
	}
	return nil
}

func restoreProviders(ctx context.Context, manager *provider.Manager, specs []config.ProviderSpec, runtimeMeta []provider.RuntimeMetadata) error {
	if manager == nil {
		return nil
	}
	desiredRunning := make(map[string]bool, len(runtimeMeta))
	for _, meta := range runtimeMeta {
		name := strings.ToLower(strings.TrimSpace(meta.Name))
		if name == "" {
			continue
		}
		desiredRunning[name] = meta.Running
	}

	currentSpecs := manager.ProviderSpecsSnapshot()
	current := make(map[string]config.ProviderSpec, len(currentSpecs))
	for _, spec := range currentSpecs {
		current[strings.ToLower(spec.Name)] = spec
	}

	desired := make(map[string]config.ProviderSpec, len(specs))
	for _, spec := range specs {
		desired[strings.ToLower(spec.Name)] = spec
	}

	for name, spec := range current {
		if _, ok := desired[name]; ok {
			continue
		}
		if err := manager.Remove(spec.Name); err != nil && !errors.Is(err, provider.ErrProviderNotFound) {
			return fmt.Errorf("remove provider %s: %w", spec.Name, err)
		}
	}

	for _, spec := range specs {
		key := strings.ToLower(spec.Name)
		if existing, ok := current[key]; ok {
			if !providerSpecsEqual(existing, spec) {
				if _, err := manager.Update(ctx, spec, false); err != nil {
					return fmt.Errorf("update provider %s: %w", spec.Name, err)
				}
			}
		} else {
			if _, err := manager.Create(ctx, spec, false); err != nil {
				return fmt.Errorf("create provider %s: %w", spec.Name, err)
			}
		}
	}

	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}
		normalized := strings.ToLower(name)
		shouldRun := desiredRunning[normalized]
		if shouldRun {
			if _, err := manager.StartProvider(ctx, name); err != nil && !errors.Is(err, provider.ErrProviderRunning) {
				return fmt.Errorf("start provider %s: %w", name, err)
			}
		} else {
			if _, ok := desiredRunning[normalized]; ok {
				if _, err := manager.StopProvider(name); err != nil {
					if !errors.Is(err, provider.ErrProviderNotRunning) && !errors.Is(err, provider.ErrProviderNotFound) {
						return fmt.Errorf("stop provider %s: %w", name, err)
					}
				}
			}
		}
	}
	return nil
}

func lambdaRunningState(instances []runtime.InstanceSnapshot) map[string]bool {
	if len(instances) == 0 {
		return nil
	}
	out := make(map[string]bool, len(instances))
	for _, snapshot := range instances {
		out[strings.ToLower(strings.TrimSpace(snapshot.ID))] = snapshot.Running
	}
	return out
}

func providerSpecsEqual(a, b config.ProviderSpec) bool {
	if !strings.EqualFold(a.Name, b.Name) {
		return false
	}
	if !strings.EqualFold(a.Adapter, b.Adapter) {
		return false
	}
	return reflect.DeepEqual(a.Config, b.Config)
}
