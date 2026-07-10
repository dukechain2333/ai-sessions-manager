package ui

import (
	"strings"
	"testing"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

func TestRenderTranscript(t *testing.T) {
	tr := store.Transcript{Messages: []store.Message{
		{Kind: store.KindUser, Text: "make slides from my notes"},
		{Kind: store.KindAssistant, Text: "I'll read the notes file first."},
		{Kind: store.KindTool, Text: "Bash: Run tests"},
	}}
	out := renderTranscript(tr, 40, defaultStyles())
	for _, want := range []string{"› make slides", "● I'll read", "⚒ Bash: Run tests"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderTranscriptEmpty(t *testing.T) {
	out := renderTranscript(store.Transcript{}, 40, defaultStyles())
	if !strings.Contains(out, "no messages") {
		t.Errorf("empty transcript should say 'no messages', got:\n%s", out)
	}
}
