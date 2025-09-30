package integration

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoGorillaWebsocketImports(t *testing.T) {
	assertNoImport(t, []string{"internal", "cmd"}, "github.com/gorilla/websocket")
}

func TestNoEncodingJSONImportsInProduction(t *testing.T) {
	excluded := map[string]struct{}{
		"tests":                 {},
		"specs":                 {},
		"docs":                  {},
		"lib":                   {},
		"internal/adapter/mock": {},
	}
	validate := func(path string) bool {
		for prefix := range excluded {
			if strings.HasPrefix(path, prefix) {
				return false
			}
		}
		return true
	}
	assertNoImportWithFilter(t, []string{"internal", "cmd"}, "encoding/json", validate)
}

func TestNoCompatibilityCodeMarkers(t *testing.T) {
	patterns := []string{"compat", "legacy", "deprecated", "USE_OLD"}
	walkFiles(t, []string{"internal", "cmd"}, func(path string, data string) {
		for _, pattern := range patterns {
			if strings.Contains(strings.ToLower(data), pattern) {
				t.Fatalf("found banned pattern %q in %s", pattern, path)
			}
		}
	})
}

func TestConsumerDoesNotReturnClonesToPool(t *testing.T) {
	walkFiles(t, []string{"internal/consumer"}, func(path string, data string) {
		if strings.Contains(data, ".Put(") {
			if strings.Contains(data, "pool.Put") || strings.Contains(data, "PoolManager") {
				t.Fatalf("consumer code must not call Put() on clones: %s", path)
			}
		}
	})
}

func assertNoImport(t *testing.T, roots []string, importPath string) {
	assertNoImportWithFilter(t, roots, importPath, func(string) bool { return true })
}

func assertNoImportWithFilter(t *testing.T, roots []string, importPath string, include func(string) bool) {
	walkFiles(t, roots, func(path string, data string) {
		if !strings.HasSuffix(path, ".go") {
			return
		}
		if !include(path) {
			return
		}
		if strings.Contains(data, "\""+importPath+"\"") {
			t.Fatalf("unexpected import %s in %s", importPath, path)
		}
	})
}

func walkFiles(t *testing.T, roots []string, fn func(path string, data string)) {
	t.Helper()
	rootDir := findRepoRoot()
	for _, root := range roots {
		fullRoot := filepath.Join(rootDir, root)
		err := filepath.WalkDir(fullRoot, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			relPath, relErr := filepath.Rel(rootDir, path)
			if relErr != nil {
				relPath = path
			}
			fn(relPath, string(data))
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}
