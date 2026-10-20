package cleaner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// A symlink inside node_modules (as pnpm creates towards its global store)
// must be removed as a link: whatever it points to has to survive.
func TestSymlinkToExternalDirIsRemovedButTargetSurvives(t *testing.T) {
	external := filepath.Join(t.TempDir(), "pnpm-store")
	keep := filepath.Join(external, "pkg", "index.js")
	if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	nm := nodeModules(t)
	link := filepath.Join(nm, "linked-pkg")
	if err := linkDir(external, link); err != nil {
		t.Skipf("cannot create a directory link here: %v", err)
	}

	out := New(Options{}).Clean(context.Background(), result(nm, &gitguard.GitStatus{}))
	if out.Err != nil || !out.Deleted {
		t.Fatalf("want deleted: %+v", out)
	}
	if exists(nm) {
		t.Fatal("node_modules must be gone")
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "precious" {
		t.Fatalf("external directory must be untouched: %v %q", err, data)
	}
}

// Read-only files (Windows attribute) and read-only directories (POSIX) block
// a plain RemoveAll; the cleaner must fix the permissions and retry.
func TestDeletesReadOnlyContent(t *testing.T) {
	nm := nodeModules(t)
	locked := filepath.Join(nm, "locked-dir")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(locked, "ro.txt")
	if err := os.WriteFile(file, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	out := New(Options{}).Clean(context.Background(), result(nm, &gitguard.GitStatus{}))
	if out.Err != nil || !out.Deleted || exists(nm) {
		t.Fatalf("read-only content must not block deletion: %+v exists=%v", out, exists(nm))
	}
}

// linkDir links link -> target. Windows symlinks need a privilege that CI and
// developer shells often lack, so fall back to a directory junction, which is
// what many Windows tools (and pnpm) create anyway.
func linkDir(target, link string) error {
	err := os.Symlink(target, link)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	if out, jerr := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); jerr != nil {
		return fmt.Errorf("symlink: %v; junction: %v: %s", err, jerr, out)
	}
	return nil
}
