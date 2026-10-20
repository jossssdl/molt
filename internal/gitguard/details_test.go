package gitguard

import (
	"os"
	"strings"
	"testing"
)

func TestDetailsNameEveryCause(t *testing.T) {
	s := GitStatus{IsRepo: true, DirtyFiles: 3, UntrackedFiles: 1, Unpushed: 2, NoUpstream: true, TargetTracked: true}
	got := strings.Join(s.Details(), "|")
	for _, want := range []string{
		"3 tracked files have uncommitted changes",
		"1 untracked file outside the target",
		"2 commits not pushed to upstream",
		"no upstream",
		"tracks files inside the target",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Details missing %q in %q", want, got)
		}
	}
	if d := (GitStatus{IsRepo: true}).Details(); len(d) != 0 {
		t.Errorf("clean repo must have no details: %v", d)
	}
	if d := (GitStatus{IsRepo: true, Err: os.ErrPermission}).Details(); len(d) != 1 || !strings.Contains(d[0], "could not determine") {
		t.Errorf("error details: %v", d)
	}
}
