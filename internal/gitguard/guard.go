package gitguard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultTimeout = 2 * time.Minute

// Guard reports the Git state that applies to a deletion target.
type Guard interface {
	// Status returns the state of the repository containing targetPath. It
	// never fails outright: problems are reported in GitStatus.Err.
	Status(ctx context.Context, targetPath string) GitStatus
}

// ExecGuard implements Guard by running the git CLI. Repository state is
// computed once per repository and shared across targets, so scanning many
// node_modules inside one monorepo costs a single `git status`.
type ExecGuard struct {
	// GitBin is the git executable (default "git").
	GitBin string
	// Timeout bounds each git invocation (default 2 minutes).
	Timeout time.Duration

	mu    sync.Mutex
	repos map[string]*repoEntry
}

// NewExecGuard returns a Guard backed by the git executable on PATH.
func NewExecGuard() *ExecGuard {
	return &ExecGuard{repos: make(map[string]*repoEntry)}
}

type change struct {
	path      string // slash-separated, relative to the repo root
	untracked bool
}

type repoState struct {
	changes    []change
	unpushed   int
	noUpstream bool
	lastCommit time.Time
	err        error
}

type repoEntry struct {
	once  sync.Once
	state repoState
}

// Status implements Guard.
func (g *ExecGuard) Status(ctx context.Context, targetPath string) GitStatus {
	abs, err := filepath.Abs(targetPath)
	if err != nil {
		return GitStatus{Err: err}
	}
	root, ok := findRepoRoot(filepath.Dir(abs))
	if !ok {
		return GitStatus{}
	}

	st := g.repo(ctx, root)
	out := GitStatus{
		IsRepo:     true,
		Root:       root,
		Unpushed:   st.unpushed,
		NoUpstream: st.noUpstream,
		LastCommit: st.lastCommit,
		Err:        st.err,
	}
	if st.err != nil {
		return out
	}

	rel, err := filepath.Rel(root, abs)
	if err != nil {
		out.Err = err
		return out
	}
	rel = filepath.ToSlash(rel)

	for _, c := range st.changes {
		// Untracked files inside the target are the target itself, not
		// work that deleting it would lose.
		if c.untracked && strings.HasPrefix(c.path, rel+"/") {
			continue
		}
		if c.untracked {
			out.UntrackedFiles++
		} else {
			out.DirtyFiles++
		}
	}

	tracked, err := g.hasTracked(ctx, root, rel)
	if err != nil {
		out.Err = err
		return out
	}
	out.TargetTracked = tracked
	return out
}

// findRepoRoot walks up from dir looking for a .git entry (a directory, or a
// file for worktrees and submodules). Doing this in Go avoids a process spawn
// per target.
func findRepoRoot(dir string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func (g *ExecGuard) repo(ctx context.Context, root string) repoState {
	g.mu.Lock()
	if g.repos == nil {
		g.repos = make(map[string]*repoEntry)
	}
	e := g.repos[root]
	if e == nil {
		e = &repoEntry{}
		g.repos[root] = e
	}
	g.mu.Unlock()

	e.once.Do(func() { e.state = g.load(ctx, root) })
	return e.state
}

func (g *ExecGuard) load(ctx context.Context, root string) repoState {
	var s repoState

	out, err := g.git(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		s.err = err
		return s
	}
	s.changes = parsePorcelain(out)

	// A repository without commits has no HEAD: nothing to push or lose.
	out, err = g.git(ctx, root, "log", "-1", "--format=%ct")
	if err != nil {
		return s
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return s
	}
	s.lastCommit = time.Unix(secs, 0)

	out, err = g.git(ctx, root, "rev-list", "--count", "@{u}..HEAD")
	if err != nil {
		s.noUpstream = true
		return s
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		s.err = fmt.Errorf("parse unpushed count %q: %w", out, err)
		return s
	}
	s.unpushed = n
	return s
}

// hasTracked reports whether git tracks any file at or under rel.
func (g *ExecGuard) hasTracked(ctx context.Context, root, rel string) (bool, error) {
	out, err := g.git(ctx, root, "ls-files", "-z", "--", rel)
	if err != nil {
		return false, err
	}
	return len(out) > 0, nil
}

// parsePorcelain parses `git status --porcelain=v1 -z` output. Ignored files
// ("!!") are skipped; a rename or copy entry is followed by its source path,
// which is consumed.
func parsePorcelain(b []byte) []change {
	var out []change
	recs := strings.Split(string(b), "\x00")
	for i := 0; i < len(recs); i++ {
		r := recs[i]
		if len(r) < 4 {
			continue
		}
		x, y := r[0], r[1]
		if x == '!' {
			continue
		}
		out = append(out, change{path: r[3:], untracked: x == '?' && y == '?'})
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++
		}
	}
	return out
}

func (g *ExecGuard) git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	bin := g.GitBin
	if bin == "" {
		bin = "git"
	}
	full := append([]string{"--literal-pathspecs", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, bin, full...)
	// Read-only: never take the index lock, never prompt, stable messages.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")

	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}
