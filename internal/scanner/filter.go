package scanner

import (
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// excluder decides which paths the scan must skip. A pattern matches when it
// (as a glob) matches the entry's base name or full path, or when it is an
// absolute path that is the entry or one of its parents.
type excluder struct{ patterns []string }

func newExcluder(patterns []string) *excluder {
	e := &excluder{}
	for _, p := range patterns {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		if filepath.IsAbs(p) {
			p = filepath.Clean(p)
		}
		e.patterns = append(e.patterns, p)
	}
	return e
}

func fold(s string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(s)
	}
	return s
}

func (e *excluder) match(path, name string) bool {
	if e == nil {
		return false
	}
	for _, p := range e.patterns {
		if ok, _ := filepath.Match(fold(p), fold(name)); ok {
			return true
		}
		if ok, _ := filepath.Match(fold(p), fold(path)); ok {
			return true
		}
		if filepath.IsAbs(p) {
			fp, fpath := fold(p), fold(path)
			if fpath == fp || strings.HasPrefix(fpath, fp+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

// LastActivity is the most recent sign of life of a result: the newest file
// change below it or the last commit of its repository. Zero if unknown.
func (r ScanResult) LastActivity() time.Time {
	t := r.LastModified
	if r.Git != nil && r.Git.LastCommit.After(t) {
		t = r.Git.LastCommit
	}
	return t
}

// Label is the human-readable name of the result when its target provides one
// (for example "HF Model: org/name"), or "".
func (r ScanResult) Label() string {
	if l, ok := r.Target.(interface{ Label(string) string }); ok {
		return l.Label(r.Path)
	}
	return ""
}
