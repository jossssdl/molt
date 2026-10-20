package scanner

import (
	"context"
	"io/fs"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/jossssdl/molt/internal/gitguard"
	"github.com/jossssdl/molt/internal/targets"
)

// Scanner discovers reclaimable directories and measures them in parallel.
type Scanner struct {
	// Targets are the rules to look for.
	Targets []targets.Target
	// Guard checks Git state for git-aware targets. Nil disables the check.
	Guard gitguard.Guard
	// Workers is the number of measuring goroutines (default: NumCPU).
	Workers int
}

type candidate struct {
	path   string
	target targets.Target
}

// Scan walks roots, resolves location targets, and streams results as they
// are measured. The returned channel is closed when everything is done or ctx
// is cancelled. Scan-level problems (e.g. an unreadable root) arrive as
// results with a nil Target and Err set.
func (s *Scanner) Scan(ctx context.Context, roots []string) <-chan ScanResult {
	out := make(chan ScanResult)
	jobs := make(chan candidate, 64)

	var patterns []targets.PatternTarget
	var locations []targets.LocationTarget
	for _, t := range s.Targets {
		switch v := t.(type) {
		case targets.PatternTarget:
			patterns = append(patterns, v)
		case targets.LocationTarget:
			locations = append(locations, v)
		}
	}

	emit := func(r ScanResult) bool {
		select {
		case out <- r:
			return true
		case <-ctx.Done():
			return false
		}
	}

	// Discovery: one walker per root plus one per location target. A shared
	// set drops paths reached twice (overlapping roots).
	var seen sync.Map
	submit := func(c candidate) bool {
		if _, dup := seen.LoadOrStore(filepath.Clean(c.path), struct{}{}); dup {
			return true
		}
		select {
		case jobs <- c:
			return true
		case <-ctx.Done():
			return false
		}
	}

	var discovery sync.WaitGroup
	for _, root := range roots {
		discovery.Add(1)
		go func(root string) {
			defer discovery.Done()
			discover(ctx, root, patterns, submit, emit)
		}(root)
	}
	for _, lt := range locations {
		discovery.Add(1)
		go func(lt targets.LocationTarget) {
			defer discovery.Done()
			paths, err := lt.Resolve()
			if err != nil {
				emit(ScanResult{Path: lt.Name(), Err: err})
			}
			for _, p := range paths {
				if !submit(candidate{path: p, target: lt}) {
					return
				}
			}
		}(lt)
	}
	go func() {
		discovery.Wait()
		close(jobs)
	}()

	workers := s.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	var pool sync.WaitGroup
	for i := 0; i < workers; i++ {
		pool.Add(1)
		go func() {
			defer pool.Done()
			for c := range jobs {
				if ctx.Err() != nil {
					return
				}
				if !emit(s.process(ctx, c)) {
					return
				}
			}
		}()
	}
	go func() {
		pool.Wait()
		close(out)
	}()
	return out
}

// discover walks root and submits every directory matched by a pattern target.
// Matched directories are not descended into, so nested node_modules are
// reported once, as part of their outermost parent.
func discover(ctx context.Context, root string, patterns []targets.PatternTarget,
	submit func(candidate) bool, emit func(ScanResult) bool) {
	if len(patterns) == 0 {
		return
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		emit(ScanResult{Path: root, Err: err})
		return
	}
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			if d == nil {
				return err // the root itself is unusable
			}
			if d.IsDir() {
				return fs.SkipDir // unreadable directory: keep going
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == ".git" {
			return fs.SkipDir
		}
		for _, t := range patterns {
			if t.Match(path, name) {
				if !submit(candidate{path: path, target: t}) {
					return ctx.Err()
				}
				return fs.SkipDir
			}
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		emit(ScanResult{Path: root, Err: err})
	}
}

// process measures one candidate and checks its Git state.
func (s *Scanner) process(ctx context.Context, c candidate) ScanResult {
	u, err := Measure(ctx, c.path)
	r := ScanResult{
		Path:         c.path,
		Target:       c.target,
		SizeBytes:    u.Size,
		FileCount:    u.Files,
		LastModified: u.Newest,
		Err:          err,
	}
	if c.target.GitAware() && s.Guard != nil {
		st := s.Guard.Status(ctx, c.path)
		r.Git = &st
	}
	return r
}
