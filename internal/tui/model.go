// Package tui is the interactive Bubbletea front-end: a filterable, sortable
// table of scan results with safety badges, multi-select (keyboard and mouse)
// and a reclaimable-space bar.
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
	"github.com/jossssdl/molt/internal/targets"
)

type sortKey int

const (
	sortSize sortKey = iota
	sortInactivity
)

type filter int

const (
	filterAll filter = iota
	filterSafe
	filterRisky
	filterDev
	filterAI
	numFilters
)

var filterNames = [numFilters]string{"All", "Safe", "Risky", "Dev", "AI"}

type mode int

const (
	modeBrowse mode = iota
	modeConfirm
	modeDeleting
)

// Layout constants shared by Update (mouse hit-testing) and View.
const (
	headerLines = 5 // title, stats, bar, filters, column header
	footerLines = 5 // separator, 2 detail lines, status, help
	firstRowY   = headerLines
)

type (
	resultMsg   scanner.ScanResult
	scanDoneMsg struct{}
	tickMsg     time.Time
	cleanedMsg  []cleaner.Outcome
)

// Model is the Bubbletea model.
type Model struct {
	ctx     context.Context
	results <-chan scanner.ScanResult
	cleaner *cleaner.Cleaner
	dryRun  bool

	items    []scanner.ScanResult // every result, sorted
	shown    []int                // indices into items that pass the filter
	selected map[string]bool
	cursor   int // index into shown
	offset   int
	sortBy   sortKey
	filter   filter
	mode     mode
	scanning bool
	needSort bool

	status  string
	statusT time.Time
	freed   int64

	spin          spinner.Model
	width, height int
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

// Init starts the spinner, the re-sort ticker and the result stream.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, tick(), waitForResult(m.results))
}

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
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

func passes(f filter, r scanner.ScanResult) bool {
	switch f {
	case filterSafe:
		return r.Safety() == scanner.SafetySafe
	case filterRisky:
		return r.Safety() != scanner.SafetySafe
	case filterDev:
		return r.Target.Category() == targets.CategoryDev
	case filterAI:
		return r.Target.Category() == targets.CategoryAI
	}
	return true
}

// refresh re-sorts items, recomputes the filtered view and keeps the cursor on
// the same path when possible.
func (m *Model) refresh() {
	cur := ""
	if it, ok := m.cur(); ok {
		cur = it.Path
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
	m.shown = m.shown[:0]
	for i, it := range m.items {
		if passes(m.filter, it) {
			m.shown = append(m.shown, i)
		}
	}
	m.cursor = clamp(m.cursor, 0, len(m.shown)-1)
	for k, idx := range m.shown {
		if m.items[idx].Path == cur {
			m.cursor = k
			break
		}
	}
	m.needSort = false
	m.scroll()
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return max(lo, min(hi, v))
}

func (m Model) cur() (scanner.ScanResult, bool) {
	if m.cursor < 0 || m.cursor >= len(m.shown) {
		return scanner.ScanResult{}, false
	}
	return m.items[m.shown[m.cursor]], true
}

func (m Model) rows() int { return max(3, m.height-headerLines-footerLines) }

func (m *Model) scroll() {
	rows := m.rows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = clamp(m.offset, 0, max(0, len(m.shown)-rows))
}

func (m *Model) move(delta int) {
	m.cursor = clamp(m.cursor+delta, 0, len(m.shown)-1)
	m.scroll()
}

func (m *Model) toast(format string, a ...any) {
	m.status = fmt.Sprintf(format, a...)
	m.statusT = m.now()
}

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

func (m *Model) toggle(it scanner.ScanResult) {
	if m.selected[it.Path] {
		delete(m.selected, it.Path)
	} else {
		m.selected[it.Path] = true
	}
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scroll()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tickMsg:
		if m.needSort {
			m.refresh()
		}
		if m.status != "" && m.mode == modeBrowse && m.now().Sub(m.statusT) > 6*time.Second {
			m.status = ""
		}
		return m, tick()

	case resultMsg:
		m.items = append(m.items, scanner.ScanResult(msg))
		m.needSort = true
		return m, waitForResult(m.results)

	case scanDoneMsg:
		m.scanning = false
		m.refresh()

	case cleanedMsg:
		m.mode = modeBrowse
		m.applyOutcomes(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg), nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleMouse(e tea.MouseMsg) Model {
	if m.mode != modeBrowse {
		return m
	}
	switch {
	case e.Button == tea.MouseButtonWheelUp:
		m.move(-3)
	case e.Button == tea.MouseButtonWheelDown:
		m.move(3)
	case e.Button == tea.MouseButtonLeft && e.Action == tea.MouseActionPress:
		row := e.Y - firstRowY
		if row < 0 || row >= m.rows() {
			return m
		}
		if idx := m.offset + row; idx < len(m.shown) {
			m.cursor = idx
			m.toggle(m.items[m.shown[idx]])
		}
	}
	return m
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
			delete(m.selected, o.Path)
		case o.DryRun:
			done++
			m.freed += o.Bytes
			delete(m.selected, o.Path)
		}
	}
	kept := m.items[:0]
	for _, it := range m.items {
		if !removed[it.Path] {
			kept = append(kept, it)
		}
	}
	m.items = kept
	m.refresh()

	verb := "freed"
	if m.dryRun {
		verb = "would free (dry-run, nothing deleted)"
	}
	m.toast("%d item(s) processed — %s %s", done, verb, scanner.FormatBytes(m.freed))
	if failed > 0 {
		m.toast("%s; %d failed: %v", m.status, failed, firstErr)
	}
}

