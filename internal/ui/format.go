package ui

import (
	"fmt"
	"time"

	"github.com/Neha611/commhub/internal/safe"
)

// Ago renders a compact relative time for list rows.
func Ago(t time.Time, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Format("2 Jan")
	}
}

// Until renders a countdown for meetings, which is the one number people
// actually look at.
func Until(t time.Time, now time.Time) string {
	d := t.Sub(now)
	if d < 0 {
		return "now"
	}
	if d < time.Hour {
		return fmt.Sprintf("in %d min", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h < 24 {
		if m == 0 {
			return fmt.Sprintf("in %dh", h)
		}
		return fmt.Sprintf("in %dh %02dm", h, m)
	}
	return "on " + t.Format("Mon 15:04")
}

// Clip sanitises and truncates in one step, so no render path can forget the
// first half.
func Clip(s string, n int) string { return safe.Truncate(safe.Text(s), n) }
