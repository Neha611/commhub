package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/Neha611/commhub/internal/safe"
	"github.com/Neha611/commhub/internal/store"
	"github.com/Neha611/commhub/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	if m.width == 0 {
		return "starting…"
	}
	if m.mode == modeWelcome {
		return m.welcomeView()
	}
	if !m.Connected() {
		return m.welcomeView()
	}
	if m.mode == modeHelp {
		return m.helpView()
	}

	bodyHeight := m.height - 3 // footer + status line
	if bodyHeight < 5 {
		bodyHeight = 5
	}

	cols := m.layout(bodyHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)

	return strings.Join([]string{body, m.footer(), m.statusLine()}, "\n")
}

// layout renders panes side by side when there is room, and only the active
// pane when there is not, so a narrow terminal degrades instead of wrapping.
func (m Model) layout(h int) []string {
	n := len(m.panes)
	minWidth := 34
	if m.width < minWidth*2 {
		return []string{m.renderPane(m.active, m.width-2, h)}
	}
	fit := m.width / minWidth
	if fit > n {
		fit = n
	}
	// Always include the active pane in the visible window.
	start := 0
	if m.active >= fit {
		start = m.active - fit + 1
	}
	each := (m.width / fit) - 2
	var out []string
	for i := start; i < start+fit && i < n; i++ {
		out = append(out, m.renderPane(i, each, h))
	}
	return out
}

func (m Model) renderPane(idx, w, h int) string {
	p := m.panes[idx]
	active := idx == m.active
	inner := w - 4
	if inner < 10 {
		inner = 10
	}

	title := p.title
	if p.id != panePriority {
		if n := m.counts[string(p.id)]; n > 0 {
			title = fmt.Sprintf("%s (%d)", title, n)
		}
	} else if m.search != "" {
		title = "Search: " + safe.Truncate(m.search, inner-10)
	}

	head := ui.PaneTitle.Render(title)
	if active {
		head = ui.PaneTitleActive.Render(title)
	}

	rows := make([]string, 0, h)
	rows = append(rows, head, "")

	visible := h - 4
	if visible < 1 {
		visible = 1
	}
	start := 0
	if p.cursor >= visible {
		start = p.cursor - visible + 1
	}

	if len(p.items) == 0 {
		msg := "nothing here"
		if m.search != "" {
			msg = "no matches"
		}
		rows = append(rows, ui.RowMuted.Render(msg))
	}

	for i := start; i < len(p.items) && i < start+visible; i++ {
		rows = append(rows, m.renderRow(p.items[i], i == p.cursor && active, inner, p.id))
	}

	style := ui.Pane
	if active {
		style = ui.PaneActive
	}
	return style.Width(w).Height(h).Render(strings.Join(rows, "\n"))
}

func (m Model) renderRow(it store.Ranked, selected bool, w int, pid paneID) string {
	var right string
	if it.StartsAt != nil {
		right = ui.Until(*it.StartsAt, m.now)
	} else {
		right = ui.Ago(it.Timestamp, m.now)
	}

	marker := " "
	switch {
	case it.StartsAt != nil && it.StartsAt.Sub(m.now) <= 15*time.Minute:
		marker = "!"
	case it.IsDirectToMe && it.Unread:
		marker = "@"
	case it.IsStarred:
		marker = "*"
	case it.Unread:
		marker = "•"
	}

	label := it.Sender
	if pid == paneID("calendar") || it.StartsAt != nil {
		label = it.Title
	}
	if it.ThreadCount > 1 {
		label = fmt.Sprintf("%s (%d)", label, it.ThreadCount)
	}

	// Reserve room for the marker, the right-hand time, and the gaps.
	labelW := w - len(right) - 4
	if labelW < 6 {
		labelW = 6
	}
	line := fmt.Sprintf("%s %-*s %s", marker, labelW, ui.Clip(label, labelW), right)

	if selected {
		return ui.RowSelected.Width(w).Render(line)
	}
	style := ui.Row
	if !it.Unread && it.StartsAt == nil {
		style = ui.RowMuted
	}
	if marker == "!" {
		style = ui.Urgent
	}
	return style.Render(line)
}

