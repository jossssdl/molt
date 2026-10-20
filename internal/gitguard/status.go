// Package gitguard decides whether a directory is safe to delete from the
// point of view of the Git repository that contains it.
package gitguard

import (
	"fmt"
	"time"
)

// GitStatus is the repository state relevant to deleting a target.
type GitStatus struct {
	// IsRepo is false when the target is not inside any Git working tree.
	IsRepo bool
	// Root is the working tree root (empty when IsRepo is false).
	Root string
	// DirtyFiles counts tracked files with staged or unstaged changes.
	DirtyFiles int
	// UntrackedFiles counts untracked, non-ignored files outside the target.
	UntrackedFiles int
	// Unpushed counts local commits missing from the upstream branch.
	Unpushed int
	// NoUpstream is set when the branch has commits but no upstream to compare.
	NoUpstream bool
	// TargetTracked is set when Git tracks files inside the target itself.
	TargetTracked bool
	// LastCommit is the author-independent commit time of HEAD (zero if none).
	LastCommit time.Time
	// Err is set when the state could not be determined; callers must treat
	// the target as unsafe.
	Err error
}

// Safe reports whether deleting the target cannot lose work known to Git.
// Targets outside a repository are considered safe.
func (s GitStatus) Safe() bool {
	if s.Err != nil {
		return false
	}
	if !s.IsRepo {
		return true
	}
	return s.DirtyFiles == 0 && s.UntrackedFiles == 0 && s.Unpushed == 0 &&
		!s.NoUpstream && !s.TargetTracked
}

// Reasons lists human-readable causes of an unsafe status (nil when safe).
func (s GitStatus) Reasons() []string {
	if s.Err != nil {
		return []string{"git error: " + s.Err.Error()}
	}
	if !s.IsRepo {
		return nil
	}
	var r []string
	if s.DirtyFiles > 0 {
		r = append(r, fmt.Sprintf("%d uncommitted", s.DirtyFiles))
	}
	if s.UntrackedFiles > 0 {
		r = append(r, fmt.Sprintf("%d untracked", s.UntrackedFiles))
	}
	if s.Unpushed > 0 {
		r = append(r, fmt.Sprintf("%d unpushed", s.Unpushed))
	}
	if s.NoUpstream {
		r = append(r, "no upstream")
	}
	if s.TargetTracked {
		r = append(r, "tracked by git")
	}
	return r
}
