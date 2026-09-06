package safe

import (
	"strings"
	"unicode/utf8"
)

// Text sanitises attacker-controlled text for display on a single line.
// Newlines and tabs collapse to spaces.
//
// Everything CommHub renders is attacker-controlled: mail subjects, sender
// display names, snippets, and calendar event titles, since anyone can send an
// unsolicited invite. See SEC-09.
func Text(s string) string { return sanitize(s, false) }

// Block sanitises multi-line content, preserving newlines only.
func Block(s string) string { return sanitize(s, true) }

func sanitize(s string, keepNewlines bool) string {
	var b strings.Builder
	b.Grow(len(s))

	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]

		// ESC introduces a control sequence. Consume and discard the whole
		// sequence, not just the ESC, or the payload would render as literal
		// text (e.g. "[31m").
		if r == 0x1B {
			i = skipEscape(rs, i)
			continue
		}
		// C1 has single-character equivalents of the same introducers. Dropping
		// the introducer alone would leave its payload as visible text, so
		// consume those sequences too.
		if r == 0x9B { // CSI
			i = skipCSI(rs, i+1)
			continue
		}
		if r == 0x9D || r == 0x90 || r == 0x9E || r == 0x9F { // OSC, DCS, PM, APC
			i = skipUntilST(rs, i+1)
			continue
		}

		switch {
		case r == '\n':
			if keepNewlines {
				b.WriteRune('\n')
			} else {
				b.WriteRune(' ')
			}
		case r == '\t':
			b.WriteRune(' ')
		case r < 0x20, r == 0x7F: // C0 and DEL
			// dropped
		case r >= 0x80 && r <= 0x9F: // C1, includes single-byte CSI/OSC forms
			// dropped
		case isInvisible(r):
			// dropped
		case r == utf8.RuneError:
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// skipEscape returns the index of the final rune of the escape sequence that
// starts at i, so the caller's loop increment lands past it.
func skipEscape(rs []rune, i int) int {
	if i+1 >= len(rs) {
		return i
	}
	switch rs[i+1] {
	case '[': // CSI: parameters then a final byte in 0x40..0x7E
		return skipCSI(rs, i+2)
	case ']': // OSC: terminated by BEL or ST. Covers OSC 8 (hyperlinks) and
		// OSC 52 (clipboard write) — the paste-jacking primitive.
		return skipUntilST(rs, i+2)
	case 'P', 'X', '^', '_': // DCS, SOS, PM, APC
		return skipUntilST(rs, i+2)
	default:
		return i + 1 // two-character escape
	}
}

func skipCSI(rs []rune, from int) int {
	for j := from; j < len(rs); j++ {
		if rs[j] >= 0x40 && rs[j] <= 0x7E {
			return j
		}
	}
	return len(rs)
}

func skipUntilST(rs []rune, from int) int {
	for j := from; j < len(rs); j++ {
		if rs[j] == 0x07 || rs[j] == 0x9C { // BEL or single-byte ST
			return j
		}
		if rs[j] == 0x1B && j+1 < len(rs) && rs[j+1] == '\\' { // ESC backslash
			return j + 1
		}
	}
	return len(rs)
}

func isInvisible(r rune) bool {
	switch r {
	case 0x00AD, // soft hyphen
		0x200B, 0x200C, 0x200D, // zero-width space/non-joiner/joiner
		0x2060, 0xFEFF, // word joiner, BOM
		0x200E, 0x200F: // LTR/RTL marks
		return true
	}
	// Bidi overrides and isolates: text that renders in a different order than
	// it is stored, so a displayed sender or URL can lie about itself.
	if r >= 0x202A && r <= 0x202E {
		return true
	}
	if r >= 0x2066 && r <= 0x2069 {
		return true
	}
	// Unassigned planes and private use.
	if r >= 0xE000 && r <= 0xF8FF {
		return true
	}
	return false
}

// Truncate shortens display text to n runes, appending an ellipsis. It assumes
// its input has already been sanitised.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(rs[:n-1]) + "…"
}
