package config

import (
	"context"
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	ctx := context.Background()

	// Load without YAML file (should use defaults)
	cfg, err := Load(ctx, "/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify defaults
	if cfg.Environment != EnvProd {
		t.Errorf("expected environment %s, got %s", EnvProd, cfg.Environment)
	}

	if cfg.Eventbus.BufferSize != 1024 {
		t.Errorf("expected eventbus buffer size 1024, got %d", cfg.Eventbus.BufferSize)
	}

	if cfg.Telemetry.ServiceName != "meltica-gateway" {
		t.Errorf("expected service name meltica-gateway, got %s", cfg.Telemetry.ServiceName)
	}

	if cfg.ManifestPath != "config/runtime.yaml" {
		t.Errorf("expected default manifest path config/runtime.yaml, got %s", cfg.ManifestPath)
	}
}

func TestLoad_WithEnv(t *testing.T) {
	ctx := context.Background()

	// Set environment variables
	os.Setenv("MELTICA_ENV", "dev")
	os.Setenv("OTEL_SERVICE_NAME", "test-service")
	os.Setenv("MELTICA_MANIFEST", "/tmp/runtime.yaml")
	defer func() {
		os.Unsetenv("MELTICA_ENV")
		os.Unsetenv("OTEL_SERVICE_NAME")
		os.Unsetenv("MELTICA_MANIFEST")
	}()

	cfg, err := Load(ctx, "/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify env overrides
	if cfg.Environment != EnvDev {
		t.Errorf("expected environment dev, got %s", cfg.Environment)
	}

	if cfg.Telemetry.ServiceName != "test-service" {
		t.Errorf("expected service name from env, got %s", cfg.Telemetry.ServiceName)
	}

	if cfg.ManifestPath != "/tmp/runtime.yaml" {
		t.Errorf("expected manifest path from env, got %s", cfg.ManifestPath)
	}
}

func TestDefaultAppConfig(t *testing.T) {
	cfg := defaultAppConfig()

	if cfg.Environment != EnvProd {
		t.Errorf("expected prod environment, got %s", cfg.Environment)
	}

	if cfg.Dispatcher.Routes == nil {
		t.Error("expected initialized routes map")
	}

	if cfg.Telemetry.EnableMetrics != true {
		t.Error("expected metrics enabled by default")
	}

	if cfg.ManifestPath != "config/runtime.yaml" {
		t.Errorf("expected default manifest path, got %s", cfg.ManifestPath)
	}
}
