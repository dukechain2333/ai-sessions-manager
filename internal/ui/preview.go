package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

// renderTranscript renders a transcript as styled, wrapped text for the
// preview viewport, returning each message's first rendered line so hit
// navigation can jump by message. Prefixes: > user, ⏺ assistant, ⎿ tool call.
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
		prefix := "⏺ "
		switch m.Kind {
		case store.KindUser:
			style, prefix = st.UserMsg, "> "
		case store.KindTool:
			style, prefix = st.ToolMsg, "⎿ "
		}
		rendered := style.Width(width).Render(prefix + m.Text)
		starts = append(starts, line)
		line += lipgloss.Height(rendered) + 1 // +1 for the blank joiner line
		parts = append(parts, rendered)
	}
	return strings.Join(parts, "\n\n"), starts
}

// highlightTerms wraps every case-insensitive occurrence of each term in
// reverse-video toggles. All match spans are located against the ORIGINAL
// text and merged before any codes are inserted, so overlapping terms
// ("test", "testing") highlight as one contiguous region instead of
// corrupting each other's escape codes. lipgloss wraps ANSI-aware, so the
// inline codes survive width-wrapping; the closing toggle only clears
// reverse, leaving the message's own foreground styling intact.
func highlightTerms(text string, terms []string) string {
	lower := strings.ToLower(text)
	if len(lower) != len(text) {
		// ToLower changed some rune's byte length (İ, K, Ⱥ, …): byte
		// offsets in lower no longer map onto text, so slicing would
		// misalign or panic. Degrade to no highlight — hit detection and
		// jumps don't slice and are unaffected.
		return text
	}
	type span struct{ start, end int }
	var spans []span
	for _, term := range terms {
		if term == "" {
			continue
		}
		for pos := 0; ; {
			i := strings.Index(lower[pos:], term)
			if i < 0 {
				break
			}
			i += pos
			spans = append(spans, span{i, i + len(term)})
			pos = i + len(term)
		}
	}
	if len(spans) == 0 {
		return text
	}
	sort.Slice(spans, func(a, b int) bool { return spans[a].start < spans[b].start })
	merged := spans[:1]
	for _, s := range spans[1:] {
		last := &merged[len(merged)-1]
		if s.start <= last.end {
			if s.end > last.end {
				last.end = s.end
			}
			continue
		}
		merged = append(merged, s)
	}
	var b strings.Builder
	pos := 0
	for _, s := range merged {
		b.WriteString(text[pos:s.start])
		b.WriteString("\x1b[7m")
		b.WriteString(text[s.start:s.end])
		b.WriteString("\x1b[27m")
		pos = s.end
	}
	b.WriteString(text[pos:])
	return b.String()
}
