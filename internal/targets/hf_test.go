package targets

import (
	"path/filepath"
	"sort"
	"testing"
)

func TestHuggingFaceEntriesAreModelsAndDatasetsOnly(t *testing.T) {
	hub := t.TempDir()
	for _, d := range []string{"models--meta-llama--Llama-3.2-3B", "models--gpt2", "datasets--org--ds", ".locks", "spaces--a--b"} {
		writeFile(t, filepath.Join(hub, d, "refs", "main"), "x")
	}
	writeFile(t, filepath.Join(hub, "version.txt"), "1")
	writeFile(t, filepath.Join(hub, "models--not-a-dir"), "file, not a directory")

	got, err := hfEntries([]string{hub, hub, filepath.Join(hub, "missing")})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got {
		names = append(names, filepath.Base(p))
	}
	sort.Strings(names)
	want := []string{"datasets--org--ds", "models--gpt2", "models--meta-llama--Llama-3.2-3B"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v, want %v", names, want)
		}
	}
}

func TestHuggingFaceLabels(t *testing.T) {
	cases := map[string]string{
		"/c/hub/models--meta-llama--Llama-3.2-3B": "HF Model: meta-llama/Llama-3.2-3B",
		"/c/hub/models--gpt2":                     "HF Model: gpt2",
		"/c/hub/datasets--org--ds":                "HF Dataset: org/ds",
		"/c/hub/other":                            "",
	}
	for p, want := range cases {
		if got := hfLabel(filepath.FromSlash(p)); got != want {
			t.Errorf("hfLabel(%s) = %q, want %q", p, got, want)
		}
	}
	l, ok := HuggingFace().(Labeler)
	if !ok || l.Label("/x/models--a--b") != "HF Model: a/b" {
		t.Error("HuggingFace target must implement Labeler")
	}
	if o, ok := OllamaOrphans().(Labeler); !ok || o.Label("/x/blobs/sha256-abc") != "Ollama orphan blob: sha256-abc" {
		t.Error("OllamaOrphans must implement Labeler")
	}
}
