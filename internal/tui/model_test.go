package tui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jossssdl/molt/internal/cleaner"
	"github.com/jossssdl/molt/internal/gitguard"
	"github.com/jossssdl/molt/internal/scanner"
	"github.com/jossssdl/molt/internal/targets"
)

func item(path string, size int64, idle time.Duration, dirty bool) scanner.ScanResult {
	g := &gitguard.GitStatus{IsRepo: true, LastCommit: time.Now().Add(-idle)}
	if dirty {
		g.DirtyFiles = 1
	}
	return scanner.ScanResult{Path: path, Target: targets.NodeModules(), SizeBytes: size,
		LastModified: time.Now().Add(-idle), Git: g}
}

func newModel(items ...scanner.ScanResult) Model {
	ch := make(chan scanner.ScanResult)
	m := New(context.Background(), ch, cleaner.New(cleaner.Options{DryRun: true}), true)
	for _, it := range items {
		next, _ := m.Update(resultMsg(it))
		m = next.(Model)
	}
	next, _ := m.Update(scanDoneMsg{})
	return next.(Model)
}

func press(m Model, k tea.KeyMsg) Model {
	next, _ := m.Update(k)
	return next.(Model)
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

var space = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}

func TestSortBySizeThenInactivity(t *testing.T) {
	m := newModel(
		item("/a", 10, time.Hour, false),
		item("/b", 30, 24*time.Hour, false),
		item("/c", 20, 90*24*time.Hour, false),
	)
	if m.items[0].Path != "/b" {
		t.Fatalf("size sort: first=%s", m.items[0].Path)
	}
	m = press(m, runes("i"))
	if m.items[0].Path != "/c" {
		t.Fatalf("idle sort: first=%s", m.items[0].Path)
	}
}

func TestSpaceSelectsAnyItemIncludingDirtyAndAdvances(t *testing.T) {
	m := newModel(item("/dirty", 50, time.Hour, true), item("/safe", 10, time.Hour, false))
	m = press(m, space)
	if !m.selected["/dirty"] || m.cursor != 1 {
		t.Fatalf("space must select the dirty item and advance: sel=%v cursor=%d", m.selected, m.cursor)
	}
	m = press(m, space)
	if len(m.selected) != 2 {
		t.Fatalf("want both selected: %v", m.selected)
	}
}

func TestSelectAllSafeVsAll(t *testing.T) {
	m := newModel(item("/dirty", 50, time.Hour, true), item("/safe", 10, time.Hour, false))
	m = press(m, runes("a"))
	if len(m.selected) != 1 || !m.selected["/safe"] {
		t.Fatalf("a must select only SAFE: %v", m.selected)
	}
	m = press(m, runes("n"))
	m = press(m, runes("A"))
	if len(m.selected) != 2 {
		t.Fatalf("A must select everything in view: %v", m.selected)
	}
}

func TestFilterLimitsView(t *testing.T) {
	m := newModel(item("/dirty", 50, time.Hour, true), item("/safe", 10, time.Hour, false))
	m = press(m, runes("2")) // Safe
	if len(m.shown) != 1 || m.items[m.shown[0]].Path != "/safe" {
		t.Fatalf("safe filter: %v", m.shown)
	}
	m = press(m, runes("3")) // Risky
	if len(m.shown) != 1 || m.items[m.shown[0]].Path != "/dirty" {
		t.Fatalf("risky filter: %v", m.shown)
	}
}

func TestConfirmRiskyOffersSafeOnlyOrForce(t *testing.T) {
	m := newModel(item("/dirty", 50, time.Hour, true), item("/safe", 10, time.Hour, false))
	m = press(m, runes("A"))
	m = press(m, runes("d"))
	if m.mode != modeConfirm || !strings.Contains(m.statusLine(), "[f] ALL") {
		t.Fatalf("want risky confirm dialog, got mode=%v %q", m.mode, m.statusLine())
	}
	next, cmd := m.Update(runes("y"))
	m = next.(Model)
	if m.mode != modeDeleting || cmd == nil {
		t.Fatal("y should delete the SAFE subset")
	}
}

func TestCancelKeepsSelection(t *testing.T) {
	m := newModel(item("/safe", 10, time.Hour, false))
	m = press(m, space)
	m = press(m, runes("d"))
	m = press(m, runes("n"))
	if m.mode != modeBrowse || len(m.selected) != 1 {
		t.Fatal("cancel should return to browse and keep the selection")
	}
}

func TestMouseClickTogglesRow(t *testing.T) {
	m := newModel(item("/a", 50, time.Hour, false), item("/b", 10, time.Hour, false))
	m.height = 30
	next, _ := m.Update(tea.MouseMsg{X: 5, Y: firstRowY + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	if !m.selected["/b"] || m.cursor != 1 {
		t.Fatalf("click on row 2 must select /b: %v cursor=%d", m.selected, m.cursor)
	}
}

func TestViewFitsTerminal(t *testing.T) {
	var items []scanner.ScanResult
	for i := 0; i < 100; i++ {
		items = append(items, item("/some/very/long/path/"+strings.Repeat("x", 200)+"/node_modules", int64(i+1), time.Hour, i%2 == 0))
	}
	m := newModel(items...)
	m.width, m.height = 80, 24
	m.refresh()
	v := m.View()
	if n := strings.Count(v, "\n") + 1; n > 24 {
		t.Fatalf("view has %d lines for a 24-line terminal", n)
	}
	for _, want := range []string{"SAFE", "DIRTY GIT", "DRY-RUN", "reclaimable"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

// slowReader delivers its input after a delay so results arrive first.
type slowReader struct {
	data  string
	delay time.Duration
	done  bool
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	time.Sleep(r.delay)
	r.done = true
	return copy(p, r.data), nil
}

// TestProgramEndToEnd drives a real tea.Program through its input pipeline:
// raw bytes (space, space, d, n, q) must end up as a selection.
func TestProgramEndToEnd(t *testing.T) {
	ch := make(chan scanner.ScanResult, 2)
	ch <- item("/a", 50, time.Hour, true)
	ch <- item("/b", 10, time.Hour, false)
	close(ch)

	m := New(context.Background(), ch, cleaner.New(cleaner.Options{DryRun: true}), true)
	p := tea.NewProgram(m,
		tea.WithInput(&slowReader{data: "  q", delay: 600 * time.Millisecond}),
		tea.WithOutput(io.Discard))
	final, err := p.Run()
	if err != nil {
		t.Fatal(err)
	}
	fm := final.(Model)
	if len(fm.items) != 2 || len(fm.selected) != 2 {
		t.Fatalf("raw space keys must select items: items=%d selected=%v", len(fm.items), fm.selected)
	}
}
