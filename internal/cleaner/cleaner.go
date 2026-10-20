// Package cleaner is the only place in molt that deletes anything. It is
// dry-run by default and refuses anything that is not a freshly re-validated
// scan result for a known target.
package cleaner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jossssdl/molt/internal/scanner"
	"github.com/jossssdl/molt/internal/targets"
)

// Options configures a Cleaner.
type Options struct {
	// DryRun reports what would be deleted without touching the disk.
	DryRun bool
	// AllowDirty permits deleting results whose Git state is not SAFE.
	AllowDirty bool
}

// Outcome is the result of cleaning one item.
type Outcome struct {
	Path    string
	Bytes   int64
	Deleted bool // true only if the data was really removed
	DryRun  bool // true if the item passed all checks but DryRun was set
	Err     error
}

// Cleaner deletes scan results safely.
type Cleaner struct{ opts Options }

// New returns a Cleaner.
func New(opts Options) *Cleaner { return &Cleaner{opts: opts} }

// ErrUnsafe is wrapped by every refusal so callers can tell it from I/O errors.
var ErrUnsafe = errors.New("refusing to delete")

// WithAllowDirty returns a copy that also deletes results whose Git state is
// not SAFE. Callers must have obtained explicit user consent first.
func (c *Cleaner) WithAllowDirty() *Cleaner {
	o := c.opts
	o.AllowDirty = true
	return &Cleaner{opts: o}
}

// locations memoizes LocationTarget.Resolve for the duration of one batch.
// Resolving can be expensive (Ollama re-reads every manifest), and checking
// each item against a fresh Resolve would make a batch quadratic. It is built
// per call so a batch never trusts results from an earlier one.
type locations struct {
	byTarget map[string]map[string]struct{}
}

func newLocations() *locations { return &locations{byTarget: make(map[string]map[string]struct{})} }

// has reports whether p is currently produced by t, resolving t at most once.
func (l *locations) has(t targets.LocationTarget, p string) bool {
	set, ok := l.byTarget[t.Name()]
	if !ok {
		set = make(map[string]struct{})
		if paths, err := t.Resolve(); err == nil {
			for _, q := range paths {
				set[q] = struct{}{}
			}
		}
		l.byTarget[t.Name()] = set
	}
	_, found := set[p]
	return found
}

// Clean removes one scan result, or only reports it under DryRun.
func (c *Cleaner) Clean(ctx context.Context, r scanner.ScanResult) Outcome {
	return c.clean(ctx, r, newLocations())
}

func (c *Cleaner) clean(ctx context.Context, r scanner.ScanResult, locs *locations) Outcome {
	out := Outcome{Path: r.Path, Bytes: r.SizeBytes}
	if err := ctx.Err(); err != nil {
		out.Err = err
		return out
	}
	if err := c.check(r, locs); err != nil {
		out.Err = err
		return out
	}
	if c.opts.DryRun {
		out.DryRun = true
		return out
	}
	// RemoveAll removes a symlink itself rather than its destination.
	if err := os.RemoveAll(r.Path); err != nil {
		out.Err = err
		return out
	}
	out.Deleted = true
	return out
}

// CleanAll cleans items in order, stopping early if ctx is cancelled.
func (c *Cleaner) CleanAll(ctx context.Context, items []scanner.ScanResult) []Outcome {
	outs := make([]Outcome, 0, len(items))
	locs := newLocations() // shared by the whole batch: Resolve runs once per target
	for _, r := range items {
		outs = append(outs, c.clean(ctx, r, locs))
		if ctx.Err() != nil {
			break
		}
	}
	return outs
}

// check enforces every precondition for deleting r.
func (c *Cleaner) check(r scanner.ScanResult, locs *locations) error {
	refuse := func(format string, a ...any) error {
		return fmt.Errorf("%w %s: %s", ErrUnsafe, r.Path, fmt.Sprintf(format, a...))
	}

	if r.Target == nil {
		return refuse("no target attached")
	}
	p := r.Path
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return refuse("path is not absolute and clean")
	}
	if filepath.Dir(p) == p {
		return refuse("path is a filesystem root")
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(p, home); err == nil && !strings.HasPrefix(rel, "..") {
			return refuse("path is the home directory or one of its parents")
		}
	}

	info, err := os.Lstat(p)
	if err != nil {
		return err
	}
	// The scan may be stale: make sure the path is still what it was.
	switch t := r.Target.(type) {
	case targets.PatternTarget:
		if !info.IsDir() || !t.Match(p, filepath.Base(p)) {
			return refuse("no longer matches target %q", t.Name())
		}
	case targets.LocationTarget:
		if !locs.has(t, p) {
			return refuse("not a location of target %q", t.Name())
		}
	default:
		return refuse("unknown target kind")
	}

	if r.Target.GitAware() && !c.opts.AllowDirty {
		switch {
		case r.Git == nil:
			return refuse("git state unknown")
		case !r.Git.Safe():
			return refuse("git: %s", strings.Join(r.Git.Reasons(), ", "))
		}
	}
	return nil
}
