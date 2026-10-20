// Package tui is the interactive Bubbletea front-end: a sortable table of
// scan results with safety badges, multi-select and a reclaimable-space bar.
package tui

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jossssdl/molt/internal/cleaner"
	"github.com/jossssdl/molt/internal/scanner"
)

type sortKey int

const (
	sortSize sortKey = iota
	sortInactivity
)

type mode int

const (
	modeBrowse mode = iota
	modeConfirm
	modeDeleting
)

type (
	resultMsg   scanner.ScanResult
	scanDoneMsg struct{}
	cleanedMsg  []cleaner.Outcome
)

// Model is the Bubbletea model.
type Model struct {
	ctx     context.Context
	results <-chan scanner.ScanResult
	cleaner *cleaner.Cleaner
	dryRun  bool

	items    []scanner.ScanResult
	selected map[string]bool
	cursor   int
	offset   int
	sortBy   sortKey
	mode     mode
	scanning bool
	status   string

	spin          spinner.Model
	width, height int
	freed         int64
	now           func() time.Time
}

// New builds a Model that consumes results until the channel closes.
func New(ctx context.Context, results <-chan scanner.ScanResult, c *cleaner.Cleaner, dryRun bool) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return Model{
		ctx:      ctx,
		results:  results,
		cleaner:  c,
		dryRun:   dryRun,
		selected: make(map[string]bool),
		scanning: true,
		spin:     sp,
		now:      time.Now,
		width:    100,
		height:   30,
	}
}

// Init starts the spinner and the result stream.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, waitForResult(m.results))
}

func waitForResult(ch <-chan scanner.ScanResult) tea.Cmd {
	return func() tea.Msg {
		for r := range ch {
			if r.Target == nil { // scan-level warning, not an item
				continue
			}
			return resultMsg(r)
		}
		return scanDoneMsg{}
	}
}

// activity is the most recent sign of life: a commit or a file change.
func activity(r scanner.ScanResult) time.Time {
	t := r.LastModified
	if r.Git != nil && r.Git.LastCommit.After(t) {
		t = r.Git.LastCommit
	}
	return t
}

func (m *Model) sortItems() {
	cur := ""
	if m.cursor < len(m.items) {
		cur = m.items[m.cursor].Path
	}
	sort.SliceStable(m.items, func(i, j int) bool {
		a, b := m.items[i], m.items[j]
		if m.sortBy == sortInactivity {
			ta, tb := activity(a), activity(b)
			if ta.IsZero() != tb.IsZero() {
				return !ta.IsZero() // unknown activity last
			}
			if !ta.Equal(tb) {
				return ta.Before(tb) // longest idle first
			}
		}
		if a.SizeBytes != b.SizeBytes {
			return a.SizeBytes > b.SizeBytes
		}
		return a.Path < b.Path
	})
	for i, it := range m.items {
		if it.Path == cur {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

func (m *Model) clampScroll() {
	rows := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// visibleRows is the number of table rows that fit under the header and above
// the footer.
func (m Model) visibleRows() int {
	if r := m.height - 9; r > 3 {
		return r
	}
	return 3
}

func selectable(r scanner.ScanResult) bool { return r.Safety() == scanner.SafetySafe }

func (m Model) selectedItems() []scanner.ScanResult {
	var out []scanner.ScanResult
	for _, it := range m.items {
		if m.selected[it.Path] {
			out = append(out, it)
		}
	}
	return out
}

func sumBytes(items []scanner.ScanResult) (n int64) {
	for _, it := range items {
		n += it.SizeBytes
	}
	return n
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampScroll()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case resultMsg:
		m.items = append(m.items, scanner.ScanResult(msg))
		m.sortItems()
		return m, waitForResult(m.results)

	case scanDoneMsg:
		m.scanning = false

	case cleanedMsg:
		m.mode = modeBrowse
		m.applyOutcomes(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) applyOutcomes(outs []cleaner.Outcome) {
	var done, failed int
	var firstErr error
	removed := make(map[string]bool)
	for _, o := range outs {
		switch {
		case o.Err != nil:
			failed++
			if firstErr == nil {
				firstErr = o.Err
			}
		case o.Deleted:
			done++
			m.freed += o.Bytes
			removed[o.Path] = true
		case o.DryRun:
			done++
			m.freed += o.Bytes
		}
	}
	kept := m.items[:0]
	for _, it := range m.items {
		if removed[it.Path] {
			delete(m.selected, it.Path)
			continue
		}
		kept = append(kept, it)
	}
	m.items = kept
	if m.cursor >= len(m.items) {
		m.cursor = max(0, len(m.items)-1)
	}
	m.clampScroll()

	verb := "freed"
	if m.dryRun {
		verb = "would free (dry-run)"
	}
	m.status = fmt.Sprintf("%d item(s) processed, %s %s", done, verb, scanner.FormatBytes(m.freed))
	if failed > 0 {
		m.status += fmt.Sprintf("; %d failed: %v", failed, firstErr)
	}
}

func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	if s == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.mode {
	case modeDeleting:
		return m, nil
	case modeConfirm:
		if s == "y" || s == "Y" {
			m.mode = modeDeleting
			m.status = "deleting..."
			return m, m.cleanCmd(m.selectedItems())
		}
		m.mode = modeBrowse
		m.status = "cancelled"
		return m, nil
	}

	m.status = ""
	switch s {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.items)-1, m.cursor+1)
		m.cursor = max(0, m.cursor)
	case "pgup":
		m.cursor = max(0, m.cursor-m.visibleRows())
	case "pgdown":
		m.cursor = max(0, min(len(m.items)-1, m.cursor+m.visibleRows()))
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = max(0, len(m.items)-1)
	case " ", "x":
		if len(m.items) > 0 {
			it := m.items[m.cursor]
			switch {
			case !selectable(it):
				m.status = "not selectable: " + it.Safety().String() + " " + it.Detail()
			case m.selected[it.Path]:
				delete(m.selected, it.Path)
			default:
				m.selected[it.Path] = true
			}
		}
	case "a":
		for _, it := range m.items {
			if selectable(it) {
				m.selected[it.Path] = true
			}
		}
	case "n":
		m.selected = make(map[string]bool)
	case "s":
		m.sortBy = sortSize
		m.sortItems()
	case "i":
		m.sortBy = sortInactivity
		m.sortItems()
	case "d", "enter":
		if len(m.selected) == 0 {
			m.status = "nothing selected (space to select, a for all SAFE)"
		} else {
			m.mode = modeConfirm
		}
	}
	m.clampScroll()
	return m, nil
}

func (m Model) cleanCmd(items []scanner.ScanResult) tea.Cmd {
	return func() tea.Msg { return cleanedMsg(m.cleaner.CleanAll(m.ctx, items)) }
}

// Run starts the program and blocks until the user quits.
func Run(ctx context.Context, results <-chan scanner.ScanResult, c *cleaner.Cleaner, dryRun bool) error {
	_, err := tea.NewProgram(New(ctx, results, c, dryRun), tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}
