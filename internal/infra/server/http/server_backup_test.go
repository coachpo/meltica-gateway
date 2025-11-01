package httpserver

import (
	"context"
	"testing"

	"github.com/coachpo/meltica/internal/infra/config"
)

func TestBuildProviderConfigSnapshotMasksSensitiveKeys(t *testing.T) {
	specs := []config.ProviderSpec{
		{
			Name:    "binance",
			Adapter: "binance",
			Config: map[string]any{
				"identifier": "binance",
				"api_key":    "super-secret",
				"rest": map[string]any{
					"passphrase": "top-secret",
					"timeout":    "1s",
				},
				"config": map[string]any{
					"wsSecret": "hidden",
					"depth":    100,
				},
			},
		},
	}

	snapshot := buildProviderConfigSnapshot(specs)
	entry, ok := snapshot["binance"]
	if !ok {
		t.Fatalf("expected snapshot entry for provider")
	}

	adapter, ok := entry["adapter"].(map[string]any)
	if !ok {
		t.Fatalf("expected adapter map in snapshot")
	}

	if _, found := adapter["api_key"]; found {
		t.Fatalf("expected api_key to be omitted from snapshot")
	}

	if id := adapter["identifier"]; id != "binance" {
		t.Fatalf("expected identifier to be preserved, got %v", id)
	}

	restConfig, hasRest := adapter["rest"].(map[string]any)
	if !hasRest {
		t.Fatalf("expected rest config to be present")
	}
	if _, found := restConfig["passphrase"]; found {
		t.Fatalf("expected passphrase to be omitted from rest config")
	}
	if got := restConfig["timeout"]; got != "1s" {
		t.Fatalf("expected timeout to be retained, got %v", got)
	}

	nestedConfig, hasConfig := adapter["config"].(map[string]any)
	if !hasConfig {
		t.Fatalf("expected nested config to be present")
	}
	if _, found := nestedConfig["wsSecret"]; found {
		t.Fatalf("expected wsSecret to be omitted from nested config")
	}
	if got := nestedConfig["depth"]; got != 100 {
		t.Fatalf("expected depth to be retained, got %v", got)
	}

	if _, found := specs[0].Config["api_key"]; !found {
		t.Fatalf("original provider spec config should remain unchanged")
	}
	if rest := specs[0].Config["rest"].(map[string]any); rest["passphrase"] == nil {
		t.Fatalf("original rest config should remain unchanged")
	}
}

func TestApplyBackupUpdatesRuntimeAndConfigStore(t *testing.T) {
	runtimeCfg := config.DefaultRuntimeConfig()
	runtimeStore, err := config.NewRuntimeStore(runtimeCfg)
	if err != nil {
		t.Fatalf("NewRuntimeStore failed: %v", err)
	}

	var persisted []config.AppConfig
	appStore, err := config.NewAppConfigStore(config.DefaultAppConfig(), func(cfg config.AppConfig) error {
		persisted = append(persisted, cfg)
		return nil
	})
	if err != nil {
		t.Fatalf("NewAppConfigStore failed: %v", err)
	}

	server := &httpServer{
		environment:  config.EnvDev,
		meta:         config.MetaConfig{Name: "Meltica"},
		runtimeStore: runtimeStore,
		configStore:  appStore,
	}

	payload := ConfigBackup{
		Version:     backupVersion,
		Environment: "prod",
		Meta:        config.MetaConfig{Name: "Restored"},
		Runtime:     runtimeCfg,
		Providers:   ProviderBackupSection{},
		Lambdas:     LambdaBackupSection{},
	}
	payload.Runtime.Eventbus.BufferSize += 42
	payload.Runtime.Telemetry.ServiceName = "restored"

	if err := server.applyBackup(context.Background(), payload); err != nil {
		t.Fatalf("applyBackup failed: %v", err)
	}

	snapshot := runtimeStore.Snapshot()
	if snapshot.Eventbus.BufferSize != payload.Runtime.Eventbus.BufferSize {
		t.Fatalf("expected runtime buffer to update, got %d", snapshot.Eventbus.BufferSize)
	}
	if snapshot.Telemetry.ServiceName != "restored" {
		t.Fatalf("expected telemetry service name restored, got %s", snapshot.Telemetry.ServiceName)
	}

	if len(persisted) != 1 {
		t.Fatalf("expected persisted snapshot, got %d", len(persisted))
	}
	if persisted[0].Environment != config.EnvProd {
		t.Fatalf("expected persisted environment prod, got %s", persisted[0].Environment)
	}
	if persisted[0].Runtime.Eventbus.BufferSize != payload.Runtime.Eventbus.BufferSize {
		t.Fatalf("expected persisted runtime buffer to update, got %d", persisted[0].Runtime.Eventbus.BufferSize)
	}
}
