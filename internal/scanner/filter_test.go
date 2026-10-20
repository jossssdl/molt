package scanner

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jossssdl/molt/internal/targets"
)

func projectNames(m map[string]ScanResult) map[string]bool {
	out := make(map[string]bool)
	for p := range m {
		out[filepath.Base(filepath.Dir(p))] = true
	}
	return out
}

func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, filepath.Join(root, "keep", "node_modules", "f"), 2000)
	put(t, filepath.Join(root, "skip", "node_modules", "f"), 3000)
	put(t, filepath.Join(root, "tiny", "node_modules", "f"), 5)
	return root
}

func TestExcludeByNameGlobAndAbsolutePath(t *testing.T) {
	root := tree(t)
	cases := []struct {
		exclude []string
		want    []string
	}{
		{nil, []string{"keep", "skip", "tiny"}},
		{[]string{"skip"}, []string{"keep", "tiny"}},
		{[]string{"s*", "tiny"}, []string{"keep"}},
		{[]string{filepath.Join(root, "keep")}, []string{"skip", "tiny"}},
		{[]string{"  ", ""}, []string{"keep", "skip", "tiny"}},
	}
	for _, c := range cases {
		s := &Scanner{Targets: targets.Default(false), Exclude: c.exclude}
		got := projectNames(collect(t, s, root))
		if len(got) != len(c.want) {
			t.Errorf("exclude %v: got %v, want %v", c.exclude, got, c.want)
			continue
		}
		for _, w := range c.want {
			if !got[w] {
				t.Errorf("exclude %v: missing %s in %v", c.exclude, w, got)
			}
		}
	}
}

func TestExcludeNeverSkipsTheRootItself(t *testing.T) {
	root := tree(t)
	s := &Scanner{Targets: targets.Default(false), Exclude: []string{filepath.Base(root)}}
	if got := collect(t, s, root); len(got) != 3 {
		t.Fatalf("root must still be scanned, got %d results", len(got))
	}
}

func TestMinSize(t *testing.T) {
	s := &Scanner{Targets: targets.Default(false), MinSize: 1000}
	got := projectNames(collect(t, s, tree(t)))
	if len(got) != 2 || got["tiny"] {
		t.Fatalf("got %v", got)
	}
}

func TestMinAge(t *testing.T) {
	root := tree(t)
	now := time.Now()

	fresh := &Scanner{Targets: targets.Default(false), MinAge: 30 * 24 * time.Hour, Now: func() time.Time { return now }}
	if got := collect(t, fresh, root); len(got) != 0 {
		t.Fatalf("recently modified targets must be dropped, got %d", len(got))
	}

	later := now.Add(60 * 24 * time.Hour)
	old := &Scanner{Targets: targets.Default(false), MinAge: 30 * 24 * time.Hour, Now: func() time.Time { return later }}
	if got := collect(t, old, root); len(got) != 3 {
		t.Fatalf("idle targets must be kept, got %d", len(got))
	}
}