func (m Model) footer() string {
	var left string
	if m.next != nil && m.next.StartsAt != nil {
		when := ui.Until(*m.next.StartsAt, m.now)
		left = fmt.Sprintf("Next: %s  %s", ui.Clip(m.next.Title, 40), when)
		if m.next.StartsAt.Sub(m.now) <= 15*time.Minute {
			left = ui.Urgent.Render(left)
		} else {
			left = ui.Accent.Render(left)
		}
		if m.next.ActionURL != "" {
			left += ui.Meta.Render("   [") + ui.KeyCap.Render("J") + ui.Meta.Render(" to join]")
		}
	} else {
		left = ui.Meta.Render("No upcoming meetings")
	}
	if m.demo {
		left += ui.Warn.Render("      demo data — press ") +
			ui.KeyCap.Render("A") + ui.Warn.Render(" to connect a real account")
	}
	return ui.Footer.Width(m.width).Render(left)
}

func (m Model) statusLine() string {
	if m.needsReauth {
		return ui.Footer.Width(m.width).Render(
			ui.Urgent.Render("Google sign-in expired") +
				ui.Meta.Render(" — press ") + ui.KeyCap.Render("A") +
				ui.Meta.Render(" then Google to reconnect (about five seconds; your data is kept)"))
	}
	if m.mode == modeConfirmOpen {
		return ui.Footer.Width(m.width).Render(
			ui.Warn.Render("Open ") + ui.Heading.Render(safe.DisplayHost(m.confirmURL)) +
				ui.Warn.Render(" in your browser?  ") + ui.KeyCap.Render("y") +
				ui.Meta.Render(" confirm   any other key cancels"))
	}
	if m.mode == modeSearch {
		return ui.Footer.Width(m.width).Render(
			ui.Accent.Render("/") + m.search + ui.Meta.Render("▏  enter apply   esc clear"))
	}
	if m.err != nil {
		return ui.Footer.Width(m.width).Render(ui.Urgent.Render("error: " + m.err.Error()))
	}
	if m.status != "" && time.Now().Before(m.statusUntil) {
		return ui.Footer.Width(m.width).Render(ui.OK.Render(safe.Text(m.status)))
	}
	hints := []string{
		ui.KeyCap.Render("gp") + " priority", ui.KeyCap.Render("gm") + " mail",
		ui.KeyCap.Render("gc") + " calendar", ui.KeyCap.Render("j/k") + " nav",
		ui.KeyCap.Render("enter") + " open", ui.KeyCap.Render("J") + " join",
		ui.KeyCap.Render("r") + " reply",
		ui.KeyCap.Render("m") + " read", ui.KeyCap.Render("/") + " search",
		ui.KeyCap.Render("A") + " add account",
		ui.KeyCap.Render("?") + " help", ui.KeyCap.Render("q") + " quit",
	}
	return ui.Footer.Width(m.width).Render(ui.Meta.Render(strings.Join(hints, "  ")))
}

func (m Model) helpView() string {
	rows := [][2]string{
		{"j / k, ↓ / ↑", "move within a pane"},
		{"tab / shift-tab", "next / previous pane"},
		{"gp / gm / gc", "jump to Priority, Mail, Calendar"},
		{"gg / G", "first / last item"},
		{"enter, o", "open the selected item's link (confirms the host first)"},
		{"J", "join the next meeting"},
		{"r", "reply — opens $EDITOR with a scrubbed environment"},
		{"m", "mark read"},
		{"/", "search the local cache"},
		{"R", "sync now"},
		{"esc", "clear search or close this help"},
		{"A", "add another account"},
		{"?", "toggle this help"},
		{"q, ctrl-c", "quit"},
	}
	var b strings.Builder
	b.WriteString(ui.Heading.Render("Keys") + "\n\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("  %s  %s\n", ui.KeyCap.Render(fmt.Sprintf("%-16s", r[0])), ui.Meta.Render(r[1])))
	}
	b.WriteString("\n" + ui.Meta.Render("Ranking: meetings rise as they approach; messages decay with age.") + "\n")
	b.WriteString(ui.Meta.Render("Weights live under [priority] in config.toml."))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, ui.Help.Render(b.String()))
}
