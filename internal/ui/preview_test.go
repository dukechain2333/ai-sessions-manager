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
	out, _ := renderTranscript(tr, 40, defaultStyles())
	for _, want := range []string{"› make slides", "● I'll read", "⚒ Bash: Run tests"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderTranscriptEmpty(t *testing.T) {
	out, _ := renderTranscript(store.Transcript{}, 40, defaultStyles())
	if !strings.Contains(out, "no messages") {
		t.Errorf("empty transcript should say 'no messages', got:\n%s", out)
	}
}

func TestRenderTranscriptMessageOffsets(t *testing.T) {
	tr := store.Transcript{Messages: []store.Message{
		{Kind: store.KindUser, Text: "one"},
		{Kind: store.KindAssistant, Text: "two"},
		{Kind: store.KindUser, Text: "three"},
	}}
	out, starts := renderTranscript(tr, 40, defaultStyles())
	if len(starts) != 3 {
		t.Fatalf("starts = %v, want 3 entries", starts)
	}
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[starts[1]], "two") {
		t.Errorf("starts[1]=%d does not point at message two: %q", starts[1], lines[starts[1]])
	}
	if !strings.Contains(lines[starts[2]], "three") {
		t.Errorf("starts[2]=%d does not point at message three: %q", starts[2], lines[starts[2]])
	}
}

func TestHighlightTerms(t *testing.T) {
	got := highlightTerms("The Webhook broke the webhook", []string{"webhook"})
	want := "The \x1b[7mWebhook\x1b[27m broke the \x1b[7mwebhook\x1b[27m"
	if got != want {
		t.Errorf("highlight = %q, want %q", got, want)
	}
	if highlightTerms("nothing here", []string{"absent"}) != "nothing here" {
		t.Error("no-match text must pass through unchanged")
	}
}
