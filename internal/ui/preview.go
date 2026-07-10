package ui

import (
	"strings"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

// renderTranscript renders a transcript as styled, wrapped text for
// the preview viewport. Prefixes: › user, ● assistant, ⚒ tool call.
func renderTranscript(t store.Transcript, width int, st styles) string {
	if len(t.Messages) == 0 {
		return st.ListMeta.Render("no messages")
	}
	if width < 4 {
		width = 4
	}
	parts := make([]string, 0, len(t.Messages))
	for _, m := range t.Messages {
		var style = st.AssistantMsg
		prefix := "● "
		switch m.Kind {
		case store.KindUser:
			style, prefix = st.UserMsg, "› "
		case store.KindTool:
			style, prefix = st.ToolMsg, "⚒ "
		}
		parts = append(parts, style.Width(width).Render(prefix+m.Text))
	}
	return strings.Join(parts, "\n\n")
}
