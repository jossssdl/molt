package tui

import (
	"context"
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
	return m
}

func key(m Model, s string) Model {
	var msg tea.KeyMsg
	switch s {
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestSortBySizeThenInactivity(t *testing.T) {
	m := newModel(
		item("/a", 10, time.Hour, false),
		item("/b", 30, 24*time.Hour, false),
		item("/c", 20, 90*24*time.Hour, false),
	)
	if m.items[0].Path != "/b" {
		t.Fatalf("size sort: first=%s", m.items[0].Path)
	}
	m = key(m, "i")
	if m.items[0].Path != "/c" {
		t.Fatalf("idle sort: first=%s", m.items[0].Path)
	}
}

func TestDirtyItemsCannotBeSelected(t *testing.T) {
	m := newModel(item("/dirty", 50, time.Hour, true), item("/safe", 10, time.Hour, false))
	m = key(m, " ") // cursor on /dirty (largest)
	if len(m.selected) != 0 {
		t.Fatalf("dirty item selected: %v", m.selected)
	}
	m = key(m, "a")
	if len(m.selected) != 1 || !m.selected["/safe"] {
		t.Fatalf("a must select only SAFE items: %v", m.selected)
	}
}

func TestConfirmFlowDryRunKeepsFilesAndItems(t *testing.T) {
	m := newModel(item("/safe", 10, time.Hour, false))
	m = key(m, " ")
	m = key(m, "d")
	if m.mode != modeConfirm {
		t.Fatal("expected confirm mode")
	}
	m = key(m, "n") // anything but y cancels
	if m.mode != modeBrowse || len(m.selected) != 1 {
		t.Fatal("cancel should return to browse and keep selection")
	}
	m = key(m, "d")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = next.(Model)
	if m.mode != modeDeleting || cmd == nil {
		t.Fatal("y should start deleting")
	}
}

func TestViewShowsBadgesAndBar(t *testing.T) {
	m := newModel(item("/dirty", 50, time.Hour, true), item("/safe", 10, time.Hour, false))
	v := m.View()
	for _, want := range []string{"SAFE", "DIRTY GIT", "selected", "DRY-RUN"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}
