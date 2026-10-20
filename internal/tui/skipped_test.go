package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jossssdl/molt/internal/scanner"
	"github.com/jossssdl/molt/internal/targets"
)

func TestSkippedPathsAreCountedAndShown(t *testing.T) {
	m := newModel(item("/ok", 10, time.Hour, false))
	if strings.Contains(m.View(), "skipped") {
		t.Fatal("no indicator expected when nothing was skipped")
	}

	// A scan-level warning (Target == nil)...
	next, _ := m.Update(warnMsg(scanner.ScanResult{Path: "/denied", Err: errors.New("access denied")}))
	m = next.(Model)
	// ...and an item whose size is a lower bound because of unreadable entries.
	partial := item("/partial", 5, time.Hour, false)
	partial.Err = errors.New("permission denied")
	next, _ = m.Update(resultMsg(partial))
	m = next.(Model)

	if m.skipped != 2 {
		t.Fatalf("skipped = %d, want 2", m.skipped)
	}
	if v := m.View(); !strings.Contains(v, "⚠ 2 paths skipped (permission/error)") {
		t.Errorf("indicator missing in view:\n%s", v)
	}
	if len(m.items) != 2 {
		t.Errorf("warnings must not become items: %d", len(m.items))
	}
}

func TestWaitForResultTurnsWarningsIntoMessages(t *testing.T) {
	ch := make(chan scanner.ScanResult, 2)
	ch <- scanner.ScanResult{Path: "/denied", Err: errors.New("x")}
	ch <- scanner.ScanResult{Path: "/ok", Target: targets.NodeModules()}
	close(ch)
	if _, ok := waitForResult(ch)().(warnMsg); !ok {
		t.Fatal("Target == nil must yield warnMsg")
	}
	if _, ok := waitForResult(ch)().(resultMsg); !ok {
		t.Fatal("regular result must yield resultMsg")
	}
	if _, ok := waitForResult(ch)().(scanDoneMsg); !ok {
		t.Fatal("closed channel must yield scanDoneMsg")
	}
}
