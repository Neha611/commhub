// Package ui holds presentation only. Everything it renders has already been
// sanitised at ingest; it re-sanitises anyway, because defence at the render
// boundary costs nothing and the cost of missing once is a hijacked terminal.
package ui

import "github.com/charmbracelet/lipgloss"

// Colours are chosen from the 256-colour cube so they survive on basic
// terminals, with adaptive pairs so the palette holds on light and dark
// backgrounds alike.
var (
	ColAccent  = lipgloss.AdaptiveColor{Light: "24", Dark: "44"}
	ColMuted   = lipgloss.AdaptiveColor{Light: "245", Dark: "243"}
	ColText    = lipgloss.AdaptiveColor{Light: "236", Dark: "252"}
	ColUrgent  = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	ColWarn    = lipgloss.AdaptiveColor{Light: "130", Dark: "179"}
	ColOK      = lipgloss.AdaptiveColor{Light: "28", Dark: "78"}
	ColBorder  = lipgloss.AdaptiveColor{Light: "252", Dark: "238"}
	ColBorderA = lipgloss.AdaptiveColor{Light: "24", Dark: "44"}
)

var (
	PaneTitle = lipgloss.NewStyle().Bold(true).Foreground(ColMuted).Padding(0, 1)

	PaneTitleActive = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(ColAccent).Padding(0, 1)

	Pane = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(ColBorder).Padding(0, 1)

	PaneActive = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(ColBorderA).Padding(0, 1)

	Row         = lipgloss.NewStyle().Foreground(ColText)
	RowSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(ColAccent).Bold(true)
	RowMuted    = lipgloss.NewStyle().Foreground(ColMuted)

	Sender  = lipgloss.NewStyle().Bold(true)
	Meta    = lipgloss.NewStyle().Foreground(ColMuted)
	Urgent  = lipgloss.NewStyle().Foreground(ColUrgent).Bold(true)
	Warn    = lipgloss.NewStyle().Foreground(ColWarn)
	OK      = lipgloss.NewStyle().Foreground(ColOK)
	Accent  = lipgloss.NewStyle().Foreground(ColAccent)
	Heading = lipgloss.NewStyle().Bold(true).Foreground(ColAccent)

	Footer = lipgloss.NewStyle().Foreground(ColMuted).Padding(0, 1)

	KeyCap = lipgloss.NewStyle().Bold(true).Foreground(ColAccent)

	Help = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(ColBorderA).Padding(1, 3)

	Empty = lipgloss.NewStyle().Foreground(ColMuted).Padding(2, 4)
)
