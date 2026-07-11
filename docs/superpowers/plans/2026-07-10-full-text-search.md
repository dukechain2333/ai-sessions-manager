# Full-Text Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two search layers on the filter bar — the existing title fuzzy filter, and a full-text layer over indexed message text — switchable with Tab (or clicking the 🔍 icon), with hit-jump + highlight in the preview.

**Architecture:** A lazily built plain-text index cache (`os.UserCacheDir()/sm-index/`, one file per session, validity key `path\tmtime\tsize`) is extracted by streaming `ParseTranscript` and filtering out tool messages. The UI adds a `searchAll` layer on the filter input: debounced async searches over the cache, a search-results mode in the list pane (hits-desc order, `· N hits` meta), and preview hit navigation (`n`/`N`) with inline reverse-video term highlighting.

**Tech Stack:** Go 1.24, Bubble Tea v1.3.10, lipgloss v1.1.0 (ANSI-aware wrapping makes inline `\x1b[7m…\x1b[27m` highlight safe), stdlib `crypto/sha1`.

**Spec:** `docs/superpowers/specs/2026-07-10-full-text-search-design.md`

## Global Constraints

- No new dependencies — `go.mod` must not change (sha1/hex are stdlib).
- Existing keyboard behavior unchanged EXCEPT the two additions the spec names: Tab while the filter is focused (currently a dead key) toggles the layer; `n`/`N` with the preview focused and a full-text query active navigate hits (currently unbound in the viewport keymap).
- Full-text matching: case-insensitive substring; space-separated terms AND at session level; a message "hits" if it contains ≥1 term; hit counts and `n`/`N` granularity = messages. Tool messages (`store.KindTool`) are excluded from BOTH the index and the preview hit scan.
- Index cache: `os.UserCacheDir()/sm-index/<sha1-hex-of-session-path>.txt`; line 1 = `path\tmtimeUnixNano\tsize`; body = messages joined by `\n\x1e\n`; writes are atomic (temp file + rename); stale/corrupt caches rebuild silently.
- Debounce: `150 * time.Millisecond`; stale results (older seq) are dropped.
- Placeholders: `filter…` (title layer) ⇄ `search…` (full-text layer). Esc clears the query, blurs, AND resets to the title layer.
- All work on branch `feat/search` in `~/Desktop/ai-sessions-manager`. Commit per task with trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`. CI gate per task: `gofmt -l .` empty, `go vet ./...`, `go test -race ./...`.
- Tests use small synthetic fixtures only — never the user's real transcripts.
- Test fixture facts (`newTestModel()`: 100×30, sessions s1/alpha, s2/beta, s3 empty/hidden): filter row y=1, list content x∈[1,38].

---

### Task 1: `store.SearchIndex` — per-session cache build and read

**Files:**
- Create: `internal/store/searchindex.go`
- Test: `internal/store/searchindex_test.go`

**Interfaces:**
- Consumes: `ParseTranscript(path)` (existing; streams; `Message{Kind, Text}` with `KindUser/KindAssistant/KindTool`).
- Produces (Tasks 2/4 rely on these exact names):
  - `type SearchIndex struct { Dir string }`
  - `func NewSearchIndex() (SearchIndex, error)` — `os.UserCacheDir()/sm-index`, MkdirAll 0o755.
  - `func (ix SearchIndex) EnsureSession(path string) error` — skip when fresh, else rebuild atomically.
  - `func (ix SearchIndex) Messages(path string) ([]string, bool)` — cached messages + freshness; `(nil, false)` when missing/stale/corrupt.
  - `const indexMsgSep = "\n\x1e\n"`

- [ ] **Step 1: Write the failing tests**

Create `internal/store/searchindex_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/Desktop/ai-sessions-manager && go test ./internal/store/ -run 'TestEnsureSession|TestMessages' -v`
Expected: compile error — `SearchIndex` undefined.

- [ ] **Step 3: Implement**

Create `internal/store/searchindex.go`:

```go
package store

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// indexMsgSep joins extracted messages in a cache file. \x1e (record
// separator) cannot occur in message text, so splitting is unambiguous.
const indexMsgSep = "\n\x1e\n"

// SearchIndex is a per-session plain-text cache of message content, used
// by the full-text search layer. One file per session under Dir; line 1 is
// the validity key "path\tmtimeUnixNano\tsize", the body is the messages
// joined by indexMsgSep. Tool messages are excluded at extraction time.
type SearchIndex struct {
	Dir string
}

// NewSearchIndex places the cache under the platform user-cache dir.
func NewSearchIndex() (SearchIndex, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return SearchIndex{}, err
	}
	dir := filepath.Join(base, "sm-index")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SearchIndex{}, err
	}
	return SearchIndex{Dir: dir}, nil
}

func (ix SearchIndex) cacheFile(sessionPath string) string {
	sum := sha1.Sum([]byte(sessionPath))
	return filepath.Join(ix.Dir, hex.EncodeToString(sum[:])+".txt")
}

func validityKey(sessionPath string) (string, error) {
	st, err := os.Stat(sessionPath)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\t%d\t%d", sessionPath, st.ModTime().UnixNano(), st.Size()), nil
}

