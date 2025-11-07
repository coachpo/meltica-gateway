package migrations

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDirSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db", "migrations")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir temp migrations: %v", err)
	}

	resolved, err := resolveDir(path)
	if err != nil {
		t.Fatalf("resolveDir returned error: %v", err)
	}
	if !filepath.IsAbs(resolved) {
		t.Fatalf("expected absolute path, got %s", resolved)
	}
	if resolved != filepath.Clean(resolved) {
		t.Fatalf("expected clean path, got %s", resolved)
	}
}

func TestResolveDirMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing")
	_, err := resolveDir(path)
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist, got %v", err)
	}
}

func TestResolveDirFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_, err := resolveDir(path)
	if err == nil {
		t.Fatal("expected error for file path")
	}
	if !errors.Is(err, errNotDirectory) {
		t.Fatalf("expected errNotDirectory, got %v", err)
	}
}

func TestFileURLUnixAndWindows(t *testing.T) {
	cases := []string{
		"/tmp/migrations",
		"/Users/example/project/db/migrations",
		"C:/tmp/migrations",
	}
	for _, path := range cases {
		got := fileURL(path)
		if !strings.HasPrefix(got, "file://") {
			t.Fatalf("expected file:// prefix for %s, got %s", path, got)
		}
		if len(got) <= len("file://") {
			t.Fatalf("expected path data in file url for %s, got %s", path, got)
		}
	}
}

func TestApplyValidatesPathBeforeConnecting(t *testing.T) {
	ctx := context.Background()
	err := Apply(ctx, "postgresql://invalid", "does-not-exist", nil)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected missing directory error, got %v", err)
	}
}

func TestRollbackValidatesPathBeforeConnecting(t *testing.T) {
	ctx := context.Background()
	err := Rollback(ctx, "postgresql://invalid", "still-missing", 1, nil)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected missing directory error, got %v", err)
	}
}
