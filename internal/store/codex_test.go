package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCodexParseMetadata(t *testing.T) {
	m, err := NewCodexProvider(t.TempDir()).ParseMetadata("codex_testdata/rollout-basic.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if m.CWD != "/home/w/proj" {
		t.Errorf("CWD = %q", m.CWD)
	}
	if m.Title != "Identify the bug in this project" {
		t.Errorf("Title = %q (want first real user prompt)", m.Title)
	}
	if m.FirstPrompt != "Identify the bug in this project" {
		t.Errorf("FirstPrompt = %q", m.FirstPrompt)
	}
	if m.UserMessages != 1 {
		t.Errorf("UserMessages = %d, want 1 (developer + <context> excluded)", m.UserMessages)
	}
	if m.TotalMessages != 2 {
		t.Errorf("TotalMessages = %d, want 2 (1 user + 1 assistant)", m.TotalMessages)
	}
	want := time.Date(2026, 6, 26, 3, 52, 34, 743000000, time.UTC)
	if !m.LastActivity.Equal(want) {
		t.Errorf("LastActivity = %v, want %v (session_meta timestamp)", m.LastActivity, want)
	}
}

func TestCodexEmptySession(t *testing.T) {
	m, err := NewCodexProvider(t.TempDir()).ParseMetadata("codex_testdata/rollout-empty.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if m.UserMessages != 0 {
		t.Errorf("UserMessages = %d, want 0", m.UserMessages)
	}
	s := Session{Agent: AgentCodex}
	s.Apply(m)
	if !s.Empty() {
		t.Error("session with no real user prompt should be Empty")
	}
}

func TestCodexScan(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "2026", "06", "26",
		"rollout-2026-06-26T03-52-34-019f020e-d6ab-7ff2-99b4-c3274454ea14.jsonl")
	writeFile(t, f, "{}\n")
	p := NewCodexProvider(dir)
	if !p.Available() {
		t.Fatal("Available() should be true when dir exists")
	}
	ss, err := p.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 1 {
		t.Fatalf("scanned %d, want 1", len(ss))
	}
	if ss[0].Agent != AgentCodex {
		t.Errorf("Agent = %v", ss[0].Agent)
	}
	if ss[0].ID != "019f020e-d6ab-7ff2-99b4-c3274454ea14" {
		t.Errorf("ID = %q (want trailing UUID)", ss[0].ID)
	}
}
