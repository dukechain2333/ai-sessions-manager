package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeProviderScanTagsAgent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "-home-x", "aaaa.jsonl"), "{}\n")
	p := NewClaudeProvider(dir)
	if !p.Available() || p.Agent() != AgentClaude {
		t.Fatalf("available=%v agent=%v", p.Available(), p.Agent())
	}
	ss, err := p.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 1 || ss[0].Agent != AgentClaude {
		t.Fatalf("scan = %+v", ss)
	}
	name, args := p.ResumeCommand(ss[0])
	if name != "claude" || len(args) != 2 || args[0] != "--resume" {
		t.Errorf("resume = %s %v", name, args)
	}
}

func TestScanAllMergesByRecency(t *testing.T) {
	dir := t.TempDir()
	older := filepath.Join(dir, "-a", "old.jsonl")
	newer := filepath.Join(dir, "-b", "new.jsonl")
	writeFile(t, older, "{}\n")
	writeFile(t, newer, "{}\n")
	old := time.Now().Add(-time.Hour)
	// force mtimes
	touch(t, older, old)
	p := NewClaudeProvider(dir)
	ss, err := ScanAll([]Provider{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 2 || ss[0].ID != "new" {
		t.Fatalf("merge order = %+v", ss)
	}
	if ProviderFor([]Provider{p}, AgentClaude) == nil {
		t.Error("ProviderFor(claude) nil")
	}
	if ProviderFor([]Provider{p}, AgentCodex) != nil {
		t.Error("ProviderFor(codex) should be nil")
	}
}
