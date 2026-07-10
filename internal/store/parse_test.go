package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseMetadataUsesOriginCWD(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "moved.jsonl")
	// Session starts in /home/w/proj, then cd's into a subdirectory. Claude
	// filed it under /home/w/proj, so resume must target the origin, not the
	// last-seen cwd.
	content := `{"type":"user","message":{"role":"user","content":"start here"},"uuid":"u1","timestamp":"2026-07-01T10:00:00.000Z","cwd":"/home/w/proj"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]},"uuid":"a1","timestamp":"2026-07-01T10:00:05.000Z","cwd":"/home/w/proj"}
{"type":"user","message":{"role":"user","content":"now in a subdir"},"uuid":"u2","timestamp":"2026-07-01T10:00:10.000Z","cwd":"/home/w/proj/sub"}
`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ParseMetadata(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.CWD != "/home/w/proj" {
		t.Errorf("CWD = %q, want /home/w/proj (session origin, not the later subdir)", m.CWD)
	}
}

func TestParseMetadata(t *testing.T) {
	m, err := ParseMetadata("testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Fix the flaky test" {
		t.Errorf("Title = %q", m.Title)
	}
	if m.FirstPrompt != "Please fix the flaky test in parser_test.go" {
		t.Errorf("FirstPrompt = %q", m.FirstPrompt)
	}
	if m.CWD != "/home/william/demo" {
		t.Errorf("CWD = %q", m.CWD)
	}
	if m.GitBranch != "main" {
		t.Errorf("GitBranch = %q", m.GitBranch)
	}
	if m.UserMessages != 1 {
		t.Errorf("UserMessages = %d, want 1 (meta + tool_result records excluded)", m.UserMessages)
	}
	if m.TotalMessages != 3 {
		t.Errorf("TotalMessages = %d, want 3", m.TotalMessages)
	}
	want := time.Date(2026, 7, 1, 10, 0, 20, 0, time.UTC)
	if !m.LastActivity.Equal(want) {
		t.Errorf("LastActivity = %v, want %v", m.LastActivity, want)
	}
}

func TestParseMetadataEmptySession(t *testing.T) {
	m, err := ParseMetadata("testdata/empty.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if m.UserMessages != 0 {
		t.Errorf("UserMessages = %d, want 0", m.UserMessages)
	}
	s := Session{}
	s.Apply(m)
	if !s.Empty() {
		t.Error("session with 0 user messages should be Empty")
	}
}

func TestParseMetadataTitleFallback(t *testing.T) {
	m, err := ParseMetadata("testdata/untitled.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title == "" || len([]rune(m.Title)) > 60 {
		t.Errorf("Title fallback = %q, want first prompt truncated to 60 runes", m.Title)
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("hello   world", 20); got != "hello world" {
		t.Errorf("whitespace collapse: %q", got)
	}
	if got := Truncate("abcdefgh", 5); got != "abcd…" {
		t.Errorf("truncation: %q", got)
	}
}
