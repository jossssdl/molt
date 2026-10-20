// Package scanner finds reclaimable directories concurrently and measures
// them, attaching the Git safety state of the repository that owns each one.
package scanner

import (
	"fmt"
	"strings"
	"time"

	"github.com/jossssdl/molt/internal/gitguard"
	"github.com/jossssdl/molt/internal/targets"
)

// Safety is the deletion verdict shown to the user.
type Safety int

const (
	// SafetySafe means nothing known to Git would be lost.
	SafetySafe Safety = iota
	// SafetyDirtyGit means the owning repo has uncommitted, untracked or
	// unpushed work (or the target is tracked).
	SafetyDirtyGit
	// SafetyUnknown means the state could not be determined.
	SafetyUnknown
)

func (s Safety) String() string {
	switch s {
	case SafetySafe:
		return "SAFE"
	case SafetyDirtyGit:
		return "DIRTY GIT"
	default:
		return "UNKNOWN"
	}
}

// ScanResult is one reclaimable item found on disk.
type ScanResult struct {
	// Path is the absolute path of the directory or file.
	Path string
	// Target is the rule that matched; nil for scan-level errors.
	Target targets.Target
	// SizeBytes is the total size of regular files below Path.
	SizeBytes int64
	// FileCount is the number of regular files below Path.
	FileCount int64
	// LastModified is the newest modification time below Path.
	LastModified time.Time
	// Git is the repository state; nil when the target is not git-aware.
	Git *gitguard.GitStatus
	// Err is a non-fatal problem (e.g. unreadable subdirectories); size may
	// be a lower bound when set.
	Err error
}

// Safety computes the deletion verdict.
func (r ScanResult) Safety() Safety {
	if r.Git == nil {
		return SafetySafe
	}
	if r.Git.Err != nil {
		return SafetyUnknown
	}
	if r.Git.Safe() {
		return SafetySafe
	}
	return SafetyDirtyGit
}

// FormatBytes renders n using binary units (KiB, MiB, ...).
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Detail returns the reasons behind a non-SAFE verdict, comma separated.
func (r ScanResult) Detail() string {
	if r.Git == nil {
		return ""
	}
	return strings.Join(r.Git.Reasons(), ", ")
}
