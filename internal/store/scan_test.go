package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func touch(t *testing.T, path string, mt time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mt, mt); err != nil {
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
	for i := range sessions {
		sessions[i].Agent = AgentClaude
	}
	results := make(chan EnrichResult, len(sessions))
	prov := []Provider{NewClaudeProvider(dir)}
	Enrich(sessions, prov, 2, results)
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

func TestEnrichZeroWorkers(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "-x", "s1.jsonl"),
		`{"type":"user","message":{"role":"user","content":"hello there"},"timestamp":"2026-07-01T10:00:00.000Z","cwd":"/tmp"}`+"\n")
	sessions, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range sessions {
		sessions[i].Agent = AgentClaude
	}
	results := make(chan EnrichResult, len(sessions))
	prov := []Provider{NewClaudeProvider(dir)}
	Enrich(sessions, prov, 0, results)
	n := 0
	for range results {
		n++
	}
	// The loop terminating at all proves Enrich did not deadlock.
	if n != 1 {
		t.Fatalf("got %d results, want 1", n)
	}
}

// TestEnrichConcurrentWithSliceMutation reproduces the delete-time data
// race: the UI's RemoveSession shifts the sessions slice in place
// (append(sessions[:i], sessions[i+1:]...), which overwrites the backing
// array) while Enrich's worker goroutines may still be reading it. Enrich
// must snapshot the fields it needs before spawning workers so the
// workers never touch the caller's slice. Run this test with -race: it
// races (and reliably fails) if a worker reads sessions[i] directly, and
// is race-free once Enrich reads from an internal snapshot instead.
func TestEnrichConcurrentWithSliceMutation(t *testing.T) {
	dir := t.TempDir()
	// n files, each with many lines: real ParseMetadata work per file
	// widens the window during which workers are actively reading the
	// (pre-fix: shared, post-fix: snapshotted) slice, so the concurrent
	// mutation below reliably overlaps with it instead of racing to
	// finish first.
	const n = 32
	const linesPerFile = 200
	for i := 0; i < n; i++ {
		var sb strings.Builder
		for l := 0; l < linesPerFile; l++ {
			sb.WriteString(fmt.Sprintf(
				`{"type":"user","message":{"role":"user","content":"hello %d line %d"},"timestamp":"2026-07-01T10:00:00.000Z","cwd":"/tmp/p%d"}`+"\n",
				i, l, i))
		}
		writeFile(t, filepath.Join(dir, fmt.Sprintf("-proj%d", i), fmt.Sprintf("s%d.jsonl", i)), sb.String())
	}
	sessions, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != n {
		t.Fatalf("got %d sessions, want %d", len(sessions), n)
	}
	for i := range sessions {
		sessions[i].Agent = AgentClaude
	}

	results := make(chan EnrichResult, len(sessions))
	// Enrich is called synchronously, exactly like the UI's Update handler
	// does: it snapshots what it needs and spawns its worker/dispatcher
	// goroutines before returning. Only after it returns do we mutate the
	// caller's slice, mirroring the real timeline (RemoveSession runs from
	// a later, sequential Update call while a prior scan's workers are
	// still draining in the background).
	prov := []Provider{NewClaudeProvider(dir)}
	Enrich(sessions, prov, 4, results)

	// Concurrently perform the same in-place shift RemoveSession(0) does
	// (append(sessions[:0], sessions[1:]...)) on the SAME backing array
	// held by this test — not a copy. Before the snapshot fix, Enrich's
	// workers read sessions[i].Path/Slug directly, which races with these
	// writes under `go test -race`. The mutator runs for as long as the
	// drain loop below is still receiving, rather than a fixed iteration
	// count, so it reliably overlaps with worker activity instead of
	// racing to finish before the (fast, tiny-file) workers get scheduled.
	stop := make(chan struct{})
	mutatorDone := make(chan struct{})
	go func() {
		defer close(mutatorDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if len(sessions) > 1 {
				copy(sessions[0:], sessions[1:])
			}
		}
	}()

	got := 0
	for range results {
		got++
	}
	close(stop)
	<-mutatorDone
	if got != n {
		t.Fatalf("got %d results, want %d", got, n)
	}
}

func TestEnrichDispatchesByAgent(t *testing.T) {
	cdir := t.TempDir()
	// one codex session with a real prompt
	cf := filepath.Join(cdir, "2026", "06", "26", "rollout-x-bbbb.jsonl")
	writeFile(t, cf, `{"type":"session_meta","payload":{"cwd":"/home/w/p","timestamp":"2026-06-26T03:52:34.743Z"}}`+"\n"+
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello codex"}]}}`+"\n")
	sessions := []Session{{ID: "bbbb", Path: cf, Agent: AgentCodex}}
	provs := []Provider{NewClaudeProvider(t.TempDir()), NewCodexProvider(cdir)}
	results := make(chan EnrichResult, 1)
	Enrich(sessions, provs, 2, results)
	r := <-results
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Meta.CWD != "/home/w/p" || r.Meta.FirstPrompt != "hello codex" {
		t.Errorf("codex enrich = %+v", r.Meta)
	}
	<-results // drain enrichDone (channel close)
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
