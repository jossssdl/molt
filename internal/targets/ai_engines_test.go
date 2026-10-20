package targets

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func envOf(m map[string]string) envFunc { return func(k string) string { return m[k] } }

func base(paths []string) []string {
	var out []string
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLlamaCacheDirsPerPlatform(t *testing.T) {
	home := filepath.FromSlash("/h")
	cases := []struct {
		goos string
		env  map[string]string
		want []string
	}{
		{"linux", nil, []string{filepath.Join(home, ".cache", "llama.cpp")}},
		{"darwin", nil, []string{filepath.Join(home, ".cache", "llama.cpp"), filepath.Join(home, "Library", "Caches", "llama.cpp")}},
		{"windows", map[string]string{"LOCALAPPDATA": `C:\L`}, []string{filepath.Join(home, ".cache", "llama.cpp"), filepath.Join(`C:\L`, "llama.cpp")}},
		{"linux", map[string]string{"LLAMA_CACHE": "/custom"}, []string{"/custom", filepath.Join(home, ".cache", "llama.cpp")}},
	}
	for _, c := range cases {
		got := llamaCacheDirs(envOf(c.env), home, c.goos)
		if !equal(got, c.want) {
			t.Errorf("%s %v: got %v, want %v", c.goos, c.env, got, c.want)
		}
	}
}

func TestLlamaFilesGgufAndStalePartials(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old := now.Add(-48 * time.Hour)

	writeFile(t, filepath.Join(dir, "a.gguf"), "m")
	writeFile(t, filepath.Join(dir, "sub", "b.GGUF"), "m")
	writeFile(t, filepath.Join(dir, "stale.gguf.partial"), "m")
	writeFile(t, filepath.Join(dir, "fresh.gguf.partial"), "m")
	writeFile(t, filepath.Join(dir, "dl.gguf.downloadInProgress"), "m")
	writeFile(t, filepath.Join(dir, "notes.txt"), "m")
	writeFile(t, filepath.Join(dir, "a.gguf.json"), "{}")
	for _, n := range []string{"stale.gguf.partial", "dl.gguf.downloadInProgress"} {
		if err := os.Chtimes(filepath.Join(dir, n), old, old); err != nil {
			t.Fatal(err)
		}
	}

	got := base(llamaFiles([]string{dir, dir, filepath.Join(dir, "missing")}, now))
	want := []string{"a.gguf", "b.GGUF", "dl.gguf.downloadInProgress", "stale.gguf.partial"}
	if !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLMStudioModelsAreSecondLevelDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "lmstudio-community", "Llama-3-8B-GGUF", "m.gguf"), "x")
	writeFile(t, filepath.Join(root, "lmstudio-community", "Qwen-GGUF", "q.gguf"), "x")
	writeFile(t, filepath.Join(root, "TheBloke", "Mistral", "m.gguf"), "x")
	writeFile(t, filepath.Join(root, ".internal", "cache", "x"), "x")
	writeFile(t, filepath.Join(root, "pub", ".hidden", "x"), "x")
	writeFile(t, filepath.Join(root, "loose-file.txt"), "x")
	writeFile(t, filepath.Join(root, "pub", "stray.txt"), "x")

	got := lmStudioModels([]string{root, root, filepath.Join(root, "missing")})
	if want := []string{"Llama-3-8B-GGUF", "Mistral", "Qwen-GGUF"}; !equal(base(got), want) {
		t.Fatalf("got %v, want %v", base(got), want)
	}
	label := LMStudioModels().(Labeler).Label(filepath.Join(root, "TheBloke", "Mistral"))
	if label != "LM Studio: TheBloke/Mistral" {
		t.Errorf("label = %q", label)
	}
}

func TestLMStudioRoots(t *testing.T) {
	home := filepath.FromSlash("/h")
	got := lmStudioRoots(home)
	want := []string{filepath.Join(home, ".lmstudio", "models"), filepath.Join(home, ".cache", "lm-studio", "models")}
	if !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if lmStudioRoots("") != nil {
		t.Error("no home, no roots")
	}
}

func TestTorchHubDirs(t *testing.T) {
	home := filepath.FromSlash("/h")
	got := torchHubDirs(envOf(map[string]string{"TORCH_HOME": "/t"}), home)
	want := []string{filepath.Join("/t", "hub"), filepath.Join(home, ".cache", "torch", "hub")}
	if !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := torchHubDirs(envOf(nil), home); len(got) != 1 || got[0] != filepath.Join(home, ".cache", "torch", "hub") {
		t.Fatalf("default: %v", got)
	}
}

func TestUvCacheDirsPerPlatform(t *testing.T) {
	home := filepath.FromSlash("/h")
	if got := uvCacheDirs(envOf(map[string]string{"UV_CACHE_DIR": "/u"}), home, "linux"); got[0] != "/u" {
		t.Errorf("UV_CACHE_DIR must win: %v", got)
	}
	win := uvCacheDirs(envOf(map[string]string{"LOCALAPPDATA": `C:\L`}), home, "windows")
	if win[len(win)-1] != filepath.Join(`C:\L`, "uv", "cache") {
		t.Errorf("windows: %v", win)
	}
	mac := uvCacheDirs(envOf(nil), home, "darwin")
	if mac[len(mac)-1] != filepath.Join(home, "Library", "Caches", "uv") {
		t.Errorf("darwin: %v", mac)
	}
}

func TestDefaultIncludesEveryAIEngineOnlyWhenRequested(t *testing.T) {
	names := func(ts []Target) map[string]bool {
		m := make(map[string]bool)
		for _, tt := range ts {
			m[tt.Name()] = true
		}
		return m
	}
	if n := names(Default(false)); n["llama.cpp model"] || n["uv cache"] {
		t.Error("AI targets must be opt-in")
	}
	n := names(Default(true))
	for _, want := range []string{"huggingface", "ollama orphan blob", "llama.cpp model", "lm studio model", "pytorch hub", "uv cache", "pip cache"} {
		if !n[want] {
			t.Errorf("Default(true) missing %q", want)
		}
	}
	for _, tt := range Default(true) {
		if _, isPattern := tt.(PatternTarget); isPattern {
			continue // per-project targets are covered elsewhere
		}
		if _, ok := tt.(LocationTarget); !ok || tt.GitAware() {
			t.Errorf("%s must be a non-git LocationTarget", tt.Name())
		}
	}
}