// EnsureSession makes the cache file for one session fresh: a no-op when
// the validity key matches, otherwise a streaming re-extract written
// atomically (temp file + rename) so a failed extraction never leaves a
// half-indexed session behind.
func (ix SearchIndex) EnsureSession(sessionPath string) error {
	key, err := validityKey(sessionPath)
	if err != nil {
		return err
	}
	if cur, ok := ix.readKey(sessionPath); ok && cur == key {
		return nil
	}
	tr, err := ParseTranscript(sessionPath)
	if err != nil {
		return err
	}
	var texts []string
	for _, m := range tr.Messages {
		if m.Kind == KindTool {
			continue
		}
		texts = append(texts, m.Text)
	}
	tmp, err := os.CreateTemp(ix.Dir, "tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.WriteString(key + "\n" + strings.Join(texts, indexMsgSep))
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(tmp.Name(), ix.cacheFile(sessionPath)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// readKey returns the cache file's header line. Freshness is decided by
// the callers' direct comparison against the recomputed validity key —
// any corruption simply fails that comparison and triggers a rebuild.
func (ix SearchIndex) readKey(sessionPath string) (string, bool) {
	data, err := os.ReadFile(ix.cacheFile(sessionPath))
	if err != nil {
		return "", false
	}
	head, _, _ := strings.Cut(string(data), "\n")
	return head, true
}

// Messages returns the cached message texts for a session and whether the
// cache is fresh (validity key matches the live file). Stale, corrupt, or
// missing caches — and missing sources — return (nil, false).
func (ix SearchIndex) Messages(sessionPath string) ([]string, bool) {
	key, err := validityKey(sessionPath)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(ix.cacheFile(sessionPath))
	if err != nil {
		return nil, false
	}
	head, body, _ := strings.Cut(string(data), "\n")
	if head != key {
		return nil, false
	}
	if body == "" {
		return []string{}, true
	}
	return strings.Split(body, indexMsgSep), true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestEnsureSession|TestMessages' -v` then `go test ./...`
Expected: all PASS.

- [ ] **Step 5: Full gate + commit**

```bash
gofmt -l . && go vet ./... && go test -race ./...
git add internal/store/searchindex.go internal/store/searchindex_test.go
git commit -m "feat(store): per-session plain-text search index cache"
```

---

### Task 2: `EnsureAll` + `Search`

**Files:**
- Modify: `internal/store/searchindex.go` (append)
- Test: `internal/store/searchindex_test.go` (append)

**Interfaces:**
- Consumes: Task 1's `SearchIndex`, `Messages`, `EnsureSession`; `Session{Path, LastActivity}`; the worker pattern from `Enrich` (`internal/store/scan.go:57`).
- Produces (Task 4 relies on these exact names):
  - `type IndexProgress struct { Done, Total int; Err error }`
  - `func (ix SearchIndex) EnsureAll(sessions []Session, workers int, results chan<- IndexProgress)` — one message per session (Done increments), channel closed when finished.
  - `type SessionHits struct { Session int; MsgHits int; First int }` (Session = index into the sessions slice; First = first hit message index).
  - `func (ix SearchIndex) Search(query string, sessions []Session) (hits []SessionHits, indexed int)` — AND terms at session level; hits sorted MsgHits desc, then LastActivity desc, then Session asc; `indexed` counts sessions with fresh caches.
  - `func SplitTerms(q string) []string` — lower-cased fields; empty slice for blank queries.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/searchindex_test.go`:

```go
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
```

Add `"encoding/json"` to the test file's imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestEnsureAll|TestSearch|TestSplitTerms' -v`
Expected: compile error — `IndexProgress`/`Search`/`SplitTerms` undefined.

- [ ] **Step 3: Implement**

Append to `internal/store/searchindex.go` (add `"sort"`, `"sync"` imports):

```go
// IndexProgress is one EnsureAll progress tick: Done sessions out of Total.
type IndexProgress struct {
	Done, Total int
	Err         error
}

// EnsureAll freshens the cache for every session concurrently, sending one
// IndexProgress per session and closing results when done — the same
// shape as Enrich, so the UI can reuse its channel-pump pattern.
func (ix SearchIndex) EnsureAll(sessions []Session, workers int, results chan<- IndexProgress) {
	if workers < 1 {
		workers = 1
	}
	paths := make([]string, len(sessions))
	for i, s := range sessions {
		paths[i] = s.Path
	}
	jobs := make(chan int)
	done := make(chan error)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				done <- ix.EnsureSession(paths[i])
			}
		}()
	}
	go func() {
		for i := range paths {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(done)
	}()
	go func() {
		n := 0
		for err := range done {
			n++
			results <- IndexProgress{Done: n, Total: len(paths), Err: err}
		}
		close(results)
	}()
}

// SessionHits is one full-text search result: the session's index in the
// slice handed to Search, how many messages hit, and the first hit message.
type SessionHits struct {
	Session int
	MsgHits int
	First   int
}

// SplitTerms lower-cases and splits a query on whitespace.
func SplitTerms(q string) []string {
	return strings.Fields(strings.ToLower(q))
}

// Search runs a case-insensitive AND-of-terms search over the cached
// message text of sessions. Sessions without a fresh cache are skipped
// (indexed reports how many were searchable). Hits are ordered by message
// hit count desc, then recency desc, then slice order.
func (ix SearchIndex) Search(query string, sessions []Session) (hits []SessionHits, indexed int) {
	terms := SplitTerms(query)
	if len(terms) == 0 {
		return nil, 0
	}
	for si, s := range sessions {
		msgs, fresh := ix.Messages(s.Path)
		if !fresh {
			continue
		}
		indexed++
		h := SessionHits{Session: si, First: -1}
		remaining := make(map[string]bool, len(terms))
		for _, t := range terms {
			remaining[t] = true
		}
		for mi, m := range msgs {
			lower := strings.ToLower(m)
			hit := false
			for _, t := range terms {
				if strings.Contains(lower, t) {
					hit = true
					delete(remaining, t)
				}
			}
			if hit {
				h.MsgHits++
				if h.First < 0 {
					h.First = mi
				}
			}
		}
		if h.MsgHits > 0 && len(remaining) == 0 { // every term appeared somewhere
			hits = append(hits, h)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].MsgHits != hits[j].MsgHits {
			return hits[i].MsgHits > hits[j].MsgHits
		}
		return sessions[hits[i].Session].LastActivity.After(sessions[hits[j].Session].LastActivity)
	})
	return hits, indexed
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -v` — all PASS (including Task 1's).

- [ ] **Step 5: Full gate + commit**

```bash
gofmt -l . && go vet ./... && go test -race ./...
git add internal/store/searchindex.go internal/store/searchindex_test.go
git commit -m "feat(store): concurrent index build and AND-term full-text search"
```

---

### Task 3: list pane search-results mode

**Files:**
- Modify: `internal/ui/listpane.go`
- Test: `internal/ui/listpane_test.go` (append)

**Interfaces:**
- Consumes: `store.SessionHits` (Task 2), existing `refresh()` / `rows` / meta rendering.
- Produces (Task 4 relies on): `func (l *listPane) SetSearchResults(hits []store.SessionHits)` — non-nil puts the pane in search-results mode (flat, given order, `· N hits` on the meta line, headers/grouping/fuzzy-filter suppressed); nil restores normal behavior. `l.searchHits(sessionIdx) int` helper returns the hit count for a session (0 when absent/off).

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/listpane_test.go`:

```go
func TestSearchResultsMode(t *testing.T) {
	l := newTestPane()
	l.ToggleGroup() // grouped, to prove search mode overrides grouping
	l.SetSearchResults([]store.SessionHits{
		{Session: 1, MsgHits: 3, First: 0}, // s2 first (more hits)
		{Session: 0, MsgHits: 1, First: 2},
	})
	if got := l.Len(); got != 2 {
		t.Fatalf("Len = %d, want 2", got)
	}
	if s, _, ok := l.Selected(); !ok || s.ID != "s2" {
		t.Fatalf("first result should be selected, got %v", s.ID)
	}
	for _, r := range l.rows {
		if r.header {
			t.Fatal("search mode must not render project headers")
		}
	}
	view := l.View()
	if !strings.Contains(view, "· 3 hits") {
		t.Errorf("meta line should show hit count, view:\n%s", view)
	}
	l.SetSearchResults(nil)
	if got := l.Len(); got != 2 {
		t.Errorf("clearing search restores normal view, Len = %d", got)
	}
	if len(l.rows) == 0 || !l.rows[0].header {
		t.Error("grouped headers should be back after clearing")
	}
}

func TestRemoveSessionAdjustsSearchResults(t *testing.T) {
	l := newTestPane()
	l.SetSearchResults([]store.SessionHits{
		{Session: 1, MsgHits: 3}, // s2
		{Session: 0, MsgHits: 1}, // s1
	})
	l.RemoveSession(0) // drop s1: s2's index shifts from 1 to 0
	if got := l.Len(); got != 1 {
		t.Fatalf("Len = %d, want 1 after removing a result", got)
	}
	if s, _, ok := l.Selected(); !ok || s.ID != "s2" {
		t.Errorf("remaining result should be s2, got %v", s.ID)
	}
	if n := l.searchHits(0); n != 3 {
		t.Errorf("s2's hits must follow its shifted index, got %d", n)
	}
}

func TestSearchResultsSingularHit(t *testing.T) {
	l := newTestPane()
	l.SetSearchResults([]store.SessionHits{{Session: 0, MsgHits: 1}})
	if view := l.View(); !strings.Contains(view, "· 1 hit ") && !strings.HasSuffix(strings.TrimRight(view, " \n"), "· 1 hit") && !strings.Contains(view, "· 1 hit\n") {
		t.Errorf("singular form wanted, view:\n%s", view)
	}
}
```

Add `"strings"` and the `store` import to the test file if not present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run TestSearchResults -v`
Expected: compile error — `SetSearchResults` undefined.

- [ ] **Step 3: Implement**

In `internal/ui/listpane.go`:

1. Add a field to `listPane`: `search []store.SessionHits` (add the `store` import alias already present).
2. Add after `SetFilter`:

```go
// SetSearchResults switches the pane to full-text results: sessions in the
// given order, flat, with hit counts on the meta line. nil switches back.
func (l *listPane) SetSearchResults(hits []store.SessionHits) {
	l.search = hits
	l.cursor = 0
	l.lineOffset = 0
	l.refresh()
	l.cursorToFirstSession()
}

// searchHits returns the full-text hit count for a session index, 0 when
// search mode is off or the session is not in the results.
func (l *listPane) searchHits(sessionIdx int) int {
	for _, h := range l.search {
		if h.Session == sessionIdx {
			return h.MsgHits
		}
	}
	return 0
}
```

Also extend `RemoveSession` — deleting a session while search results are
showing must not leave stale indices behind (every `h.Session` above the
removed index shifts down by one; the removed session's own hit vanishes).
Append to the end of `RemoveSession`, after the existing `l.refresh()`:

```go
	if l.search != nil {
		kept := l.search[:0]
		for _, h := range l.search {
			switch {
			case h.Session == i:
				continue
			case h.Session > i:
				h.Session--
			}
			kept = append(kept, h)
		}
		l.search = kept
		l.refresh()
	}
```

(Order matters: the first `refresh()` ran with stale search entries; the
second one, inside the guard, rebuilds rows from the corrected set. Keeping
both calls is simpler than restructuring the existing function.)

3. In `refresh()`, at the very top of the row-building phase, branch on search mode. The existing structure is: build `base` (filter or all), count per project, then grouped/flat rows. Insert BEFORE the `base` selection:

```go
	if l.search != nil {
		l.rows = l.rows[:0]
		l.counts = map[string]int{}
		l.total = 0
		for _, h := range l.search {
			if h.Session < 0 || h.Session >= len(l.sessions) {
				continue
			}
			l.total++
			l.rows = append(l.rows, row{project: l.sessions[h.Session].Project(), session: h.Session})
		}
		if l.cursor >= len(l.rows) {
			l.cursor = len(l.rows) - 1
		}
		if l.cursor < 0 {
			l.cursor = 0
		}
		l.ensureVisible()
		return
	}
```

4. In the meta-line rendering inside `View()` (the line that joins project/humanTime/branch for a session row), append the hit suffix. Find where the meta string is assembled and add, immediately after it is built:

```go
		if n := l.searchHits(r.session); n > 0 {
			meta += " · " + fmt.Sprintf("%d hit", n)
			if n != 1 {
				meta += "s"
			}
		}
```

(Adapt the variable name to the actual local; add `"fmt"` import if missing. If the meta line is built inline rather than via a variable, introduce a `meta := …` local first — smallest change that allows the suffix.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v` — all PASS (search tests + every pre-existing test; grouping/filter behavior must be untouched when `search == nil`).

- [ ] **Step 5: Full gate + commit**

```bash
gofmt -l . && go vet ./... && go test -race ./...
git add internal/ui/listpane.go internal/ui/listpane_test.go
git commit -m "feat(ui): list pane search-results mode with hit counts"
```

---

### Task 4: model — search layer, debounce, index orchestration

**Files:**
- Modify: `internal/ui/model.go`
- Test: `internal/ui/mouse_test.go`? No — create `internal/ui/search_test.go`

**Interfaces:**
- Consumes: Tasks 1-3 (`store.NewSearchIndex`, `EnsureAll`, `Search`, `SplitTerms`, `IndexProgress`, `SetSearchResults`); existing focusFilter key branch; `waitEnrich` channel-pump pattern.
- Produces (Tasks 5/6 rely on):
  - Model fields: `searchAll bool`, `searchSeq int`, `activeQuery string`, `matched int`, `index store.SearchIndex`, `indexErr error`, `indexReady, indexing bool`, `indexDone, indexTotal int`, `indexCh chan store.IndexProgress`.
  - `func (m *Model) toggleSearchLayer() tea.Cmd` — flips layer, swaps placeholder, clears the other layer's list state, dispatches the right search; shared by Tab (this task) and the 🔍 click (Task 6).
  - `func (m *Model) dispatchSearch() tea.Cmd` — debounce entry: bumps `searchSeq`, returns a `tea.Tick(searchDebounce, …)` producing `searchTickMsg{seq}`.
  - Msg types: `searchTickMsg{seq int}`, `searchResultMsg{seq int; hits []store.SessionHits; indexed int}`, `indexProgressMsg{p store.IndexProgress; ch chan store.IndexProgress}`, `indexDoneMsg{ch chan store.IndexProgress}`.
  - `const searchDebounce = 150 * time.Millisecond`
  - Title bar states: `N sessions[ · M matched[…]][ · indexing k/n…]`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/search_test.go`:

```go
package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

// searchModel returns a model whose sessions point at real tiny .jsonl
// files and whose index lives in a temp dir, with the full-text layer on.
func searchModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel()
	m.index = store.SearchIndex{Dir: t.TempDir()}
	m.indexErr = nil
	dir := t.TempDir()
	write := func(name string, texts ...string) string {
		p := filepath.Join(dir, name+".jsonl")
		var b strings.Builder
		for _, text := range texts {
			b.WriteString(`{"type":"user","message":{"role":"user","content":"` + text + `"}}` + "\n")
		}
		if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// s1: one hit message; s2: two hit messages (MsgHits counts MESSAGES,
	// so s2 must outrank s1 despite s1 being more recent).
	m.list.sessions[0].Path = write("s1", "the quick brown fox")
	m.list.sessions[1].Path = write("s2", "quick one", "quick two")
	for _, s := range m.list.sessions[:2] {
		if err := m.index.EnsureSession(s.Path); err != nil {
			t.Fatal(err)
		}
	}
	m.indexReady = true
	return m
}

func typeInto(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m2, _ := m.Update(key(string(r)))
		m = m2.(Model)
	}
	return m
}

func TestTabTogglesSearchLayer(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	if !m.searchAll {
		t.Fatal("tab in the filter must enable the full-text layer")
	}
	if m.filterInput.Placeholder != "search…" {
		t.Errorf("placeholder = %q, want search…", m.filterInput.Placeholder)
	}
	if m.focus != focusFilter {
		t.Error("layer toggle must keep the filter focused")
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	if m.searchAll || m.filterInput.Placeholder != "filter…" {
		t.Error("second tab must switch back to the title layer")
	}
}

func TestEscResetsToTitleLayer(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m = typeInto(t, m, "quick")
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	if m.searchAll {
		t.Error("esc must reset to the title layer")
	}
	if m.list.search != nil {
		t.Error("esc must clear search results")
	}
	if m.filterInput.Value() != "" || m.focus != focusList {
		t.Error("esc keeps its existing clear+blur behavior")
	}
}

func TestSearchAllDoesNotFuzzyFilter(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m = typeInto(t, m, "zzznothing")
	if m.list.filter != "" {
		t.Error("typing in the full-text layer must not drive the fuzzy filter")
	}
}

func TestSearchPipelineEndToEnd(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m = typeInto(t, m, "quick")
	// drive the debounce deterministically: fire the tick for the live seq
	m2, cmd := m.Update(searchTickMsg{seq: m.searchSeq})
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("live tick must return the async search cmd")
	}
	msg := cmd() // run the search synchronously
	res, ok := msg.(searchResultMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want searchResultMsg", msg)
	}
	m2, _ = m.Update(res)
	m = m2.(Model)
	if m.matched != 2 {
		t.Fatalf("matched = %d, want 2", m.matched)
	}
	if s, _, ok := m.list.Selected(); !ok || s.ID != "s2" {
		t.Errorf("s2 has more hits and must rank first, got %v", s.ID)
	}
	if !strings.Contains(m.View(), "· 2 matched") {
		t.Error("title bar must show the matched count")
	}
}

func TestStaleTickAndResultIgnored(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m = typeInto(t, m, "qu")   // seq bumps per keystroke
	stale := m.searchSeq - 1   // an older debounce tick
	m2, cmd := m.Update(searchTickMsg{seq: stale})
	m = m2.(Model)
	if cmd != nil {
		t.Error("stale tick must be dropped")
	}
	m2, _ = m.Update(searchResultMsg{seq: stale, hits: []store.SessionHits{{Session: 0, MsgHits: 9}}})
	m = m2.(Model)
	if m.list.search != nil {
		t.Error("stale result must be dropped")
	}
}

func TestIndexingProgressShownAndSearchRedispatched(t *testing.T) {
	m := searchModel(t)
	m.indexReady = false
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m = typeInto(t, m, "quick")
	m2, cmd := m.Update(searchTickMsg{seq: m.searchSeq})
	m = m2.(Model)
	if !m.indexing || cmd == nil {
		t.Fatal("first full-text search must kick off indexing plus the partial search")
	}
	ch := m.indexCh
	m2, _ = m.Update(indexProgressMsg{p: store.IndexProgress{Done: 1, Total: 2}, ch: ch})
	m = m2.(Model)
	if !strings.Contains(m.View(), "indexing 1/2…") {
		t.Errorf("title bar must show indexing progress, view head: %.120s", m.View())
	}
	m2, cmd = m.Update(indexDoneMsg{ch: ch})
	m = m2.(Model)
	if !m.indexReady || m.indexing {
		t.Error("indexDoneMsg must mark the index ready")
	}
	if cmd == nil {
		t.Error("index completion must re-dispatch the search")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestTabToggles|TestEscResets|TestSearchAll|TestSearchPipeline|TestStaleTick|TestIndexing' -v`
Expected: compile error — `m.index`/`searchTickMsg` undefined.

- [ ] **Step 3: Implement**

In `internal/ui/model.go`:

1. Imports: add `"time"` is already present (Task 4 of mouse feature); add nothing else.
2. Model struct — append fields:

```go
	// full-text search layer
	searchAll   bool
	searchSeq   int
	activeQuery string
	matched     int
	index       store.SearchIndex
	indexErr    error
	indexReady  bool
	indexing    bool
	indexDone   int
	indexTotal  int
	indexCh     chan store.IndexProgress
```

3. In `New()`: after the existing literal fields,

```go
	m := Model{ /* existing literal unchanged */ }
```

becomes: keep the literal as-is, then before `return`:

```go
	ret := Model{ /* …existing fields exactly as today… */ }
	ret.index, ret.indexErr = store.NewSearchIndex()
	return ret
```

(Mechanically: assign the composite literal to a local, set `index`/`indexErr`, return the local.)

4. Constants and msg types (near the other msg types):

```go
const searchDebounce = 150 * time.Millisecond

type (
	searchTickMsg   struct{ seq int }
	searchResultMsg struct {
		seq  int
		hits []store.SessionHits
	}
	indexProgressMsg struct {
		p  store.IndexProgress
		ch chan store.IndexProgress
	}
	indexDoneMsg struct{ ch chan store.IndexProgress }
)
```

5. Layer toggle + debounce + search cmds (new methods):

```go
// toggleSearchLayer flips between the title fuzzy filter and the full-text
// layer. Shared by Tab in the filter and the 🔍 icon click.
func (m *Model) toggleSearchLayer() tea.Cmd {
	if m.indexErr != nil {
		m.dialog = dialogError
		m.errText = "search index unavailable: " + m.indexErr.Error()
		return nil
	}
	m.searchAll = !m.searchAll
	if m.searchAll {
		m.filterInput.Placeholder = "search…"
		m.list.SetFilter("")
		return m.dispatchSearch()
	}
	m.filterInput.Placeholder = "filter…"
	m.matched = 0
	m.list.SetSearchResults(nil)
	m.list.SetFilter(m.filterInput.Value())
	return m.loadTranscriptCmd()
}

// dispatchSearch starts (or restarts) the debounce clock for the current
// query. Empty queries clear results immediately.
func (m *Model) dispatchSearch() tea.Cmd {
	m.searchSeq++
	q := strings.TrimSpace(m.filterInput.Value())
	if q == "" {
		m.activeQuery = ""
		m.matched = 0
		m.list.SetSearchResults(nil)
		return m.loadTranscriptCmd()
	}
	seq := m.searchSeq
	return tea.Tick(searchDebounce, func(time.Time) tea.Msg { return searchTickMsg{seq: seq} })
}

// runSearch is the async search over the index cache. It snapshots the
// sessions slice on the Update goroutine first — the enrich pump mutates
// session structs in place, so the search goroutine must never read the
// live slice (same reason Enrich itself snapshots up front).
func (m *Model) runSearch(seq int) tea.Cmd {
	ix, q := m.index, m.filterInput.Value()
	sessions := append([]store.Session(nil), m.list.Sessions()...)
	return func() tea.Msg {
		hits, _ := ix.Search(q, sessions)
		return searchResultMsg{seq: seq, hits: hits}
	}
}

func waitIndex(ch chan store.IndexProgress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return indexDoneMsg{ch: ch}
		}
		return indexProgressMsg{p: p, ch: ch}
	}
}
```

6. `Update()` — new cases (alongside the existing msg cases):

```go
	case searchTickMsg:
		if !m.searchAll || msg.seq != m.searchSeq {
			return m, nil
		}
		m.activeQuery = strings.TrimSpace(m.filterInput.Value())
		var cmds []tea.Cmd
		if !m.indexReady && !m.indexing {
			ch := make(chan store.IndexProgress, 8)
			m.indexCh = ch
			m.indexing = true
			m.indexDone, m.indexTotal = 0, len(m.list.Sessions())
			m.index.EnsureAll(m.list.Sessions(), 4, ch)
			cmds = append(cmds, waitIndex(ch))
		}
		cmds = append(cmds, m.runSearch(msg.seq))
		return m, tea.Batch(cmds...)

	case searchResultMsg:
		if !m.searchAll || msg.seq != m.searchSeq {
			return m, nil
		}
		m.matched = len(msg.hits)
		m.list.SetSearchResults(msg.hits)
		return m, m.loadTranscriptCmd()

	case indexProgressMsg:
		if msg.ch != m.indexCh {
			return m, nil
		}
		m.indexDone, m.indexTotal = msg.p.Done, msg.p.Total
		return m, waitIndex(msg.ch)

	case indexDoneMsg:
		if msg.ch != m.indexCh {
			return m, nil
		}
		m.indexReady = true
		m.indexing = false
		if m.searchAll && m.activeQuery != "" {
			return m, m.runSearch(m.searchSeq)
		}
		return m, nil
```

7. focusFilter key branch — extend. Today it special-cases Esc and Enter, then feeds `filterInput.Update` + `SetFilter`. New shape:

```go
	case focusFilter:
		switch msg.Type {
		case tea.KeyEsc:
			m.filterInput.SetValue("")
			m.filterInput.Blur()
			m.list.SetFilter("")
			m.focus = focusList
			if m.searchAll {
				m.searchAll = false
				m.filterInput.Placeholder = "filter…"
				m.matched = 0
				m.activeQuery = ""
				m.list.SetSearchResults(nil)
			}
			return m, m.loadTranscriptCmd()
		case tea.KeyEnter:
			m.filterInput.Blur()
			m.focus = focusList
			return m, nil
		case tea.KeyTab:
			return m, m.toggleSearchLayer()
		}
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		if m.searchAll {
			return m, tea.Batch(cmd, m.dispatchSearch())
		}
		m.list.SetFilter(m.filterInput.Value())
		return m, tea.Batch(cmd, m.loadTranscriptCmd())
```

8. `r` rescan (focusList branch): change `case "r": return m, m.scanCmd()` to also force revalidation:

```go
		case "r":
			m.indexReady = false // next full-text search revalidates the cache
			return m, m.scanCmd()
```

9. Title bar count in `View()` — replace the `count := …` block with exactly:

```go
	count := fmt.Sprintf("%d sessions", m.list.Len())
	if m.searchAll && m.activeQuery != "" {
		count = fmt.Sprintf("%d sessions · %d matched", len(m.list.Sessions()), m.matched)
		if !m.indexReady {
			count += "…"
		}
	}
	if m.indexing {
		count += fmt.Sprintf(" · indexing %d/%d…", m.indexDone, m.indexTotal)
	}
	if m.loading {
		count += " · scanning…"
	}
```

(While searching, the left number is the TOTAL session count — the matched count carries the search information.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v` — all new tests PASS; every pre-existing filter/list/mouse test still PASS.

- [ ] **Step 5: Full gate + commit**

```bash
gofmt -l . && go vet ./... && go test -race ./...
git add internal/ui/model.go internal/ui/search_test.go
git commit -m "feat(ui): full-text search layer with debounce and lazy index build"
```

---

### Task 5: preview hit jump, highlight, n/N

**Files:**
- Modify: `internal/ui/preview.go`
- Modify: `internal/ui/model.go` (transcriptMsg handler, focusPreview keys)
- Test: `internal/ui/preview_test.go` (append), `internal/ui/search_test.go` (append)

**Interfaces:**
- Consumes: Task 4's `m.searchAll`/`m.activeQuery`; `store.SplitTerms`; `store.Transcript`.
- Produces:
  - `renderTranscript(t store.Transcript, width int, st styles) (string, []int)` — second return = each message's first line index in the rendered output.
  - `func highlightTerms(text string, terms []string) string` — wraps case-insensitive matches in `\x1b[7m…\x1b[27m`, preserving original casing.
  - Model fields `msgStarts []int`, `hitMsgs []int`, `curHit int`; `n`/`N` handling in the focusPreview branch.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/preview_test.go`:

```go
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
```

Append to `internal/ui/search_test.go`:

```go
func TestPreviewJumpsAndCyclesHits(t *testing.T) {
	m := searchModel(t)
	m.searchAll = true
	m.activeQuery = "quick"
	// Each filler renders ~25 wrapped lines at the fixture's preview width
	// (58), so consecutive hits land at viewport offsets that stay distinct
	// even after SetYOffset clamping (content ≫ viewport height 25).
	long := strings.Repeat("filler words here ", 80)
	tr := store.Transcript{SessionID: "s1", Messages: []store.Message{
		{Kind: store.KindAssistant, Text: long},
		{Kind: store.KindUser, Text: "the quick brown fox"},
		{Kind: store.KindAssistant, Text: long},
		{Kind: store.KindUser, Text: "quick again"},
	}}
	m.previewFor = "s1"
	m2, _ := m.Update(transcriptMsg{id: "s1", t: tr})
	m = m2.(Model)
	if len(m.hitMsgs) != 2 || m.hitMsgs[0] != 1 || m.hitMsgs[1] != 3 {
		t.Fatalf("hitMsgs = %v, want [1 3]", m.hitMsgs)
	}
	if m.preview.YOffset == 0 {
		t.Error("preview must jump to the first hit (message 1 sits below a long message)")
	}
	if !strings.Contains(m.preview.View(), "\x1b[7m") {
		t.Error("hit terms must be reverse-video highlighted")
	}
	first := m.preview.YOffset
	m.focus = focusPreview
	m2, _ = m.Update(key("n"))
	m = m2.(Model)
	if m.preview.YOffset <= first {
		t.Error("n must jump to the next hit further down")
	}
	m2, _ = m.Update(key("n")) // wraps to the first hit
	m = m2.(Model)
	if m.preview.YOffset != first {
		t.Errorf("n past the last hit must wrap: YOffset=%d want %d", m.preview.YOffset, first)
	}
	m2, _ = m.Update(key("N")) // back to the last hit
	m = m2.(Model)
	if m.preview.YOffset <= first {
		t.Error("N must wrap backwards to the last hit")
	}
}

func TestPreviewNoQueryNoHighlight(t *testing.T) {
	m := searchModel(t)
	tr := store.Transcript{SessionID: "s1", Messages: []store.Message{{Kind: store.KindUser, Text: "quick"}}}
	m.previewFor = "s1"
	m2, _ := m.Update(transcriptMsg{id: "s1", t: tr})
	m = m2.(Model)
	if strings.Contains(m.preview.View(), "\x1b[7m") {
		t.Error("no active query → no highlight")
	}
	if m.preview.YOffset != 0 {
		t.Error("no active query → no jump")
	}
}
```

Add missing imports (`"strings"`, `store`) where needed.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestRenderTranscriptMessageOffsets|TestHighlightTerms|TestPreviewJumps|TestPreviewNoQuery' -v`
Expected: compile errors — `renderTranscript` returns 1 value; `highlightTerms`/`hitMsgs` undefined.

- [ ] **Step 3: Implement**

`internal/ui/preview.go` — new version of the file:

```go
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

// renderTranscript renders a transcript as styled, wrapped text for the
// preview viewport, returning each message's first rendered line so hit
// navigation can jump by message. Prefixes: › user, ● assistant, ⚒ tool.
func renderTranscript(t store.Transcript, width int, st styles) (string, []int) {
	if len(t.Messages) == 0 {
		return st.ListMeta.Render("no messages"), nil
	}
	if width < 4 {
		width = 4
	}
	parts := make([]string, 0, len(t.Messages))
	starts := make([]int, 0, len(t.Messages))
	line := 0
	for _, m := range t.Messages {
		var style = st.AssistantMsg
		prefix := "● "
		switch m.Kind {
		case store.KindUser:
			style, prefix = st.UserMsg, "› "
		case store.KindTool:
			style, prefix = st.ToolMsg, "⚒ "
		}
		rendered := style.Width(width).Render(prefix + m.Text)
		starts = append(starts, line)
		line += lipgloss.Height(rendered) + 1 // +1 for the blank joiner line
		parts = append(parts, rendered)
	}
	return strings.Join(parts, "\n\n"), starts
}

// highlightTerms wraps every case-insensitive occurrence of each term in
// reverse-video toggles. All match spans are located against the ORIGINAL
// text and merged before any codes are inserted, so overlapping terms
// ("test", "testing") highlight as one contiguous region instead of
// corrupting each other's escape codes. lipgloss wraps ANSI-aware, so the
// inline codes survive width-wrapping; the closing toggle only clears
// reverse, leaving the message's own foreground styling intact.
func highlightTerms(text string, terms []string) string {
	lower := strings.ToLower(text)
	type span struct{ start, end int }
	var spans []span
	for _, term := range terms {
		if term == "" {
			continue
		}
		for pos := 0; ; {
			i := strings.Index(lower[pos:], term)
			if i < 0 {
				break
			}
			i += pos
			spans = append(spans, span{i, i + len(term)})
			pos = i + len(term)
		}
	}
	if len(spans) == 0 {
		return text
	}
	sort.Slice(spans, func(a, b int) bool { return spans[a].start < spans[b].start })
	merged := spans[:1]
	for _, s := range spans[1:] {
		last := &merged[len(merged)-1]
		if s.start <= last.end {
			if s.end > last.end {
				last.end = s.end
			}
			continue
		}
		merged = append(merged, s)
	}
	var b strings.Builder
	pos := 0
	for _, s := range merged {
		b.WriteString(text[pos:s.start])
		b.WriteString("\x1b[7m")
		b.WriteString(text[s.start:s.end])
		b.WriteString("\x1b[27m")
		pos = s.end
	}
	b.WriteString(text[pos:])
	return b.String()
}
```

`internal/ui/model.go`:

1. Model fields (append): `msgStarts []int`, `hitMsgs []int`, `curHit int`.
2. `transcriptMsg` handler — replace the success path:

```go
	case transcriptMsg:
		if msg.id != m.previewFor {
			return m, nil
		}
		if msg.err != nil {
			m.preview.SetContent(m.st.ErrorText.Render(msg.err.Error()))
			return m, nil
		}
		tr := msg.t
		m.hitMsgs = nil
		m.curHit = 0
		terms := store.SplitTerms(m.activeQuery)
		if m.searchAll && len(terms) > 0 {
			for i := range tr.Messages {
				if tr.Messages[i].Kind == store.KindTool {
					continue
				}
				lower := strings.ToLower(tr.Messages[i].Text)
				for _, t := range terms {
					if strings.Contains(lower, t) {
						tr.Messages[i].Text = highlightTerms(tr.Messages[i].Text, terms)
						m.hitMsgs = append(m.hitMsgs, i)
						break
					}
				}
			}
		}
		content, starts := renderTranscript(tr, m.preview.Width, m.st)
		m.msgStarts = starts
		m.preview.SetContent(content)
		m.preview.GotoTop()
		if len(m.hitMsgs) > 0 {
			m.preview.SetYOffset(m.msgStarts[m.hitMsgs[0]])
		}
		return m, nil
```

(Note: `tr` is a copy of the message slice header, but `tr.Messages[i].Text = …` mutates the shared backing array of the cached transcript. To avoid poisoning the cache, deep-copy the messages first:

```go
		tr := msg.t
		msgs := make([]store.Message, len(tr.Messages))
		copy(msgs, tr.Messages)
		tr.Messages = msgs
```

Include this copy — it is required, not optional.)

3. focusPreview key branch — add before the default forwarding:

```go
		case "n", "N":
			if len(m.hitMsgs) > 0 {
				if msg.String() == "n" {
					m.curHit = (m.curHit + 1) % len(m.hitMsgs)
				} else {
					m.curHit = (m.curHit - 1 + len(m.hitMsgs)) % len(m.hitMsgs)
				}
				m.preview.SetYOffset(m.msgStarts[m.hitMsgs[m.curHit]])
				return m, nil
			}
```

(Fall through to the viewport when there are no hits: place this case so that when `len(m.hitMsgs) == 0` execution continues to the existing `m.preview.Update(msg)` — i.e. don't `return` in that situation.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v` — all PASS. The pre-existing `renderTranscript` call sites are exactly two (the transcriptMsg handler you just rewrote and `preview_test.go`'s existing test, which must be updated to `out, _ := renderTranscript(…)`).

- [ ] **Step 5: Full gate + commit**

```bash
gofmt -l . && go vet ./... && go test -race ./...
git add internal/ui/preview.go internal/ui/preview_test.go internal/ui/model.go internal/ui/search_test.go
git commit -m "feat(ui): preview hit jump, term highlight, n/N navigation"
```

---

### Task 6: 🔍 icon click toggles the layer

**Files:**
- Modify: `internal/ui/mouse.go` (zoneFilter branch)
- Test: `internal/ui/search_test.go` (append)

**Interfaces:**
- Consumes: Task 4's `toggleSearchLayer`; existing `zoneFilter` click branch in `handleMouse`.
- Produces: clicking bar columns x ≤ 2 toggles layer AND focuses; the rest of the bar only focuses (existing behavior).

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/search_test.go`:

```go
func TestClickSearchIconTogglesLayer(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(click(1, 1)) // the 🔍 icon
	m = m2.(Model)
	if !m.searchAll {
		t.Fatal("clicking the 🔍 icon must enable the full-text layer")
	}
	if m.focus != focusFilter || !m.filterInput.Focused() {
		t.Error("icon click must also focus the input")
	}
	m2, _ = m.Update(click(10, 1)) // bar body: focus only, no toggle
	m = m2.(Model)
	if !m.searchAll {
		t.Error("clicking the bar body must not toggle the layer")
	}
	m2, _ = m.Update(click(1, 1))
	m = m2.(Model)
	if m.searchAll {
		t.Error("second icon click must toggle back")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestClickSearchIcon -v`
Expected: FAIL — icon click currently only focuses (searchAll stays false).

- [ ] **Step 3: Implement**

In `internal/ui/mouse.go`, `handleMouse`, replace the `case zoneFilter:` branch:

```go
	case zoneFilter:
		m.focus = focusFilter
		m.filterInput.Focus()
		if msg.X <= 2 { // the 🔍 icon toggles the search layer
			return m, m.toggleSearchLayer()
		}
		return m, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v` — all PASS (including the pre-existing `TestClickFilterBarFocusesFilter`, which clicks x=5 and must be unaffected).

- [ ] **Step 5: Full gate + commit**

```bash
gofmt -l . && go vet ./... && go test -race ./...
git add internal/ui/mouse.go internal/ui/search_test.go
git commit -m "feat(ui): search-layer toggle on the filter-bar icon"
```

---

### Task 7: README, full gate, install

**Files:**
- Modify: `README.md` (Usage section, after the Mouse subsection)

**Interfaces:** none — docs and verification.

- [ ] **Step 1: Document search**

Insert into `README.md`, directly after the `### Mouse` subsection (before `### Resuming`):

```markdown
### Search

The filter bar has two layers — press `/` to focus it, then **Tab** (or
click the 🔍 icon) to switch:

- **filter…** — the default fuzzy filter over title, project, and first
  prompt.
- **search…** — full-text search over everything said in every session.
  Space-separated terms must all appear in a session (AND). Results are
  ordered by hit count; the preview jumps to the first hit with matches
  highlighted, and `n` / `N` (preview focused) step through hits.

The first full-text search builds a plain-text index under your user cache
directory (`sm-index/`); the title bar shows `indexing …` progress. After
that, searches are instant and only changed sessions are re-indexed.
```

- [ ] **Step 2: Full gate**

```bash
cd ~/Desktop/ai-sessions-manager
gofmt -l . && go vet ./... && go test -race ./...
```

Expected: `gofmt -l` silent, vet clean, all tests PASS.

- [ ] **Step 3: Install**

```bash
make install && sm --version
```

Expected: builds, installs to `~/.local/bin/sm`, prints a version. The interactive smoke check (Tab toggle, a real full-text query over the live corpus, first-run indexing progress, n/N in the preview, 🔍 click) is done by the human afterward.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: search usage"
```
