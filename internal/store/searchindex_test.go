package store

import (
	"encoding/json"
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

func TestEnsureSessionFreshnessWithTabInPath(t *testing.T) {
	ix := testIndex(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "odd\tname.jsonl")
	line := `{"type":"user","message":{"role":"user","content":"tabbed path"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Skip("filesystem rejects tab in filename")
	}
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
		t.Error("EnsureSession must agree with Messages that a tabbed-path cache is fresh")
	}
	if _, fresh := ix.Messages(path); !fresh {
		t.Error("Messages must read the tabbed-path cache as fresh")
	}
}

func TestZeroMessageSessionRoundTrip(t *testing.T) {
	ix := testIndex(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "toolonly.jsonl")
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ix.EnsureSession(path); err != nil {
		t.Fatal(err)
	}
	msgs, fresh := ix.Messages(path)
	if !fresh || msgs == nil || len(msgs) != 0 {
		t.Errorf("tool-only session: msgs=%v fresh=%v, want empty slice and fresh", msgs, fresh)
	}
}

func writeCustomSession(t *testing.T, dir, name string, prompts ...string) string {
	t.Helper()
	path := filepath.Join(dir, name+".jsonl")
	var lines []string
	for _, p := range prompts {
		lines = append(lines, `{"type":"user","message":{"role":"user","content":`+jsonString(p)+`}}`)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestEnsureAllIndexesEverything(t *testing.T) {
	ix := testIndex(t)
	dir := t.TempDir()
	sessions := []Session{
		{Path: writeCustomSession(t, dir, "a", "alpha message")},
		{Path: writeCustomSession(t, dir, "b", "beta message")},
	}
	ch := make(chan IndexProgress, len(sessions))
	ix.EnsureAll(sessions, 2, ch)
	var last IndexProgress
	n := 0
	for p := range ch {
		n++
		last = p
	}
	if n != 2 || last.Done != 2 || last.Total != 2 {
		t.Fatalf("progress: n=%d last=%+v, want 2 messages ending 2/2", n, last)
	}
	for _, s := range sessions {
		if _, fresh := ix.Messages(s.Path); !fresh {
			t.Errorf("%s not indexed", s.Path)
		}
	}
}

func TestSearchAndSemanticsAndCounts(t *testing.T) {
	ix := testIndex(t)
	dir := t.TempDir()
	now := time.Now()
	sessions := []Session{
		{Path: writeCustomSession(t, dir, "a", "the webhook broke", "we fixed the payload", "unrelated chatter"), LastActivity: now},
		{Path: writeCustomSession(t, dir, "b", "webhook only, no second term"), LastActivity: now.Add(-time.Hour)},
		{Path: writeCustomSession(t, dir, "c", "payload only"), LastActivity: now.Add(-2 * time.Hour)},
	}
	for _, s := range sessions {
		if err := ix.EnsureSession(s.Path); err != nil {
			t.Fatal(err)
		}
	}
	hits, indexed := ix.Search("WEBHOOK payload", sessions)
	if indexed != 3 {
		t.Fatalf("indexed = %d, want 3", indexed)
	}
	if len(hits) != 1 || hits[0].Session != 0 {
		t.Fatalf("hits = %+v, want only session 0 (AND at session level)", hits)
	}
	if hits[0].MsgHits != 2 || hits[0].First != 0 {
		t.Errorf("MsgHits=%d First=%d, want 2 (msg0+msg1) and 0", hits[0].MsgHits, hits[0].First)
	}
}

func TestSearchOrderAndUnindexed(t *testing.T) {
	ix := testIndex(t)
	dir := t.TempDir()
	now := time.Now()
	sessions := []Session{
		{Path: writeCustomSession(t, dir, "one", "hit"), LastActivity: now.Add(-time.Hour)},
		{Path: writeCustomSession(t, dir, "two", "hit", "hit again"), LastActivity: now.Add(-2 * time.Hour)},
		{Path: writeCustomSession(t, dir, "three", "hit"), LastActivity: now},
		{Path: filepath.Join(dir, "never-indexed.jsonl")},
	}
	os.WriteFile(sessions[3].Path, []byte(`{"type":"user","message":{"role":"user","content":"hit"}}`+"\n"), 0o644)
	for _, s := range sessions[:3] {
		if err := ix.EnsureSession(s.Path); err != nil {
			t.Fatal(err)
		}
	}
	hits, indexed := ix.Search("hit", sessions)
	if indexed != 3 {
		t.Fatalf("indexed = %d, want 3 (the 4th has no cache)", indexed)
	}
	got := []int{}
	for _, h := range hits {
		got = append(got, h.Session)
	}
	// two (2 msg hits) first; then three vs one both 1 hit → recency (three newer)
	want := []int{1, 2, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestSplitTerms(t *testing.T) {
	if terms := SplitTerms("  Foo  BAR "); len(terms) != 2 || terms[0] != "foo" || terms[1] != "bar" {
		t.Errorf("SplitTerms = %v", terms)
	}
	if terms := SplitTerms("   "); len(terms) != 0 {
		t.Errorf("blank query → %v, want empty", terms)
	}
}
