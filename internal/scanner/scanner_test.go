package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jossssdl/molt/internal/targets"
)

func put(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func collect(t *testing.T, s *Scanner, roots ...string) map[string]ScanResult {
	t.Helper()
	got := make(map[string]ScanResult)
	for r := range s.Scan(context.Background(), roots) {
		if r.Err != nil {
			t.Fatalf("unexpected error for %s: %v", r.Path, r.Err)
		}
		got[r.Path] = r
	}
	return got
}

func TestScanFindsTargetsOnceAndMeasuresThem(t *testing.T) {
	root := t.TempDir()

	// node_modules with a nested node_modules: reported once, nested counted.
	put(t, filepath.Join(root, "web", "package.json"), 1)
	put(t, filepath.Join(root, "web", "node_modules", "x", "index.js"), 10)
	put(t, filepath.Join(root, "web", "node_modules", "x", "node_modules", "y.js"), 5)
	// .next needs package.json beside it.
	put(t, filepath.Join(root, "web", ".next", "build.js"), 7)
	// venv: only with pyvenv.cfg.
	put(t, filepath.Join(root, "py", ".venv", "pyvenv.cfg"), 3)
	put(t, filepath.Join(root, "py", ".venv", "lib", "f.py"), 4)
	put(t, filepath.Join(root, "fake", ".venv", "readme"), 100)
	// target: only beside Cargo.toml.
	put(t, filepath.Join(root, "rs", "Cargo.toml"), 1)
	put(t, filepath.Join(root, "rs", "target", "t.bin"), 20)
	put(t, filepath.Join(root, "other", "target", "keep.txt"), 100)

	s := &Scanner{Targets: targets.Default(false), Workers: 4}
	got := collect(t, s, root)

	want := map[string]int64{
		filepath.Join(root, "web", "node_modules"): 15,
		filepath.Join(root, "web", ".next"):        7,
		filepath.Join(root, "py", ".venv"):         7,
		filepath.Join(root, "rs", "target"):        20,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results %v, want %d", len(got), keys(got), len(want))
	}
	for p, size := range want {
		r, ok := got[p]
		if !ok {
			t.Fatalf("missing %s", p)
		}
		if r.SizeBytes != size {
			t.Errorf("%s: size %d, want %d", p, r.SizeBytes, size)
		}
		if r.Git != nil {
			t.Errorf("%s: nil Guard must not produce Git status", p)
		}
		if r.Safety() != SafetySafe {
			t.Errorf("%s: want SAFE without git info", p)
		}
	}
}

func TestOverlappingRootsDeduplicate(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "a", "node_modules", "f"), 1)
	s := &Scanner{Targets: targets.Default(false)}
	got := collect(t, s, root, filepath.Join(root, "a"))
	if len(got) != 1 {
		t.Fatalf("want 1 result, got %v", keys(got))
	}
}

func TestCancelledContextClosesChannel(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		put(t, filepath.Join(root, string(rune('a'+i)), "node_modules", "f"), 1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Scanner{Targets: targets.Default(false)}
	for range s.Scan(ctx, []string{root}) {
	}
}

func TestMeasureFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "blob")
	put(t, p, 42)
	u, err := Measure(context.Background(), p)
	if err != nil || u.Size != 42 || u.Files != 1 {
		t.Fatalf("got %+v, %v", u, err)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536: "1.5 KiB", 1 << 30: "1.0 GiB"}
	for n, want := range cases {
		if got := FormatBytes(n); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func keys(m map[string]ScanResult) []string {
	var k []string
	for p := range m {
		k = append(k, p)
	}
	return k
}
