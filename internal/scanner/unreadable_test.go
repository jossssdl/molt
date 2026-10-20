package scanner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jossssdl/molt/internal/targets"
)

// An unreadable directory must be reported as a warning (Target == nil) and
// must not stop the rest of the scan.
func TestUnreadableDirectoryIsReportedNotFatal(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	root := t.TempDir()
	put(t, filepath.Join(root, "ok", "node_modules", "f"), 1)
	locked := filepath.Join(root, "locked")
	put(t, filepath.Join(locked, "x"), 1)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o755)

	s := &Scanner{Targets: targets.Default(false)}
	var items, warnings int
	for r := range s.Scan(context.Background(), []string{root}) {
		if r.Target == nil {
			warnings++
		} else {
			items++
		}
	}
	if items != 1 || warnings != 1 {
		t.Fatalf("items=%d warnings=%d, want 1 and 1", items, warnings)
	}
}
