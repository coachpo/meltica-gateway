package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLambdaManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lambda-manifest.yaml")
	yaml := `
lambdas:
  - id: test-lambda
    provider: fake
    symbol: BTC-USDT
    strategy: delay
    config: {}
    auto_start: true
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	manifest, err := LoadLambdaManifest(context.Background(), path)
	if err != nil {
		t.Fatalf("LoadLambdaManifest failed: %v", err)
	}
	if len(manifest.Lambdas) != 1 {
		t.Fatalf("expected 1 lambda, got %d", len(manifest.Lambdas))
	}
}

func TestLoadLambdaManifestValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lambda-manifest.yaml")
	yaml := `lambdas: []`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, err := LoadLambdaManifest(context.Background(), path); err == nil {
		t.Fatal("expected validation error")
	}
}