func (m Model) riskyCount(items []scanner.ScanResult) int {
	n := 0
	for _, it := range items {
		if it.Safety() != scanner.SafetySafe {
			n++
		}
	}
	return n
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
		return m.confirmKey(s)
	}

	switch s {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-m.rows())
	case "pgdown":
		m.move(m.rows())
	case "home", "g":
		m.move(-len(m.shown))
	case "end", "G":
		m.move(len(m.shown))
	case " ", "x", "space":
		if it, ok := m.cur(); ok {
			m.toggle(it)
			m.move(1) // advance so holding space sweeps down the list
		}
	case "a": // every SAFE item in view
		for _, idx := range m.shown {
			if it := m.items[idx]; it.Safety() == scanner.SafetySafe {
				m.selected[it.Path] = true
			}
		}
	case "A": // everything in view, risky included
		for _, idx := range m.shown {
			m.selected[m.items[idx].Path] = true
		}
	case "n":
		m.selected = make(map[string]bool)
	case "s":
		m.sortBy = sortSize
		m.refresh()
	case "i":
		m.sortBy = sortInactivity
		m.refresh()
	case "tab", "f", "right", "l":
		m.filter = (m.filter + 1) % numFilters
		m.refresh()
	case "shift+tab", "left", "h":
		m.filter = (m.filter + numFilters - 1) % numFilters
		m.refresh()
	case "1", "2", "3", "4", "5":
		m.filter = filter(s[0] - '1')
		m.refresh()
	case "d", "enter":
		if len(m.selected) == 0 {
			m.toast("Nothing selected — press space on an item (or 'a' for all SAFE)")
		} else {
			m.mode = modeConfirm
		}
	}
	return m, nil
}

func (m Model) confirmKey(s string) (tea.Model, tea.Cmd) {
	items := m.selectedItems()
	risky := m.riskyCount(items)
	switch {
	case s == "y" && risky == 0, s == "f" && risky > 0:
		m.mode = modeDeleting
		m.toast("Working…")
		return m, m.cleanCmd(items, risky > 0)
	case s == "y" && risky > 0: // delete only what is SAFE
		var safe []scanner.ScanResult
		for _, it := range items {
			if it.Safety() == scanner.SafetySafe {
				safe = append(safe, it)
			}
		}
		if len(safe) == 0 {
			m.mode = modeBrowse
			m.toast("Nothing SAFE in the selection — use 'f' to force or 'n' to cancel")
			return m, nil
		}
		m.mode = modeDeleting
		m.toast("Working…")
		return m, m.cleanCmd(safe, false)
	}
	m.mode = modeBrowse
	m.toast("Cancelled")
	return m, nil
}

func (m Model) cleanCmd(items []scanner.ScanResult, force bool) tea.Cmd {
	c := m.cleaner
	if force {
		c = c.WithAllowDirty()
	}
	ctx := m.ctx
	return func() tea.Msg { return cleanedMsg(c.CleanAll(ctx, items)) }
}

// Run starts the program and blocks until the user quits.
func Run(ctx context.Context, results <-chan scanner.ScanResult, c *cleaner.Cleaner, dryRun bool) error {
	_, err := tea.NewProgram(New(ctx, results, c, dryRun),
		tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx)).Run()
	return err
}
