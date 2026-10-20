package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jossssdl/molt/internal/scanner"
)

var (
	accent = lipgloss.Color("141")
	green  = lipgloss.Color("42")
	red    = lipgloss.Color("203")
	amber  = lipgloss.Color("214")
	gray   = lipgloss.Color("241")

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(accent).Padding(0, 1)
	modeStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(amber).Padding(0, 1)
	liveStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(red).Padding(0, 1)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236"))
	cursorBg    = lipgloss.NewStyle().Background(lipgloss.Color("238"))
	dimStyle    = lipgloss.NewStyle().Foreground(gray)
	boldStyle   = lipgloss.NewStyle().Bold(true)
	warnStyle   = lipgloss.NewStyle().Bold(true).Foreground(amber)
	okStyle     = lipgloss.NewStyle().Foreground(green)
	badStyle    = lipgloss.NewStyle().Foreground(red)
	tabOn       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(accent).Padding(0, 1)
	tabOff      = lipgloss.NewStyle().Foreground(gray).Padding(0, 1)

	safeBadge  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(green)
	dirtyBadge = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(red)
	unkBadge   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(amber)
)

// Column widths (cells). The badge column is fixed so rows align.
const (
	wMark  = 2
	wCheck = 2
	wSize  = 10
	wBadge = 11
	wType  = 13
	wIdle  = 6
)

func badge(r scanner.ScanResult) string {
	var text string
	var st lipgloss.Style
	switch r.Safety() {
	case scanner.SafetySafe:
		text, st = "SAFE", safeBadge
	case scanner.SafetyDirtyGit:
		text, st = "DIRTY GIT", dirtyBadge
	default:
		text, st = "UNKNOWN", unkBadge
	}
	return st.Render(" " + pad(text, wBadge-2) + " ")
}

// ago renders a coarse "time since" such as "3d" or "5mo".
func ago(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return "<1h"
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 2*365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	}
}

// shorten replaces the home directory with ~ and keeps the tail of long paths.
func shorten(p string, w int) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		p = "~" + p[len(home):]
	}
	r := []rune(p)
	if w <= 1 || len(r) <= w {
		return p
	}
	return "…" + string(r[len(r)-(w-1):])
}

