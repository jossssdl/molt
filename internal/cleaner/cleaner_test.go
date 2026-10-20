package cleaner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jossssdl/molt/internal/gitguard"
	"github.com/jossssdl/molt/internal/scanner"
	"github.com/jossssdl/molt/internal/targets"
)

func nodeModules(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "node_modules")
	if err := os.MkdirAll(filepath.Join(p, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "x", "f.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func result(p string, git *gitguard.GitStatus) scanner.ScanResult {
	return scanner.ScanResult{Path: p, Target: targets.NodeModules(), SizeBytes: 1, Git: git}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestDryRunDoesNotDelete(t *testing.T) {
	p := nodeModules(t)
	out := New(Options{DryRun: true}).Clean(context.Background(), result(p, &gitguard.GitStatus{}))
	if out.Err != nil || !out.DryRun || out.Deleted || !exists(p) {
		t.Fatalf("dry run must keep data: %+v", out)
	}
}

func TestDeletesSafeTarget(t *testing.T) {
	p := nodeModules(t)
	out := New(Options{}).Clean(context.Background(), result(p, &gitguard.GitStatus{}))
	if out.Err != nil || !out.Deleted || exists(p) {
		t.Fatalf("want deleted: %+v", out)
	}
}

func TestRefusesDirtyUnlessAllowed(t *testing.T) {
	p := nodeModules(t)
	dirty := &gitguard.GitStatus{IsRepo: true, DirtyFiles: 2}
	out := New(Options{}).Clean(context.Background(), result(p, dirty))
	if !errors.Is(out.Err, ErrUnsafe) || !exists(p) {
		t.Fatalf("want refusal: %+v", out)
	}
	out = New(Options{AllowDirty: true}).Clean(context.Background(), result(p, dirty))
	if out.Err != nil || !out.Deleted {
		t.Fatalf("AllowDirty should delete: %+v", out)
	}
}

func TestRefusesUnknownGitState(t *testing.T) {
	p := nodeModules(t)
	if out := New(Options{}).Clean(context.Background(), result(p, nil)); !errors.Is(out.Err, ErrUnsafe) || !exists(p) {
		t.Fatalf("want refusal: %+v", out)
	}
}

func TestRefusesPathThatNoLongerMatches(t *testing.T) {
	p := filepath.Join(t.TempDir(), "docs") // not named node_modules
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if out := New(Options{}).Clean(context.Background(), result(p, &gitguard.GitStatus{})); !errors.Is(out.Err, ErrUnsafe) || !exists(p) {
		t.Fatalf("want refusal: %+v", out)
	}
}

func TestRefusesRootsHomeAndRelative(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.VolumeName(home) + string(filepath.Separator), home, "node_modules"} {
		out := New(Options{AllowDirty: true}).Clean(context.Background(), result(p, nil))
		if !errors.Is(out.Err, ErrUnsafe) {
			t.Errorf("%q: want refusal, got %+v", p, out)
		}
	}
}
