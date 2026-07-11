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
	for _, want := range []string{"> make slides", "⏺ I'll read", "⎿ Bash: Run tests"} {
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

func TestHighlightTermsOverlappingSubstrings(t *testing.T) {
	cases := []struct {
		name  string
		terms []string
		text  string
		want  string
	}{
		{"shorter first", []string{"cat", "cats"}, "the cats sat", "the \x1b[7mcats\x1b[27m sat"},
		{"longer first", []string{"cats", "cat"}, "the cats sat", "the \x1b[7mcats\x1b[27m sat"},
		{"prefix pair", []string{"test", "testing"}, "testing framework", "\x1b[7mtesting\x1b[27m framework"},
	}
	for _, c := range cases {
		if got := highlightTerms(c.text, c.terms); got != c.want {
			t.Errorf("%s: highlightTerms(%q, %v) = %q, want %q", c.name, c.text, c.terms, got, c.want)
		}
	}
}

func TestHighlightTermsLengthChangingRunes(t *testing.T) {
	// Ⱥ's lowercase is one byte LONGER (2→3): pre-guard this panicked.
	if got := highlightTerms("Ⱥx quick", []string{"quick"}); got != "Ⱥx quick" {
		t.Errorf("length-growing rune: got %q, want unhighlighted pass-through", got)
	}
	// İ's lowercase is one byte SHORTER (2→1): pre-guard this misaligned.
	if got := highlightTerms("İstanbul quick", []string{"quick"}); got != "İstanbul quick" {
		t.Errorf("length-shrinking rune: got %q, want unhighlighted pass-through", got)
	}
	// plain multi-byte text without case-length changes must still highlight
	if got := highlightTerms("你好 quick 世界", []string{"quick"}); got != "你好 \x1b[7mquick\x1b[27m 世界" {
		t.Errorf("CJK text must still highlight: got %q", got)
	}
}
