package safe

import (
	"strings"
	"testing"
)

func TestTextStripsEscapeSequences(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain", "Quarterly review", "Quarterly review"},
		{"csi colour", "\x1b[31mURGENT\x1b[0m", "URGENT"},
		{"csi clear screen", "before\x1b[2Jafter", "beforeafter"},
		{"osc 8 hyperlink", "\x1b]8;;https://evil.example\x07click\x1b]8;;\x07", "click"},
		{"osc 52 clipboard write", "hi\x1b]52;c;ZXZpbA==\x07 there", "hi there"},
		{"osc terminated by ST", "a\x1b]0;title\x1b\\b", "ab"},
		{"dcs", "a\x1bPq#0;2;0;0;0\x1b\\b", "ab"},
		{"c0 controls", "a\x00b\x07c", "abc"},
		{"c1 csi consumes its sequence", "a\u009bm b", "a b"},
		{"c1 osc", "a\u009d0;title\u009cb", "ab"},
		{"zero width", "pay\u200bpal.com", "paypal.com"},
		{"bidi override", "invoice\u202egnp.exe", "invoicegnp.exe"},
		{"rtl isolate", "\u2066spoof\u2069", "spoof"},
		{"newline collapses", "line1\nline2", "line1 line2"},
		{"soft hyphen", "ad\u00admin", "admin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Text(c.in); got != c.want {
				t.Fatalf("Text(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestTextNeverLeavesControlCharacters(t *testing.T) {
	// Anything the sanitiser emits must be safe to hand to the renderer.
	inputs := []string{
		"\x1b[38;2;255;0;0mred", "\x1b]0;retitle\x07", "\x1b_apc\x1b\\",
		"\x1bX sos \x1b\\", "\x1b^pm\x1b\\", "\x1b[?1049h", "\x1b", "\x1b[",
		"mixed\x1b[1mbold\x00\x07\u200b\u202e",
	}
	for _, in := range inputs {
		out := Text(in)
		for _, r := range out {
			if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) {
				t.Fatalf("Text(%q) leaked control rune %U in %q", in, r, out)
			}
		}
	}
}

func TestBlockKeepsNewlines(t *testing.T) {
	got := Block("a\nb\x1b[31mc")
	if !strings.Contains(got, "\n") {
		t.Fatalf("Block dropped newlines: %q", got)
	}
	if strings.Contains(got, "\x1b") {
		t.Fatalf("Block leaked escape: %q", got)
	}
}

func FuzzTextIsAlwaysRenderSafe(f *testing.F) {
	f.Add("\x1b[31mhello")
	f.Add("\x1b]52;c;AAA\x07")
	f.Add("plain text")
	f.Fuzz(func(t *testing.T, s string) {
		out := Text(s)
		for _, r := range out {
			if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) {
				t.Fatalf("control rune %U survived sanitising of %q", r, s)
			}
		}
	})
}
