package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// displayText removes terminal instructions from untrusted session/config/error
// text. Styling is applied only after this boundary; source data and launch paths
// remain unchanged. Keep newlines and tabs for transcript formatting.
func displayText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

// displayLine flattens untrusted text and truncates by terminal cells, not
// Unicode code points: CJK and emoji must fit the same geometry as mouse input.
func displayLine(s string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Truncate(strings.Join(strings.Fields(displayText(s)), " "), width, "…")
}