func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func padLeft(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// bar draws capacity split into selected / other safe / rest.
func bar(width int, selected, safe, total int64) string {
	if width < 4 {
		return ""
	}
	if total <= 0 {
		return dimStyle.Render(strings.Repeat("░", width))
	}
	cells := func(v int64) int { return int(float64(v) / float64(total) * float64(width)) }
	sel := min(width, cells(selected))
	other := max(0, min(width-sel, cells(safe)-sel))
	rest := width - sel - other
	return lipgloss.NewStyle().Foreground(accent).Render(strings.Repeat("█", sel)) +
		okStyle.Render(strings.Repeat("█", other)) +
		dimStyle.Render(strings.Repeat("░", rest))
}

// View renders the whole screen. Every line is clipped to the terminal width
// and the output never exceeds the terminal height, so nothing wraps or
// scrolls the header off screen.
func (m Model) View() string {
	w := max(40, m.width)
	lines := make([]string, 0, m.height)
	add := func(s string) { lines = append(lines, s) }

	// 1. title
	title := titleStyle.Render("molt")
	if m.dryRun {
		title += " " + modeStyle.Render("DRY-RUN")
	} else {
		title += " " + liveStyle.Render("LIVE")
	}
	if m.scanning {
		title += "  " + m.spin.View() + dimStyle.Render(fmt.Sprintf("scanning… %d found", len(m.items)))
	} else {
		title += "  " + dimStyle.Render(fmt.Sprintf("scan complete · %d found", len(m.items)))
	}
	add(title)

	// 2. numbers
	var total, safe int64
	counts := [numFilters]int{}
	for _, it := range m.items {
		total += it.SizeBytes
		if it.Safety() == scanner.SafetySafe {
			safe += it.SizeBytes
		}
		for f := filterAll; f < numFilters; f++ {
			if passes(f, it) {
				counts[f]++
			}
		}
	}
	selItems := m.selectedItems()
	sel := sumBytes(selItems)
	add(fmt.Sprintf("%s %s  %s %s  %s %s  %s",
		dimStyle.Render("total"), boldStyle.Render(scanner.FormatBytes(total)),
		dimStyle.Render("reclaimable (SAFE)"), okStyle.Bold(true).Render(scanner.FormatBytes(safe)),
		dimStyle.Render("selected"), lipgloss.NewStyle().Bold(true).Foreground(accent).Render(scanner.FormatBytes(sel)),
		dimStyle.Render(fmt.Sprintf("(%d items)", len(selItems)))))

	// 3. bar
	add(bar(min(w-1, 80), sel, safe, total))

	// 4. filter tabs
	var tabs []string
	for f := filterAll; f < numFilters; f++ {
		label := fmt.Sprintf("%d %s %d", int(f)+1, filterNames[f], counts[f])
		if f == m.filter {
			tabs = append(tabs, tabOn.Render(label))
		} else {
			tabs = append(tabs, tabOff.Render(label))
		}
	}
	sortName := "size"
	if m.sortBy == sortInactivity {
		sortName = "idle time"
	}
	add(strings.Join(tabs, "") + dimStyle.Render("  sort: "+sortName))

	// 5. column header
	pathW := max(10, w-wMark-wCheck-wSize-wBadge-wType-wIdle-6)
	add(headerStyle.Render(pad("", wMark+wCheck) + padLeft("SIZE", wSize) + "  " + pad("STATUS", wBadge) +
		pad("TYPE", wType) + padLeft("IDLE", wIdle) + "  " + pad("PATH", pathW+2)))

	// rows
	now := m.now()
	rows := m.rows()
	if len(m.shown) == 0 {
		msg := "Scanning… results appear here as they are found."
		if !m.scanning {
			msg = "Nothing in this view. Press tab or 1-5 to change the filter."
		}
		add(dimStyle.Render("  " + msg))
	}
	end := min(len(m.shown), m.offset+rows)
	for k := m.offset; k < end; k++ {
		it := m.items[m.shown[k]]
		mark, check := "  ", "○ "
		if m.selected[it.Path] {
			check = okStyle.Bold(true).Render("● ")
		}
		if k == m.cursor {
			mark = lipgloss.NewStyle().Foreground(accent).Bold(true).Render("▌ ")
		}
		line := mark + check + padLeft(scanner.FormatBytes(it.SizeBytes), wSize) + "  " +
			badge(it) + " " + pad(it.Target.Name(), wType) +
			padLeft(ago(activity(it), now), wIdle) + "  " + rowName(it, pathW)
		line = lipgloss.NewStyle().MaxWidth(w).Render(line)
		if k == m.cursor {
			line = cursorBg.Render(pad(line, w))
		}
		add(line)
	}
	for len(lines) < headerLines+rows {
		add("")
	}

	// footer: separator, 3 detail lines, status, help
	add(dimStyle.Render(strings.Repeat("─", w-1)))
	if it, ok := m.cur(); ok {
		_, l2, l3 := detailLines(it)
		add(boldStyle.Render(shorten(it.Path, w-2)))
		add(l2)
		add(l3)
	} else {
		add("")
		add("")
		add("")
	}
	add(m.statusLine())
	add(dimStyle.Render("↑↓ move · space select · a all SAFE · A all · n none · tab filter · s/i sort · d delete · q quit · mouse ok"))

	for i, l := range lines {
		lines[i] = lipgloss.NewStyle().MaxWidth(w).Render(l)
	}
	if len(lines) > m.height && m.height > 0 {
		lines = lines[:m.height]
	}
	return strings.Join(lines, "\n")
}

// rowName is what the PATH column shows: the target's own label when it has
// one (for example "HF Model: org/name"), otherwise the shortened path.
func rowName(it scanner.ScanResult, w int) string {
	if l := it.Label(); l != "" {
		return shorten(l, w)
	}
	return shorten(it.Path, w)
}

// detailLines builds the panel under the table: the label (if any), the exact
// reasons behind the safety verdict, and size/repository facts.
func detailLines(it scanner.ScanResult) (label, why, meta string) {
	label = it.Label()
	switch {
	case it.Git == nil:
		why = okStyle.Render("SAFE") + dimStyle.Render(" · no git check needed for this target")
	case it.Safety() == scanner.SafetySafe && it.Git.IsRepo:
		why = okStyle.Render("SAFE") + dimStyle.Render(" · repo is clean and fully pushed")
	case it.Safety() == scanner.SafetySafe:
		why = okStyle.Render("SAFE") + dimStyle.Render(" · not inside a git repository")
	default:
		verdict := "DIRTY GIT"
		if it.Safety() == scanner.SafetyUnknown {
			verdict = "UNKNOWN"
		}
		why = badStyle.Bold(true).Render(verdict) + dimStyle.Render(" · ") +
			badStyle.Render(strings.Join(it.Git.Details(), "; "))
	}
	parts := []string{fmt.Sprintf("%d files", it.FileCount)}
	if it.Git != nil && it.Git.IsRepo {
		parts = append(parts, "repo "+it.Git.Root)
		if !it.Git.LastCommit.IsZero() {
			parts = append(parts, "last commit "+it.Git.LastCommit.Format("2006-01-02"))
		}
	}
	if label != "" {
		parts = append([]string{label}, parts...)
	}
	if it.Err != nil {
		parts = append(parts, warnStyle.Render("partial size: "+it.Err.Error()))
	}
	meta = dimStyle.Render(strings.Join(parts, " · "))
	return label, why, meta
}

func (m Model) statusLine() string {
	if m.mode == modeConfirm {
		items := m.selectedItems()
		risky := m.riskyCount(items)
		verb := "Delete"
		if m.dryRun {
			verb = "Simulate deleting"
		}
		size := scanner.FormatBytes(sumBytes(items))
		if risky == 0 {
			return warnStyle.Render(fmt.Sprintf("%s %d item(s), %s?  [y] yes · [n] cancel", verb, len(items), size))
		}
		return warnStyle.Render(fmt.Sprintf(
			"%d of %d selected are DIRTY/UNKNOWN.  [y] only the %d SAFE · [f] ALL %d (%s) · [n] cancel",
			risky, len(items), len(items)-risky, len(items), size))
	}
	if m.status != "" {
		return warnStyle.Render(m.status)
	}
	return ""
}
