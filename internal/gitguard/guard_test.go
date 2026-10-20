package gitguard

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// runGit runs git in dir with a throwaway identity passed via -c, so no
// config is ever written.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{
		"-c", "user.name=test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false", "-C", dir,
	}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo creates a repo with one commit and, when withUpstream is set, a bare
// remote that has received it. It returns the working tree root.
func newRepo(t *testing.T, withUpstream bool) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	write(t, filepath.Join(dir, "a.txt"), "a")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "init")
	if withUpstream {
		remote := t.TempDir()
		runGit(t, remote, "init", "-q", "--bare")
		runGit(t, dir, "remote", "add", "origin", remote)
		runGit(t, dir, "push", "-q", "-u", "origin", "HEAD")
	}
	return dir
}

func status(t *testing.T, target string) GitStatus {
	t.Helper()
	return NewExecGuard().Status(context.Background(), target)
}

func TestNotARepo(t *testing.T) {
	requireGit(t)
	target := filepath.Join(t.TempDir(), "node_modules")
	write(t, filepath.Join(target, "x.js"), "x")
	st := status(t, target)
	if st.IsRepo || !st.Safe() || st.Err != nil {
		t.Fatalf("want safe non-repo, got %+v", st)
	}
}

func TestCleanRepoWithUpstreamIsSafe(t *testing.T) {
	repo := newRepo(t, true)
	target := filepath.Join(repo, "node_modules")
	write(t, filepath.Join(repo, ".gitignore"), "node_modules/\n")
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "commit", "-q", "-m", "ignore")
	runGit(t, repo, "push", "-q")
	write(t, filepath.Join(target, "x.js"), "x")

	st := status(t, target)
	if !st.Safe() || !st.IsRepo || st.LastCommit.IsZero() {
		t.Fatalf("want safe repo with commit time, got %+v (%v)", st, st.Reasons())
	}
}

func TestUntrackedTargetItselfDoesNotMakeRepoDirty(t *testing.T) {
	repo := newRepo(t, true)
	target := filepath.Join(repo, "node_modules")
	write(t, filepath.Join(target, "x.js"), "x") // not ignored, untracked

	st := status(t, target)
	if st.UntrackedFiles != 0 || !st.Safe() {
		t.Fatalf("target contents must not count as untracked work: %+v", st)
	}
}

func TestDirtyAndUntracked(t *testing.T) {
	repo := newRepo(t, true)
	target := filepath.Join(repo, "node_modules")
	write(t, filepath.Join(target, "x.js"), "x")
	write(t, filepath.Join(repo, "a.txt"), "changed")
	write(t, filepath.Join(repo, "new.txt"), "new")

	st := status(t, target)
	if st.DirtyFiles != 1 || st.UntrackedFiles != 1 || st.Safe() {
		t.Fatalf("want 1 dirty + 1 untracked, got %+v", st)
	}
}

func TestUnpushed(t *testing.T) {
	repo := newRepo(t, true)
	write(t, filepath.Join(repo, "b.txt"), "b")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "local")

	st := status(t, filepath.Join(repo, "node_modules"))
	if st.Unpushed != 1 || st.Safe() {
		t.Fatalf("want 1 unpushed, got %+v", st)
	}
}

func TestNoUpstreamIsUnsafe(t *testing.T) {
	repo := newRepo(t, false)
	st := status(t, filepath.Join(repo, "node_modules"))
	if !st.NoUpstream || st.Safe() {
		t.Fatalf("want no-upstream unsafe, got %+v", st)
	}
}

func TestTrackedTargetIsUnsafe(t *testing.T) {
	repo := newRepo(t, true)
	target := filepath.Join(repo, "node_modules")
	write(t, filepath.Join(target, "x.js"), "x")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "oops")
	runGit(t, repo, "push", "-q")

	st := status(t, target)
	if !st.TargetTracked || st.Safe() {
		t.Fatalf("want tracked target unsafe, got %+v", st)
	}
}

func TestGitMissingIsUnsafe(t *testing.T) {
	repo := newRepo(t, true)
	g := NewExecGuard()
	g.GitBin = "definitely-not-a-git-binary"
	st := g.Status(context.Background(), filepath.Join(repo, "node_modules"))
	if st.Err == nil || st.Safe() {
		t.Fatalf("want error and unsafe, got %+v", st)
	}
}

func TestParsePorcelain(t *testing.T) {
	in := "?? new.txt\x00 M a.txt\x00R  renamed.txt\x00old.txt\x00!! ignored\x00A  added.txt\x00"
	got := parsePorcelain([]byte(in))
	want := []change{
		{"new.txt", true}, {"a.txt", false}, {"renamed.txt", false}, {"added.txt", false},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
