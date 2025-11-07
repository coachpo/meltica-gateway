package main

import (
	"path/filepath"
	"testing"
)

func TestResolveConfigPathPrefersFlag(t *testing.T) {
	t.Setenv(configPathEnvVar, "config/app.ci.yaml")
	got := resolveConfigPath("custom/config.yaml")
	want := filepath.Clean("custom/config.yaml")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestResolveConfigPathFallsBackToEnv(t *testing.T) {
	t.Setenv(configPathEnvVar, "config/app.ci.yaml")
	got := resolveConfigPath("")
	want := filepath.Clean("config/app.ci.yaml")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestResolveConfigPathDefaults(t *testing.T) {
	t.Setenv(configPathEnvVar, "")
	got := resolveConfigPath("")
	want := filepath.Clean(defaultConfigPath)
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}
