package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Neha611/commhub/internal/safe"
	"github.com/Neha611/commhub/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// integration is one thing the user can connect from the welcome screen.
// Selecting it suspends the TUI and runs the corresponding wizard on the real
// terminal, because the OAuth flow needs to print URLs and read confirmations.
type integration struct {
	key       string
	name      string
	detail    string
	args      []string
	available bool
	note      string
}

func integrations() []integration {
	return []integration{
		{
			key: "google", name: "Google",
			detail:    "Gmail, Calendar and Meet — one sign-in covers all three",
			args:      []string{"connect", "google"},
			available: true,
		},
		{
			key: "slack", name: "Slack",
			detail:    "Planned for a later release",
			available: false,
			note:      "not yet",
		},
	}
}

// connectCmd hands the terminal to our own connect wizard, then reloads.
// Running the real subcommand rather than reimplementing it inside the TUI
// means there is exactly one connect flow to maintain and to get right.
func (m Model) connectCmd(in integration) tea.Cmd {
	self, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return statusMsg("cannot find my own binary: " + err.Error()) }
	}
	cmd := safe.Command(context.Background(), self, in.args...)
	cmd.Stdin = os.Stdin
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return statusMsg("connect exited: " + err.Error())
		}
		return reloadMsg{}
	})
}

func (m Model) welcomeView() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(ui.ColAccent).Render("CommHub")
	tagline := ui.Meta.Render("One terminal view of your mail, calendar and meetings.")

	blurb := []string{
		"Everything that needs your attention — from every account you connect —",
		"in a single ranked list, instead of a set of unread badges to compare.",
		"",
		ui.Meta.Render("Runs entirely on this machine. No backend, no hosted app, no account with us."),
		ui.Meta.Render("Credentials live in your OS keychain, and you can revoke them at any time."),
	}

	var rows []string
	rows = append(rows, title, tagline, "")
	rows = append(rows, blurb...)
	rows = append(rows, "", ui.Heading.Render("Connect a service"), "")

	for i, in := range integrations() {
		cursor := "  "
		name := in.name
		if i == m.optCursor {
			cursor = ui.Accent.Render("▸ ")
			name = ui.RowSelected.Render(" " + in.name + " ")
		} else if in.available {
			name = ui.Sender.Render(in.name)
		} else {
			name = ui.RowMuted.Render(in.name)
		}
		line := fmt.Sprintf("%s%-22s %s", cursor, name, ui.Meta.Render(in.detail))
		if !in.available {
			line += ui.Warn.Render("  " + in.note)
		}
		rows = append(rows, line)
	}

	hint := []string{
		ui.KeyCap.Render("j/k") + ui.Meta.Render(" move"),
		ui.KeyCap.Render("enter") + ui.Meta.Render(" connect"),
		ui.KeyCap.Render("q") + ui.Meta.Render(" quit"),
	}
	if m.Connected() {
		hint = append([]string{ui.KeyCap.Render("esc") + ui.Meta.Render(" back")}, hint...)
	}
	rows = append(rows, "", ui.Meta.Render(strings.Repeat("─", 62)), strings.Join(hint, "   "))

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		ui.Help.Render(strings.Join(rows, "\n")))
}

func (m Model) onWelcomeKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	opts := integrations()
	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		// Only escapable once something is connected; otherwise there is
		// nothing behind this screen to go back to.
		if m.Connected() {
			m.mode = modeNormal
		}
		return m, nil
	case "j", "down":
		if m.optCursor < len(opts)-1 {
			m.optCursor++
		}
		return m, nil
	case "k", "up":
		if m.optCursor > 0 {
			m.optCursor--
		}
		return m, nil
	case "enter", " ":
		in := opts[m.optCursor]
		if !in.available {
			m.flash(in.name + " is not available yet")
			return m, nil
		}
		return m, m.connectCmd(in)
	}
	return m, nil
}
