package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDepguardBansEncodingJSON(t *testing.T) {
	lintPath := lookupGolangciLint(t)
	tempDir := t.TempDir()
	writeGoModule(t, tempDir, `// Package main is a lint fixture.
package main

import (
    _ "encoding/json"
)

func main() {}
`)

	output, err := runLint(tempDir, lintPath)
	require.Error(t, err, "expected golangci-lint to fail for encoding/json import")
	require.Contains(t, output, "encoding/json")
	require.Contains(t, output, "Use github.com/goccy/go-json instead")
}

func TestDepguardBansGorillaWebsocket(t *testing.T) {
	lintPath := lookupGolangciLint(t)
	tempDir := t.TempDir()
	writeGoModule(t, tempDir, `// Package main is a lint fixture.
package main

import (
    _ "github.com/gorilla/websocket"
)

func main() {}
`)

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, string(out))
	}

	output, err := runLint(tempDir, lintPath)
	require.Error(t, err, "expected golangci-lint to fail for gorilla/websocket import")
	require.Contains(t, output, "github.com/gorilla/websocket")
	require.Contains(t, output, "Use github.com/coder/websocket instead")
}

func TestDepguardAllowsApprovedLibraries(t *testing.T) {
	lintPath := lookupGolangciLint(t)
	tempDir := t.TempDir()
	writeGoModule(t, tempDir, `// Package main is a lint fixture.
package main

import (
    json "github.com/goccy/go-json"
)

func main() {
    _, _ = json.Marshal(struct{}{})
}
`)

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, string(out))
	}

	output, err := runLint(tempDir, lintPath)
	if err != nil {
		t.Fatalf("expected lint success, got error: %v\n%s", err, output)
	}
}

func lookupGolangciLint(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Skip("golangci-lint not available in PATH")
	}
	return path
}

func runLint(dir, lintPath string) (string, error) {
	repoRoot := findRepoRoot()
	configPath := filepath.Join(repoRoot, ".golangci.yml")
	cmd := exec.Command(lintPath, "run", "--config", configPath, "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeGoModule(t *testing.T, dir, contents string) {
	t.Helper()
	goMod := []byte("module ci-enforcement-test\n\ngo 1.25\n")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), goMod, 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
}

func findRepoRoot() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
