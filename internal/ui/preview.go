package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

// renderTranscript renders a transcript as styled, wrapped text for the
// preview viewport, returning each message's first rendered line so hit
// navigation can jump by message. Prefixes: › user, ● assistant, ⚒ tool.
func renderTranscript(t store.Transcript, width int, st styles) (string, []int) {
	if len(t.Messages) == 0 {
		return st.ListMeta.Render("no messages"), nil
	}
	if width < 4 {
		width = 4
	}
	parts := make([]string, 0, len(t.Messages))
	starts := make([]int, 0, len(t.Messages))
	line := 0
	for _, m := range t.Messages {
		var style = st.AssistantMsg
		prefix := "● "
		switch m.Kind {
		case store.KindUser:
			style, prefix = st.UserMsg, "› "
		case store.KindTool:
			style, prefix = st.ToolMsg, "⚒ "
		}
		rendered := style.Width(width).Render(prefix + m.Text)
		starts = append(starts, line)
		line += lipgloss.Height(rendered) + 1 // +1 for the blank joiner line
		parts = append(parts, rendered)
	}
	return strings.Join(parts, "\n\n"), starts
}

// highlightTerms wraps every case-insensitive occurrence of each term in
// reverse-video toggles. lipgloss wraps ANSI-aware, so the inline codes
// survive width-wrapping; the closing toggle only clears reverse, leaving
// the message's own foreground styling intact.
func highlightTerms(text string, terms []string) string {
	for _, term := range terms {
		if term == "" {
			continue
		}
		lower := strings.ToLower(text)
		var b strings.Builder
		pos := 0
		for {
			i := strings.Index(lower[pos:], term)
			if i < 0 {
				b.WriteString(text[pos:])
				break
			}
			i += pos
			b.WriteString(text[pos:i])
			b.WriteString("\x1b[7m")
			b.WriteString(text[i : i+len(term)])
			b.WriteString("\x1b[27m")
			pos = i + len(term)
		}
		text = b.String()
	}
	return text
}
