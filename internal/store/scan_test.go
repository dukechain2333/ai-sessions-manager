package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScan(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "-home-w-proja", "aaaa.jsonl")
	newer := filepath.Join(dir, "-home-w-projb", "bbbb.jsonl")
	trashed := filepath.Join(dir, ".trash", "-home-w-proja", "cccc.jsonl")
	writeFile(t, old, "{}\n")
	writeFile(t, newer, "{}\n")
	writeFile(t, trashed, "{}\n")
	os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))

	sessions, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2 (.trash excluded)", len(sessions))
	}
	if sessions[0].ID != "bbbb" || sessions[1].ID != "aaaa" {
		t.Errorf("not sorted by recency: %s, %s", sessions[0].ID, sessions[1].ID)
	}
	if sessions[0].Slug != "-home-w-projb" {
		t.Errorf("Slug = %q", sessions[0].Slug)
	}
}

func TestEnrich(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "-x", "s1.jsonl"),
		`{"type":"user","message":{"role":"user","content":"hello there"},"timestamp":"2026-07-01T10:00:00.000Z","cwd":"/tmp"}`+"\n")
	sessions, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan EnrichResult, len(sessions))
	Enrich(sessions, 2, results)
	n := 0
	for r := range results {
		n++
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		sessions[r.Index].Apply(r.Meta)
	}
	if n != 1 {
		t.Fatalf("got %d results, want 1", n)
	}
	if sessions[0].CWD != "/tmp" || sessions[0].UserMessages != 1 {
		t.Errorf("enriched session = %+v", sessions[0])
	}
}

func TestResolveSlug(t *testing.T) {
	root := t.TempDir()
	// Real directory containing a dash: home/william/hyper-sagnn
	if err := os.MkdirAll(filepath.Join(root, "home", "william", "hyper-sagnn"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ResolveSlug(root, "-home-william-hyper-sagnn")
	want := filepath.Join(root, "home", "william", "hyper-sagnn")
	if got != want {
		t.Errorf("ResolveSlug = %q, want %q", got, want)
	}
	if got := ResolveSlug(root, "-does-not-exist"); got != "" {
		t.Errorf("missing dir should resolve to \"\", got %q", got)
	}
}

func TestKnownDirs(t *testing.T) {
	real := t.TempDir()
	sessions := []Session{
		{CWD: real},
		{CWD: real}, // duplicate
		{CWD: "/nonexistent/xyz"},
		{CWD: ""},
	}
	dirs := KnownDirs(sessions)
	if len(dirs) != 1 || dirs[0] != real {
		t.Errorf("KnownDirs = %v", dirs)
	}
}
