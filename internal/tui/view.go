package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jossssdl/molt/internal/scanner"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	cursorStyle = lipgloss.NewStyle().Background(lipgloss.Color("237"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	safeBadge   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("42")).Padding(0, 1)
	dirtyBadge  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("203")).Padding(0, 1)
	unkBadge    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("214")).Padding(0, 1)
	warnStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	barFull     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	barSel      = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	barEmpty    = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)

const badgeWidth = 11 // "DIRTY GIT" + padding

func badge(r scanner.ScanResult) string {
	switch r.Safety() {
	case scanner.SafetySafe:
		return safeBadge.Render("SAFE")
	case scanner.SafetyDirtyGit:
		return dirtyBadge.Render("DIRTY GIT")
	default:
		return unkBadge.Render("UNKNOWN")
	}
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

// truncLeft keeps the tail of a path (the informative part) within w cells.
func truncLeft(s string, w int) string {
	r := []rune(s)
	if w <= 1 || len(r) <= w {
		return s
	}
	return "…" + string(r[len(r)-(w-1):])
}

func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// bar draws total capacity split into selected / other safe / unsafe.
func bar(width int, selected, safe, total int64) string {
	if total <= 0 || width < 4 {
		return strings.Repeat("░", max(width, 0))
	}
	cells := func(v int64) int { return int(float64(v) / float64(total) * float64(width)) }
	sel := cells(selected)
	other := max(0, cells(safe)-sel)
	rest := max(0, width-sel-other)
	return barSel.Render(strings.Repeat("█", sel)) +
		barFull.Render(strings.Repeat("█", other)) +
		barEmpty.Render(strings.Repeat("░", rest))
}

// View renders the whole screen.
func (m Model) View() string {
	var b strings.Builder

	mode := "DRY-RUN"
	if !m.dryRun {
		mode = "LIVE"
	}
	title := titleStyle.Render("molt") + dimStyle.Render("  mode: "+mode)
	if m.scanning {
		title += "  " + m.spin.View() + dimStyle.Render(fmt.Sprintf("scanning… %d found", len(m.items)))
	}
	b.WriteString(title + "\n\n")

	const (
		sizeW, idleW, tgtW = 10, 6, 14
		checkW             = 4
	)
	pathW := max(10, m.width-checkW-sizeW-badgeWidth-tgtW-idleW-6)

	sortMark := func(k sortKey) string {
		if m.sortBy == k {
			return "▼"
		}
		return ""
	}
	b.WriteString(headerStyle.Render(
		pad("", checkW)+pad("SIZE"+sortMark(sortSize), sizeW)+pad("STATUS", badgeWidth+1)+
			pad("TARGET", tgtW)+pad("IDLE"+sortMark(sortInactivity), idleW)+" PATH") + "\n")

	now := m.now()
	rows := m.visibleRows()
	end := min(len(m.items), m.offset+rows)
	for i := m.offset; i < end; i++ {
		it := m.items[i]
		check := "[ ] "
		switch {
		case m.selected[it.Path]:
			check = "[x] "
		case !selectable(it):
			check = " -  "
		}
		line := check + pad(scanner.FormatBytes(it.SizeBytes), sizeW) +
			pad(badge(it), badgeWidth+1) + pad(it.Target.Name(), tgtW) +
			pad(ago(activity(it), now), idleW) + " " + truncLeft(it.Path, pathW)
		if i == m.cursor {
			line = cursorStyle.Render(pad(line, m.width))
		}
		b.WriteString(line + "\n")
	}
	if len(m.items) == 0 {
		msg := "no reclaimable items found yet"
		if !m.scanning {
			msg = "nothing to clean"
		}
		b.WriteString(dimStyle.Render("  "+msg) + "\n")
	}
	for i := end - m.offset; i < rows && len(m.items) > 0; i++ {
		b.WriteString("\n")
	}

	// Footer: details of the cursor item, space bar, status, help.
	b.WriteString("\n")
	if len(m.items) > 0 {
		it := m.items[m.cursor]
		detail := it.Path
		if d := it.Detail(); d != "" {
			detail += "  —  " + d
		}
		if it.Err != nil {
			detail += "  (partial: " + it.Err.Error() + ")"
		}
		b.WriteString(dimStyle.Render(truncLeft(detail, m.width-1)) + "\n")
	} else {
		b.WriteString("\n")
	}

	var total, safe int64
	for _, it := range m.items {
		total += it.SizeBytes
		if selectable(it) {
			safe += it.SizeBytes
		}
	}
	sel := sumBytes(m.selectedItems())
	barW := max(10, min(40, m.width-60))
	b.WriteString(fmt.Sprintf("%s  selected %s · safe %s · total %s\n",
		bar(barW, sel, safe, total), scanner.FormatBytes(sel),
		scanner.FormatBytes(safe), scanner.FormatBytes(total)))

	switch {
	case m.mode == modeConfirm:
		verb := "DELETE"
		if m.dryRun {
			verb = "simulate deleting"
		}
		b.WriteString(warnStyle.Render(fmt.Sprintf("%s %d item(s), %s? [y/N]",
			verb, len(m.selected), scanner.FormatBytes(sel))) + "\n")
	case m.status != "":
		b.WriteString(warnStyle.Render(m.status) + "\n")
	default:
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("↑/↓ move · space select · a all SAFE · n none · s size · i idle · d delete · q quit"))
	return b.String()
}
