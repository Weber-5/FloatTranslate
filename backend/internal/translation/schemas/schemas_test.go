// Package schemas tests: the embedded schema files must stay byte-identical
// with the repo-root contract copies in schemas/ whenever those are present
// (they are skipped silently in standalone builds, e.g. CI exporting only
// the backend module).
package schemas

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// findRepoSchemasDir walks upward from the package directory looking for the
// repository root (identified by its docs/ marker) and returns its schemas/
// directory, or "" when not found.
func findRepoSchemasDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Skipf("cannot determine working directory: %v", err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "schemas")
		if dirHas(t, filepath.Join(dir, "docs")) &&
			fileExists(filepath.Join(candidate, "word_translation.schema.json")) &&
			fileExists(filepath.Join(candidate, "text_translation.schema.json")) {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func dirHas(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestEmbeddedSchemasMatchRepoRootWhenPresent(t *testing.T) {
	repoSchemas := findRepoSchemasDir(t)
	if repoSchemas == "" {
		t.Skip("repo root schemas/ not found (standalone build)")
	}
	cases := []struct {
		embedded func() string
		diskName string
	}{
		{WordTranslation, "word_translation.schema.json"},
		{TextTranslation, "text_translation.schema.json"},
	}
	for _, c := range cases {
		disk, err := os.ReadFile(filepath.Join(repoSchemas, c.diskName))
		if err != nil {
			t.Fatalf("read %s: %v", c.diskName, err)
		}
		if !bytes.Equal([]byte(c.embedded()), disk) {
			t.Errorf("embedded %s differs from repo copy at %s; copy the repo schema into internal/translation/schemas/",
				c.diskName, repoSchemas)
		}
	}
}

func TestEmbeddedSchemasAreNonEmptyJSONObjects(t *testing.T) {
	for name, raw := range map[string]string{
		"word_translation.schema.json": WordTranslation(),
		"text_translation.schema.json": TextTranslation(),
		"text_chunk.schema.json":       TextChunk(),
	} {
		trimmed := strings.TrimSpace(raw)
		if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
			t.Errorf("%s is not a JSON object", name)
		}
		if !strings.Contains(raw, "$schema") {
			t.Errorf("%s missing $schema", name)
		}
	}
}
