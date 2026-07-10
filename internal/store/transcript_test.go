package store

import (
	"path/filepath"
	"testing"
)

func TestParseTranscript(t *testing.T) {
	tr, err := ParseTranscript("testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	want := []Message{
		{KindUser, "Please fix the flaky test in parser_test.go"},
		{KindAssistant, "I'll look at the test first."},
		{KindTool, "Bash: Run tests"},
		{KindAssistant, "Fixed. The race was in setup."},
	}
	if len(tr.Messages) != len(want) {
		t.Fatalf("got %d messages, want %d: %+v", len(tr.Messages), len(want), tr.Messages)
	}
	for i, m := range want {
		if tr.Messages[i] != m {
			t.Errorf("message %d = %+v, want %+v", i, tr.Messages[i], m)
		}
	}
}

func TestTranscriptCache(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.jsonl")
	p2 := filepath.Join(dir, "b.jsonl")
	line := `{"type":"user","message":{"role":"user","content":"hi"},"timestamp":"2026-07-01T10:00:00.000Z"}` + "\n"
	writeFile(t, p1, line)
	writeFile(t, p2, line)

	c := NewTranscriptCache(1)
	if _, err := c.Get(p1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(p2); err != nil { // evicts p1 (capacity 1)
		t.Fatal(err)
	}
	if len(c.entries) != 1 {
		t.Errorf("cache holds %d entries, want 1", len(c.entries))
	}
	if _, err := c.Get("/nonexistent.jsonl"); err == nil {
		t.Error("missing file should error")
	}
}
