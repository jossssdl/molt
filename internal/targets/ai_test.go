package targets

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOllamaOrphans(t *testing.T) {
	models := t.TempDir()
	manifest := `{"config":{"digest":"sha256:aaa"},"layers":[{"digest":"sha256:bbb"}]}`
	writeFile(t, filepath.Join(models, "manifests", "registry", "library", "m", "latest"), manifest)

	blobs := filepath.Join(models, "blobs")
	for _, n := range []string{"sha256-aaa", "sha256-bbb", "sha256-ccc", "sha256-ddd-partial", "sha256-eee-partial", "unrelated"} {
		writeFile(t, filepath.Join(blobs, n), "data")
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(blobs, "sha256-ddd-partial"), old, old); err != nil {
		t.Fatal(err)
	}

	got, err := ollamaOrphans(models, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{filepath.Join(blobs, "sha256-ccc"), filepath.Join(blobs, "sha256-ddd-partial")}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestOllamaRefusesWithoutManifests(t *testing.T) {
	models := t.TempDir()
	writeFile(t, filepath.Join(models, "blobs", "sha256-aaa"), "data")
	if got, err := ollamaOrphans(models, time.Now()); err == nil {
		t.Fatalf("want error, got orphans %v", got)
	}
}

func TestOllamaRefusesOnCorruptManifest(t *testing.T) {
	models := t.TempDir()
	writeFile(t, filepath.Join(models, "manifests", "r", "m", "latest"), "{not json")
	writeFile(t, filepath.Join(models, "blobs", "sha256-aaa"), "data")
	if got, err := ollamaOrphans(models, time.Now()); err == nil {
		t.Fatalf("want error, got orphans %v", got)
	}
}

func TestOllamaMissingStoreIsEmpty(t *testing.T) {
	got, err := ollamaOrphans(t.TempDir(), time.Now())
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}
