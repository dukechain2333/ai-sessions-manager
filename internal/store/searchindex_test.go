package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeSessionFixture writes a minimal .jsonl with one user prompt, one
// assistant text block, and one tool_use block (which must NOT be indexed).
func writeSessionFixture(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "fixture-session.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"find the webhook clobber bug"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"The webhook payload overwrites serials."},{"type":"tool_use","name":"Bash","input":{"command":"grep -r TOOLNOISE"}}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func testIndex(t *testing.T) SearchIndex {
	t.Helper()
	return SearchIndex{Dir: t.TempDir()}
}

func TestEnsureSessionBuildsAndReads(t *testing.T) {
	ix := testIndex(t)
	path := writeSessionFixture(t, t.TempDir())
	if err := ix.EnsureSession(path); err != nil {
		t.Fatal(err)
	}
	msgs, fresh := ix.Messages(path)
	if !fresh {
		t.Fatal("cache should be fresh right after EnsureSession")
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2 (user + assistant text)", len(msgs))
	}
	if msgs[0] != "find the webhook clobber bug" {
		t.Errorf("msg0 = %q", msgs[0])
	}
	joined := strings.Join(msgs, "\n")
	if strings.Contains(joined, "TOOLNOISE") || strings.Contains(joined, "Bash") {
		t.Error("tool_use content must not be indexed")
	}
}

func TestEnsureSessionSkipsFresh(t *testing.T) {
	ix := testIndex(t)
	path := writeSessionFixture(t, t.TempDir())
	if err := ix.EnsureSession(path); err != nil {
		t.Fatal(err)
	}
	cache := ix.cacheFile(path)
	st1, _ := os.Stat(cache)
	time.Sleep(10 * time.Millisecond)
	if err := ix.EnsureSession(path); err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(cache)
	if !st2.ModTime().Equal(st1.ModTime()) {
		t.Error("fresh cache must not be rewritten")
	}
}

func TestEnsureSessionRebuildsOnSourceChange(t *testing.T) {
	ix := testIndex(t)
	dir := t.TempDir()
	path := writeSessionFixture(t, dir)
	if err := ix.EnsureSession(path); err != nil {
		t.Fatal(err)
	}
	// append a new prompt; size and mtime both change
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"user","message":{"role":"user","content":"second prompt"}}` + "\n")
	f.Close()
	if _, fresh := ix.Messages(path); fresh {
		t.Fatal("changed source must invalidate the cache")
	}
	if err := ix.EnsureSession(path); err != nil {
		t.Fatal(err)
	}
	msgs, fresh := ix.Messages(path)
	if !fresh || len(msgs) != 3 {
		t.Errorf("after rebuild: fresh=%v msgs=%d, want true/3", fresh, len(msgs))
	}
}

func TestMessagesCorruptCacheIsStale(t *testing.T) {
	ix := testIndex(t)
	path := writeSessionFixture(t, t.TempDir())
	if err := ix.EnsureSession(path); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(ix.cacheFile(path), []byte("garbage no tabs\nbody"), 0o644)
	if _, fresh := ix.Messages(path); fresh {
		t.Error("corrupt cache must read as stale")
	}
	if err := ix.EnsureSession(path); err != nil {
		t.Fatal(err)
	}
	if _, fresh := ix.Messages(path); !fresh {
		t.Error("EnsureSession must rebuild a corrupt cache")
	}
}

func TestMessagesMissingSource(t *testing.T) {
	ix := testIndex(t)
	if _, fresh := ix.Messages(filepath.Join(t.TempDir(), "nope.jsonl")); fresh {
		t.Error("missing source must not read fresh")
	}
	if err := ix.EnsureSession(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Error("EnsureSession on a missing source must error")
	}
}
