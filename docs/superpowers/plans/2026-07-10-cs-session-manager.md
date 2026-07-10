# cs — Claude Code Session Manager TUI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `cs`, a single-binary Go TUI that lists every Claude Code session on the machine, previews transcripts, and resumes/creates/deletes sessions.

**Architecture:** `internal/store` is a UI-free read model over `~/.claude/projects/*/*.jsonl` (scan fast, enrich concurrently, parse transcripts on demand). `internal/ui` is a Bubble Tea app: two-pane layout (session list + transcript preview), fuzzy filter, dialogs for delete/new-session, and `tea.ExecProcess` to hand the terminal to `claude`.

**Tech Stack:** Go ≥1.24, Bubble Tea **v1** (`charmbracelet/bubbletea@v1`), Bubbles v0.21, Lipgloss v1, `sahilm/fuzzy`.

**Spec:** `docs/superpowers/specs/2026-07-10-claude-sessions-tui-design.md` (approved).

## Global Constraints

- Repo: `/home/william/claude-sessions`. Module path: `github.com/dukechain2333/claude-sessions`. Binary name: `cs`.
- Go is installed user-locally at `~/.local/go` (no sudo on this machine). If `go` is not on PATH in a shell step, prefix: `export PATH=$HOME/.local/go/bin:$PATH`.
- Bubble Tea **v1 API only** (`tea.ExecProcess`, `tea.WithAltScreen`) — do NOT upgrade to v2.
- Session files are NEVER deleted with `rm`/`os.Remove` — only `os.Rename` into `<projectsDir>/.trash/<slug>/`.
- All colors are `lipgloss.AdaptiveColor` (light+dark terminals).
- Directories under the projects dir whose names start with `.` (e.g. `.trash`) are never scanned.
- Every commit message ends with: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Run all commands from `/home/william/claude-sessions` unless a step says otherwise.

---

### Task 1: Toolchain + project scaffold

**Files:**
- Create: `go.mod` (via `go mod init`)
- Create: `cmd/cs/main.go` (stub)
- Create: `Makefile`
- Create: `.gitignore`

**Interfaces:**
- Consumes: nothing.
- Produces: buildable module `github.com/dukechain2333/claude-sessions`; `make build` → `./cs`; deps `bubbletea@v1`, `bubbles`, `lipgloss@v1`, `sahilm/fuzzy` in go.mod for all later tasks.

- [ ] **Step 1: Install Go user-locally (skip if `go version` already works)**

```bash
export PATH=$HOME/.local/go/bin:$PATH
go version 2>/dev/null || {
  curl -fsSL https://go.dev/dl/go1.24.5.linux-amd64.tar.gz -o /tmp/claude-1000/-home-william/294a7ea6-320a-4bdd-a77c-c9e3cbc26a6d/scratchpad/go.tgz
  mkdir -p $HOME/.local
  tar -C $HOME/.local -xzf /tmp/claude-1000/-home-william/294a7ea6-320a-4bdd-a77c-c9e3cbc26a6d/scratchpad/go.tgz
  grep -q '.local/go/bin' ~/.zshrc || echo 'export PATH=$HOME/.local/go/bin:$HOME/go/bin:$PATH' >> ~/.zshrc
}
go version
```

Expected: `go version go1.24.5 linux/amd64` (or newer).

- [ ] **Step 2: Init module and fetch dependencies**

```bash
cd /home/william/claude-sessions
go mod init github.com/dukechain2333/claude-sessions
go get github.com/charmbracelet/bubbletea@v1 github.com/charmbracelet/bubbles@latest github.com/charmbracelet/lipgloss@v1 github.com/sahilm/fuzzy@latest
```

Expected: `go.mod`/`go.sum` created; bubbletea resolves to a `v1.x.y` version (verify with `grep bubbletea go.mod` — must NOT be v2).

- [ ] **Step 3: Write stub main, Makefile, .gitignore**

`cmd/cs/main.go`:
```go
package main

import "fmt"

var version = "0.1.0"

func main() {
	fmt.Println("cs", version)
}
```

`Makefile`:
```make
BINARY=cs

.PHONY: build test install vet

build:
	go build -o $(BINARY) ./cmd/cs

test:
	go test ./...

vet:
	go vet ./...

install: build
	install -Dm755 $(BINARY) $(HOME)/.local/bin/$(BINARY)
```

`.gitignore`:
```
/cs
```

- [ ] **Step 4: Verify build runs**

Run: `export PATH=$HOME/.local/go/bin:$PATH && make build && ./cs`
Expected: prints `cs 0.1.0`.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum cmd/cs/main.go Makefile .gitignore
git commit -m "chore: scaffold Go module, deps, Makefile

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: store — Session type + metadata parser

**Files:**
- Create: `internal/store/session.go`
- Create: `internal/store/parse.go`
- Create: `internal/store/parse_test.go`
- Create: `internal/store/testdata/session.jsonl`, `internal/store/testdata/empty.jsonl`, `internal/store/testdata/untitled.jsonl`

**Interfaces:**
- Consumes: nothing.
- Produces (used by every later task):
  - `type Session struct { ID, Path, Slug, CWD, Title, FirstPrompt, GitBranch string; LastActivity time.Time; UserMessages, TotalMessages int; Enriched, Unreadable bool }`
  - `func (s Session) Project() string`, `func (s Session) Empty() bool`, `func (s *Session) Apply(m Meta)`
  - `type Meta struct { Title, FirstPrompt, CWD, GitBranch string; LastActivity time.Time; UserMessages, TotalMessages int }`
  - `func ParseMetadata(path string) (Meta, error)`
  - `func Truncate(s string, n int) string`

- [ ] **Step 1: Write fixture files**

`internal/store/testdata/session.jsonl` (note line 6 is intentionally malformed):
```
{"type":"mode","mode":"normal","sessionId":"abc"}
{"type":"ai-title","aiTitle":"Fix the flaky test","sessionId":"abc"}
{"parentUuid":null,"type":"user","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>ran /model</local-command-caveat>"},"uuid":"u0","timestamp":"2026-07-01T10:00:00.000Z","cwd":"/home/william/demo","sessionId":"abc","version":"2.1.191","gitBranch":"main"}
{"parentUuid":"u0","type":"user","message":{"role":"user","content":"Please fix the flaky test in parser_test.go"},"uuid":"u1","timestamp":"2026-07-01T10:00:05.000Z","cwd":"/home/william/demo","sessionId":"abc","gitBranch":"main"}
{"parentUuid":"u1","type":"assistant","message":{"role":"assistant","model":"claude-opus-4-8","content":[{"type":"text","text":"I'll look at the test first."},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./...","description":"Run tests"}}]},"uuid":"a1","timestamp":"2026-07-01T10:00:10.000Z","cwd":"/home/william/demo","gitBranch":"main"}
not json at all {{{
{"parentUuid":"a1","type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]},"uuid":"u2","timestamp":"2026-07-01T10:00:12.000Z","cwd":"/home/william/demo"}
{"parentUuid":"u2","type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Fixed. The race was in setup."}]},"uuid":"a2","timestamp":"2026-07-01T10:00:20.000Z","cwd":"/home/william/demo","gitBranch":"main"}
{"type":"summary","summary":"Fixed flaky test","leafUuid":"a2"}
```

`internal/store/testdata/empty.jsonl`:
```
{"type":"mode","mode":"normal","sessionId":"empty1"}
{"parentUuid":null,"type":"user","isMeta":true,"message":{"role":"user","content":"<command-name>/clear</command-name>"},"uuid":"u1","timestamp":"2026-07-02T09:00:00.000Z","cwd":"/home/william/demo","sessionId":"empty1"}
```

`internal/store/testdata/untitled.jsonl`:
```
{"parentUuid":null,"type":"user","message":{"role":"user","content":"Refactor the long database helper into smaller functions so each one does a single thing"},"uuid":"u1","timestamp":"2026-07-03T09:00:00.000Z","cwd":"/home/william/demo","sessionId":"unt1"}
```

- [ ] **Step 2: Write the failing test**

`internal/store/parse_test.go`:
```go
package store

import (
	"testing"
	"time"
)

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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run 'TestParse|TestTruncate' -v`
Expected: FAIL (compile error: `ParseMetadata` undefined).

- [ ] **Step 4: Write the implementation**

`internal/store/session.go`:
```go
// Package store reads Claude Code session history from ~/.claude/projects.
package store

import (
	"path/filepath"
	"strings"
	"time"
)

type Session struct {
	ID            string // session uuid (filename without .jsonl)
	Path          string // absolute path to the .jsonl file
	Slug          string // directory name under the projects dir
	CWD           string // working directory recorded in the session
	Title         string
	FirstPrompt   string
	GitBranch     string
	LastActivity  time.Time
	UserMessages  int
	TotalMessages int
	Enriched      bool
	Unreadable    bool
}

// Project is the short label shown next to a session.
func (s Session) Project() string {
	if s.CWD != "" {
		return filepath.Base(s.CWD)
	}
	return s.Slug
}

// Empty reports whether the session contains no real user prompts
// (hook/meta-only files). Only meaningful once enriched.
func (s Session) Empty() bool {
	return s.Enriched && s.UserMessages == 0
}

func (s *Session) Apply(m Meta) {
	s.Title = m.Title
	s.FirstPrompt = m.FirstPrompt
	s.GitBranch = m.GitBranch
	if m.CWD != "" {
		s.CWD = m.CWD
	}
	if !m.LastActivity.IsZero() {
		s.LastActivity = m.LastActivity
	}
	s.UserMessages = m.UserMessages
	s.TotalMessages = m.TotalMessages
	s.Enriched = true
}

// Truncate collapses whitespace and cuts s to at most n runes,
// ending with an ellipsis when cut.
func Truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
```

`internal/store/parse.go`:
```go
package store

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// Meta is the lightweight per-session metadata extracted by one
// streaming pass over a session .jsonl file.
type Meta struct {
	Title         string
	FirstPrompt   string
	CWD           string
	GitBranch     string
	LastActivity  time.Time
	UserMessages  int
	TotalMessages int
}

type rawRecord struct {
	Type      string          `json:"type"`
	AiTitle   string          `json:"aiTitle"`
	CWD       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Timestamp string          `json:"timestamp"`
	IsMeta    bool            `json:"isMeta"`
	Message   json.RawMessage `json:"message"`
}

type apiMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func newScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	// Single records can be megabytes (pasted files, tool results).
	sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	return sc
}

func ParseMetadata(path string) (Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	var m Meta
	sc := newScanner(f)
	for sc.Scan() {
		var rec rawRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue // malformed lines are never fatal
		}
		switch rec.Type {
		case "ai-title":
			if rec.AiTitle != "" {
				m.Title = rec.AiTitle // last one wins
			}
		case "user", "assistant":
			if rec.CWD != "" {
				m.CWD = rec.CWD
			}
			if rec.GitBranch != "" {
				m.GitBranch = rec.GitBranch
			}
			if t, err := time.Parse(time.RFC3339, rec.Timestamp); err == nil && t.After(m.LastActivity) {
				m.LastActivity = t
			}
			if rec.Type == "assistant" {
				m.TotalMessages++
			} else if p := realPrompt(rec); p != "" {
				m.TotalMessages++
				m.UserMessages++
				if m.FirstPrompt == "" {
					m.FirstPrompt = p
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Meta{}, err
	}
	if m.Title == "" {
		m.Title = Truncate(m.FirstPrompt, 60)
	}
	return m, nil
}

// realPrompt returns the text of a user record iff it is a prompt the
// human actually typed: not a meta record, not a tool_result, and not
// harness-injected markup (which always starts with "<").
func realPrompt(rec rawRecord) string {
	if rec.IsMeta {
		return ""
	}
	text := strings.TrimSpace(firstText(rec.Message))
	if text == "" || strings.HasPrefix(text, "<") {
		return ""
	}
	return text
}

func firstText(raw json.RawMessage) string {
	var msg apiMessage
	if json.Unmarshal(raw, &msg) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(msg.Content, &s) == nil {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(msg.Content, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				return b.Text
			}
		}
	}
	return ""
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -v`
Expected: PASS (4 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/store/
git commit -m "feat(store): session type and jsonl metadata parser

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: store — scan, slug resolution, known dirs

**Files:**
- Create: `internal/store/scan.go`
- Create: `internal/store/scan_test.go`

**Interfaces:**
- Consumes: `Session`, `Meta`, `ParseMetadata` from Task 2.
- Produces:
  - `func Scan(projectsDir string) ([]Session, error)` — sorted most-recent-first by mtime; skips dot-dirs.
  - `type EnrichResult struct { Index int; Meta Meta; Err error }`
  - `func Enrich(sessions []Session, workers int, results chan<- EnrichResult)` — closes `results` when done.
  - `func ResolveSlug(root, slug string) string` — best-effort slug→path (production callers pass `root = "/"`).
  - `func KnownDirs(sessions []Session) []string` — unique existing CWDs, input order preserved.

- [ ] **Step 1: Write the failing test**

`internal/store/scan_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run 'TestScan|TestEnrich|TestResolveSlug|TestKnownDirs' -v`
Expected: FAIL (compile error: `Scan` undefined).

- [ ] **Step 3: Write the implementation**

`internal/store/scan.go`:
```go
package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Scan lists every session file under projectsDir, most recent first.
// It reads only directory entries — no file contents — so it is fast
// enough to run synchronously before first paint.
func Scan(projectsDir string) ([]Session, error) {
	dirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, err
	}
	var sessions []Session
	for _, d := range dirs {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		files, err := os.ReadDir(filepath.Join(projectsDir, d.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			s := Session{
				ID:   strings.TrimSuffix(f.Name(), ".jsonl"),
				Path: filepath.Join(projectsDir, d.Name(), f.Name()),
				Slug: d.Name(),
			}
			if info, err := f.Info(); err == nil {
				s.LastActivity = info.ModTime()
			}
			sessions = append(sessions, s)
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActivity.After(sessions[j].LastActivity)
	})
	return sessions, nil
}

type EnrichResult struct {
	Index int
	Meta  Meta
	Err   error
}

// Enrich parses metadata for every session concurrently, sending one
// result per session on results and closing it when all are done.
func Enrich(sessions []Session, workers int, results chan<- EnrichResult) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				m, err := ParseMetadata(sessions[i].Path)
				if err == nil && m.CWD == "" {
					m.CWD = ResolveSlug("/", sessions[i].Slug)
				}
				results <- EnrichResult{Index: i, Meta: m, Err: err}
			}
		}()
	}
	go func() {
		for i := range sessions {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
}

// ResolveSlug maps a projects-dir slug like "-home-william-hyper-sagnn"
// back to a filesystem path. Slugs replace "/" with "-", which collides
// with dashes inside directory names, so it tries every split and
// returns the longest candidate that exists under root ("" if none).
func ResolveSlug(root, slug string) string {
	tokens := strings.Split(strings.TrimPrefix(slug, "-"), "-")
	best := ""
	var walk func(prefix string, i int)
	walk = func(prefix string, i int) {
		if i == len(tokens) {
			full := filepath.Join(root, prefix)
			if st, err := os.Stat(full); err == nil && st.IsDir() && len(full) > len(best) {
				best = full
			}
			return
		}
		walk(prefix+"/"+tokens[i], i+1)
		if i > 0 {
			walk(prefix+"-"+tokens[i], i+1)
		}
	}
	walk("", 0)
	return best
}

// KnownDirs returns the unique, still-existing working directories of
// sessions, preserving input order (callers pass recency-sorted slices).
func KnownDirs(sessions []Session) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, s := range sessions {
		if s.CWD == "" || seen[s.CWD] {
			continue
		}
		seen[s.CWD] = true
		if st, err := os.Stat(s.CWD); err == nil && st.IsDir() {
			dirs = append(dirs, s.CWD)
		}
	}
	return dirs
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -v`
Expected: PASS (all store tests).

- [ ] **Step 5: Commit**

```bash
git add internal/store/scan.go internal/store/scan_test.go
git commit -m "feat(store): directory scan, concurrent enrich, slug resolution

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: store — transcripts + bounded cache

**Files:**
- Create: `internal/store/transcript.go`
- Create: `internal/store/transcript_test.go`

**Interfaces:**
- Consumes: `rawRecord`, `apiMessage`, `contentBlock`, `realPrompt`, `firstText`, `newScanner`, `Truncate` from Task 2.
- Produces:
  - `type MsgKind int` with `KindUser`, `KindAssistant`, `KindTool`
  - `type Message struct { Kind MsgKind; Text string }`
  - `type Transcript struct { SessionID string; Messages []Message }`
  - `func ParseTranscript(path string) (Transcript, error)`
  - `func NewTranscriptCache(capacity int) *TranscriptCache` with method `Get(path string) (Transcript, error)`

- [ ] **Step 1: Write the failing test**

`internal/store/transcript_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run 'TestParseTranscript|TestTranscriptCache' -v`
Expected: FAIL (compile error: `ParseTranscript` undefined).

- [ ] **Step 3: Write the implementation**

`internal/store/transcript.go`:
```go
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type MsgKind int

const (
	KindUser MsgKind = iota
	KindAssistant
	KindTool
)

type Message struct {
	Kind MsgKind
	Text string
}

type Transcript struct {
	SessionID string
	Messages  []Message
}

// ParseTranscript extracts the human-readable conversation: real user
// prompts, assistant text, and tool calls collapsed to one-liners.
func ParseTranscript(path string) (Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return Transcript{}, err
	}
	defer f.Close()
	var tr Transcript
	sc := newScanner(f)
	for sc.Scan() {
		var rec rawRecord
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		switch rec.Type {
		case "user":
			if p := realPrompt(rec); p != "" {
				tr.Messages = append(tr.Messages, Message{KindUser, p})
			}
		case "assistant":
			var msg apiMessage
			if json.Unmarshal(rec.Message, &msg) != nil {
				continue
			}
			var blocks []contentBlock
			if json.Unmarshal(msg.Content, &blocks) != nil {
				continue
			}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					if s := strings.TrimSpace(b.Text); s != "" {
						tr.Messages = append(tr.Messages, Message{KindAssistant, s})
					}
				case "tool_use":
					tr.Messages = append(tr.Messages, Message{KindTool, summarizeTool(b)})
				}
			}
		}
	}
	return tr, sc.Err()
}

func summarizeTool(b contentBlock) string {
	var input map[string]any
	json.Unmarshal(b.Input, &input)
	for _, k := range []string{"description", "command", "file_path", "pattern", "prompt", "query", "skill", "subject"} {
		if v, ok := input[k].(string); ok && v != "" {
			return b.Name + ": " + Truncate(v, 80)
		}
	}
	return b.Name
}

// TranscriptCache is a small LRU keyed by path+mtime, so an updated
// session file is re-parsed while repeated previews stay instant.
type TranscriptCache struct {
	capacity int
	order    []string
	entries  map[string]Transcript
}

func NewTranscriptCache(capacity int) *TranscriptCache {
	return &TranscriptCache{capacity: capacity, entries: map[string]Transcript{}}
}

func (c *TranscriptCache) Get(path string) (Transcript, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Transcript{}, err
	}
	key := fmt.Sprintf("%s|%d", path, st.ModTime().UnixNano())
	if t, ok := c.entries[key]; ok {
		c.touch(key)
		return t, nil
	}
	t, err := ParseTranscript(path)
	if err != nil {
		return Transcript{}, err
	}
	c.entries[key] = t
	c.order = append(c.order, key)
	if len(c.order) > c.capacity {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
	return t, nil
}

func (c *TranscriptCache) touch(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(append(c.order[:i:i], c.order[i+1:]...), key)
			return
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/transcript.go internal/store/transcript_test.go
git commit -m "feat(store): transcript parsing with tool one-liners and LRU cache

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: store — trash

**Files:**
- Create: `internal/store/trash.go`
- Create: `internal/store/trash_test.go`

**Interfaces:**
- Consumes: `Session` from Task 2.
- Produces: `func TrashSession(projectsDir string, s Session) (string, error)` — moves the file to `<projectsDir>/.trash/<slug>/<file>` and returns the destination.

- [ ] **Step 1: Write the failing test**

`internal/store/trash_test.go`:
```go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrashSession(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "-home-w-proj", "abc.jsonl")
	writeFile(t, src, "{}\n")
	s := Session{ID: "abc", Path: src, Slug: "-home-w-proj"}

	dest, err := TrashSession(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, ".trash", "-home-w-proj", "abc.jsonl")
	if dest != want {
		t.Errorf("dest = %q, want %q", dest, want)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source file still exists")
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("trashed file missing: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run TestTrashSession -v`
Expected: FAIL (compile error: `TrashSession` undefined).

- [ ] **Step 3: Write the implementation**

`internal/store/trash.go`:
```go
package store

import (
	"os"
	"path/filepath"
)

// TrashSession moves a session file into <projectsDir>/.trash/<slug>/
// so deletion is always recoverable with a plain mv. It never removes
// file contents.
func TrashSession(projectsDir string, s Session) (string, error) {
	dest := filepath.Join(projectsDir, ".trash", s.Slug, filepath.Base(s.Path))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	if err := os.Rename(s.Path, dest); err != nil {
		return "", err
	}
	return dest, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/trash.go internal/store/trash_test.go
git commit -m "feat(store): trash-based session deletion

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: ui — styles, humanTime, list pane

**Files:**
- Create: `internal/ui/styles.go`
- Create: `internal/ui/listpane.go`
- Create: `internal/ui/listpane_test.go`

**Interfaces:**
- Consumes: `store.Session`, `store.Truncate`.
- Produces:
  - `type styles struct` + `func defaultStyles() styles` (fields listed in code below; later tasks use `AppTitle`, `Count`, `ListTitle`, `ListTitleSel`, `ListMeta`, `ListMetaSel`, `UserMsg`, `AssistantMsg`, `ToolMsg`, `PaneFocused`, `PaneBlurred`, `Help`, `ErrorText`, `DialogBox`)
  - `func humanTime(t, now time.Time) string`
  - `type listPane struct` with methods `SetSize(w, h int)`, `SetSessions([]store.Session)`, `ApplyEnrich(i int, m store.Meta)`, `SetFilter(q string)`, `ToggleEmpty()`, `MoveCursor(delta int)`, `Selected() (store.Session, int, bool)`, `RemoveSession(i int)`, `Len() int`, `Sessions() []store.Session`, `View() string`. Field `focused bool` is exported enough for the package (same package).

- [ ] **Step 1: Write the failing test**

`internal/ui/listpane_test.go`:
```go
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

func testSessions() []store.Session {
	return []store.Session{
		{ID: "s1", Slug: "-p1", CWD: "/x/alpha", Title: "Create slides from notes", FirstPrompt: "make slides", UserMessages: 3, Enriched: true, LastActivity: time.Now()},
		{ID: "s2", Slug: "-p2", CWD: "/x/beta", Title: "Fix backup script", FirstPrompt: "backup is broken", UserMessages: 2, Enriched: true, LastActivity: time.Now().Add(-time.Hour)},
		{ID: "s3", Slug: "-p3", CWD: "/x/gamma", Title: "", FirstPrompt: "", UserMessages: 0, Enriched: true, LastActivity: time.Now().Add(-2 * time.Hour)},
	}
}

func newTestPane() listPane {
	l := listPane{styles: defaultStyles()}
	l.SetSize(50, 30)
	l.SetSessions(testSessions())
	return l
}

func TestListFilter(t *testing.T) {
	l := newTestPane()
	if got := len(l.visible); got != 2 {
		t.Fatalf("visible = %d, want 2 (empty session hidden)", got)
	}
	l.SetFilter("backup")
	s, _, ok := l.Selected()
	if !ok || s.ID != "s2" {
		t.Errorf("filter 'backup' selected %v", s.ID)
	}
	l.SetFilter("")
	if got := len(l.visible); got != 2 {
		t.Errorf("clearing filter: visible = %d, want 2", got)
	}
}

func TestListToggleEmpty(t *testing.T) {
	l := newTestPane()
	l.ToggleEmpty()
	if got := len(l.visible); got != 3 {
		t.Errorf("visible = %d, want 3 after ToggleEmpty", got)
	}
}

func TestListCursorAndRemove(t *testing.T) {
	l := newTestPane()
	l.MoveCursor(1)
	s, idx, _ := l.Selected()
	if s.ID != "s2" {
		t.Fatalf("cursor at %s, want s2", s.ID)
	}
	l.MoveCursor(5) // clamps at end
	if _, _, ok := l.Selected(); !ok {
		t.Fatal("selection lost after clamp")
	}
	l.RemoveSession(idx)
	for _, s := range l.Sessions() {
		if s.ID == "s2" {
			t.Error("s2 still present after RemoveSession")
		}
	}
}

func TestListView(t *testing.T) {
	l := newTestPane()
	v := l.View()
	if !strings.Contains(v, "Create slides from notes") {
		t.Errorf("view missing title:\n%s", v)
	}
	if !strings.Contains(v, "alpha") {
		t.Errorf("view missing project label:\n%s", v)
	}
}

func TestHumanTime(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		t    time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-48 * time.Hour), "2d ago"},
		{now.Add(-90 * 24 * time.Hour), "Apr 11 2026"},
	}
	for _, c := range cases {
		if got := humanTime(c.t, now); got != c.want {
			t.Errorf("humanTime(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -v`
Expected: FAIL (compile error: `listPane` undefined).

- [ ] **Step 3: Write the implementation**

`internal/ui/styles.go`:
```go
// Package ui implements the cs terminal interface with Bubble Tea.
package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type styles struct {
	AppTitle     lipgloss.Style
	Count        lipgloss.Style
	ListTitle    lipgloss.Style
	ListTitleSel lipgloss.Style
	ListMeta     lipgloss.Style
	ListMetaSel  lipgloss.Style
	UserMsg      lipgloss.Style
	AssistantMsg lipgloss.Style
	ToolMsg      lipgloss.Style
	PaneFocused  lipgloss.Style
	PaneBlurred  lipgloss.Style
	Help         lipgloss.Style
	ErrorText    lipgloss.Style
	DialogBox    lipgloss.Style
}

func defaultStyles() styles {
	accent := lipgloss.AdaptiveColor{Light: "56", Dark: "212"}
	dim := lipgloss.AdaptiveColor{Light: "245", Dark: "241"}
	text := lipgloss.AdaptiveColor{Light: "235", Dark: "252"}
	warn := lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	return styles{
		AppTitle:     lipgloss.NewStyle().Bold(true).Foreground(accent),
		Count:        lipgloss.NewStyle().Foreground(dim),
		ListTitle:    lipgloss.NewStyle().Foreground(text),
		ListTitleSel: lipgloss.NewStyle().Bold(true).Foreground(accent),
		ListMeta:     lipgloss.NewStyle().Foreground(dim),
		ListMetaSel:  lipgloss.NewStyle().Foreground(accent),
		UserMsg:      lipgloss.NewStyle().Bold(true).Foreground(text),
		AssistantMsg: lipgloss.NewStyle().Foreground(text),
		ToolMsg:      lipgloss.NewStyle().Foreground(dim),
		PaneFocused:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent),
		PaneBlurred:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dim),
		Help:         lipgloss.NewStyle().Foreground(dim),
		ErrorText:    lipgloss.NewStyle().Bold(true).Foreground(warn),
		DialogBox:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
	}
}

func humanTime(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2 2006")
	}
}
```

`internal/ui/listpane.go`:
```go
package ui

import (
	"strings"
	"time"

	"github.com/sahilm/fuzzy"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

// listPane is a hand-rolled scrolling session list: three terminal
// rows per item (title, meta, blank), fuzzy-filterable.
type listPane struct {
	sessions  []store.Session
	visible   []int // indexes into sessions, in display order
	cursor    int   // index into visible
	offset    int   // first visible row index (scrolling)
	width     int
	height    int
	filter    string
	showEmpty bool
	focused   bool
	styles    styles
}

func (l *listPane) SetSize(w, h int) {
	l.width, l.height = w, h
	l.clampScroll()
}

func (l *listPane) SetSessions(s []store.Session) {
	l.sessions = s
	l.refresh()
}

func (l *listPane) Sessions() []store.Session { return l.sessions }
func (l *listPane) Len() int                  { return len(l.visible) }

func (l *listPane) ApplyEnrich(i int, m store.Meta) {
	if i < 0 || i >= len(l.sessions) {
		return
	}
	l.sessions[i].Apply(m)
	l.refresh()
}

func (l *listPane) SetFilter(q string) {
	l.filter = q
	l.cursor = 0
	l.offset = 0
	l.refresh()
}

func (l *listPane) ToggleEmpty() {
	l.showEmpty = !l.showEmpty
	l.refresh()
}

func (l *listPane) MoveCursor(delta int) {
	l.cursor += delta
	if l.cursor < 0 {
		l.cursor = 0
	}
	if l.cursor >= len(l.visible) {
		l.cursor = len(l.visible) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	l.clampScroll()
}

func (l *listPane) Selected() (store.Session, int, bool) {
	if len(l.visible) == 0 {
		return store.Session{}, -1, false
	}
	i := l.visible[l.cursor]
	return l.sessions[i], i, true
}

func (l *listPane) RemoveSession(i int) {
	if i < 0 || i >= len(l.sessions) {
		return
	}
	l.sessions = append(l.sessions[:i], l.sessions[i+1:]...)
	l.refresh()
}

func haystack(s store.Session) string {
	return strings.ToLower(s.Title + " " + s.Project() + " " + s.FirstPrompt)
}

func (l *listPane) refresh() {
	l.visible = l.visible[:0]
	if l.filter == "" {
		for i, s := range l.sessions {
			if s.Empty() && !l.showEmpty {
				continue
			}
			l.visible = append(l.visible, i)
		}
	} else {
		targets := make([]string, len(l.sessions))
		for i, s := range l.sessions {
			targets[i] = haystack(s)
		}
		for _, m := range fuzzy.Find(strings.ToLower(l.filter), targets) {
			s := l.sessions[m.Index]
			if s.Empty() && !l.showEmpty {
				continue
			}
			l.visible = append(l.visible, m.Index)
		}
	}
	if l.cursor >= len(l.visible) {
		l.cursor = len(l.visible) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	l.clampScroll()
}

func (l *listPane) maxItems() int {
	n := l.height / 3
	if n < 1 {
		n = 1
	}
	return n
}

func (l *listPane) clampScroll() {
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+l.maxItems() {
		l.offset = l.cursor - l.maxItems() + 1
	}
	if l.offset < 0 {
		l.offset = 0
	}
}

func (l *listPane) View() string {
	if len(l.visible) == 0 {
		if l.filter != "" {
			return l.styles.ListMeta.Render("no matches")
		}
		return l.styles.ListMeta.Render("no sessions")
	}
	end := l.offset + l.maxItems()
	if end > len(l.visible) {
		end = len(l.visible)
	}
	var b strings.Builder
	for row := l.offset; row < end; row++ {
		s := l.sessions[l.visible[row]]
		title := s.Title
		if title == "" {
			if s.Enriched {
				title = "(untitled)"
			} else {
				title = s.ID
			}
		}
		meta := s.Project() + " · " + humanTime(s.LastActivity, time.Now())
		if s.GitBranch != "" {
			meta += " · " + s.GitBranch
		}
		if s.Unreadable {
			meta += " · (unreadable)"
		}
		prefix := "  "
		titleStyle, metaStyle := l.styles.ListTitle, l.styles.ListMeta
		if row == l.cursor {
			prefix = "▶ "
			titleStyle, metaStyle = l.styles.ListTitleSel, l.styles.ListMetaSel
		}
		b.WriteString(titleStyle.Render(store.Truncate(prefix+title, l.width)) + "\n")
		b.WriteString(metaStyle.Render(store.Truncate("  "+meta, l.width)) + "\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -v`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/styles.go internal/ui/listpane.go internal/ui/listpane_test.go
git commit -m "feat(ui): adaptive styles, humanTime, fuzzy-filterable list pane

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: ui — transcript preview rendering

**Files:**
- Create: `internal/ui/preview.go`
- Create: `internal/ui/preview_test.go`

**Interfaces:**
- Consumes: `store.Transcript`, `store.Message`, kinds from Task 4; `styles` from Task 6.
- Produces: `func renderTranscript(t store.Transcript, width int, st styles) string` (fed into a `viewport.Model` by Task 8).

- [ ] **Step 1: Write the failing test**

`internal/ui/preview_test.go`:
```go
package ui

import (
	"strings"
	"testing"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

func TestRenderTranscript(t *testing.T) {
	tr := store.Transcript{Messages: []store.Message{
		{Kind: store.KindUser, Text: "make slides from my notes"},
		{Kind: store.KindAssistant, Text: "I'll read the notes file first."},
		{Kind: store.KindTool, Text: "Bash: Run tests"},
	}}
	out := renderTranscript(tr, 40, defaultStyles())
	for _, want := range []string{"› make slides", "● I'll read", "⚒ Bash: Run tests"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderTranscriptEmpty(t *testing.T) {
	out := renderTranscript(store.Transcript{}, 40, defaultStyles())
	if !strings.Contains(out, "no messages") {
		t.Errorf("empty transcript should say 'no messages', got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestRenderTranscript -v`
Expected: FAIL (compile error: `renderTranscript` undefined).

- [ ] **Step 3: Write the implementation**

`internal/ui/preview.go`:
```go
package ui

import (
	"strings"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

// renderTranscript renders a transcript as styled, wrapped text for
// the preview viewport. Prefixes: › user, ● assistant, ⚒ tool call.
func renderTranscript(t store.Transcript, width int, st styles) string {
	if len(t.Messages) == 0 {
		return st.ListMeta.Render("no messages")
	}
	if width < 4 {
		width = 4
	}
	parts := make([]string, 0, len(t.Messages))
	for _, m := range t.Messages {
		var style = st.AssistantMsg
		prefix := "● "
		switch m.Kind {
		case store.KindUser:
			style, prefix = st.UserMsg, "› "
		case store.KindTool:
			style, prefix = st.ToolMsg, "⚒ "
		}
		parts = append(parts, style.Width(width).Render(prefix+m.Text))
	}
	return strings.Join(parts, "\n\n")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/preview.go internal/ui/preview_test.go
git commit -m "feat(ui): transcript preview rendering

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 8: ui — root model (browse: scan, enrich, filter, focus, preview)

**Files:**
- Create: `internal/ui/model.go`
- Create: `internal/ui/model_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2-7.
- Produces:
  - `func New(projectsDir string) Model` — the `tea.Model` used by `cmd/cs`.
  - Model fields Task 9 extends: `dialog dialogKind`, `pendingDelete int`, `pendingResume *store.Session`, `dirs []string`, `dirCursor int`, `dirInput textinput.Model`, `errText string`, `trashFn func(string, store.Session) (string, error)`, `runClaude func(dir string, args ...string) tea.Cmd`.
  - Message types: `scanDoneMsg`, `enrichMsg`, `enrichDoneMsg`, `transcriptMsg`, `claudeExitMsg`, `claudeMissingMsg`.
  - In this task the `enter`, `n`, and `d` keys are wired to methods `startResume`, `openNewSession`, `askDelete` that Task 9 implements — Task 8 ships them as no-op stubs (bodies below) so the package compiles and browsing works.

- [ ] **Step 1: Write the failing test**

`internal/ui/model_test.go`:
```go
package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func newTestModel() Model {
	m := New("/nonexistent-projects-dir")
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(Model)
	m2, _ = m.Update(scanDoneMsg{sessions: testSessions()})
	m = m2.(Model)
	for i, s := range testSessions() {
		m2, _ = m.Update(enrichMsg{Index: i, Meta: store.Meta{
			Title: s.Title, FirstPrompt: s.FirstPrompt, CWD: s.CWD,
			UserMessages: s.UserMessages, LastActivity: s.LastActivity,
		}})
		m = m2.(Model)
	}
	return m
}

func TestSlashEntersFilterMode(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	if m.focus != focusFilter {
		t.Fatalf("focus = %v, want focusFilter", m.focus)
	}
	for _, r := range "backup" {
		m2, _ = m.Update(key(string(r)))
		m = m2.(Model)
	}
	s, _, ok := m.list.Selected()
	if !ok || s.ID != "s2" {
		t.Errorf("filtered selection = %v, want s2", s.ID)
	}
	// enter keeps filter, returns focus to list
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.focus != focusList || m.list.filter != "backup" {
		t.Errorf("after enter: focus=%v filter=%q", m.focus, m.list.filter)
	}
	// esc clears filter
	m2, _ = m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	if m.list.filter != "" {
		t.Errorf("esc should clear filter, got %q", m.list.filter)
	}
}

func TestTabTogglesFocus(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	if m.focus != focusPreview {
		t.Fatalf("focus = %v, want focusPreview", m.focus)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	if m.focus != focusList {
		t.Fatalf("focus = %v, want focusList", m.focus)
	}
}

func TestJKMoveCursor(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(key("j"))
	m = m2.(Model)
	s, _, _ := m.list.Selected()
	if s.ID != "s2" {
		t.Errorf("after j: selected %s, want s2", s.ID)
	}
	m2, _ = m.Update(key("k"))
	m = m2.(Model)
	s, _, _ = m.list.Selected()
	if s.ID != "s1" {
		t.Errorf("after k: selected %s, want s1", s.ID)
	}
}

func TestEmptyToggleKey(t *testing.T) {
	m := newTestModel()
	before := m.list.Len()
	m2, _ := m.Update(key("e"))
	m = m2.(Model)
	if m.list.Len() != before+1 {
		t.Errorf("Len = %d, want %d", m.list.Len(), before+1)
	}
}

func TestQuitKey(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(key("q"))
	if cmd == nil {
		t.Fatal("q should return a quit cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q cmd should produce tea.QuitMsg")
	}
}

func TestViewRenders(t *testing.T) {
	m := newTestModel()
	v := m.View()
	if v == "" {
		t.Fatal("empty view")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'TestSlash|TestTab|TestJK|TestEmptyToggleKey|TestQuit|TestViewRenders' -v`
Expected: FAIL (compile error: `New` undefined).

- [ ] **Step 3: Write the implementation**

`internal/ui/model.go`:
```go
package ui

import (
	"fmt"
	"os/exec"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

type focusArea int

const (
	focusList focusArea = iota
	focusPreview
	focusFilter
)

type dialogKind int

const (
	dialogNone dialogKind = iota
	dialogDelete
	dialogPickDir
	dialogError
)

type (
	scanDoneMsg struct {
		sessions []store.Session
		err      error
	}
	enrichMsg     store.EnrichResult
	enrichDoneMsg struct{}
	transcriptMsg struct {
		id  string
		t   store.Transcript
		err error
	}
	claudeExitMsg    struct{ err error }
	claudeMissingMsg struct{}
)

type Model struct {
	projectsDir string
	st          styles

	list        listPane
	preview     viewport.Model
	filterInput textinput.Model
	focus       focusArea

	dialog        dialogKind
	errText       string
	pendingDelete int
	pendingResume *store.Session
	dirs          []string
	dirCursor     int
	dirInput      textinput.Model

	cache      *store.TranscriptCache
	enrichCh   chan store.EnrichResult
	previewFor string
	loading    bool

	width, height int
	ready         bool

	// injected for tests
	trashFn   func(string, store.Session) (string, error)
	runClaude func(dir string, args ...string) tea.Cmd
}

func New(projectsDir string) Model {
	st := defaultStyles()
	fi := textinput.New()
	fi.Placeholder = "filter…"
	fi.Prompt = "🔍 "
	di := textinput.New()
	di.Placeholder = "…or type a path"
	di.Prompt = "> "
	return Model{
		projectsDir:   projectsDir,
		st:            st,
		list:          listPane{styles: st},
		filterInput:   fi,
		dirInput:      di,
		cache:         store.NewTranscriptCache(8),
		pendingDelete: -1,
		trashFn:       store.TrashSession,
		runClaude:     execClaude,
	}
}

func execClaude(dir string, args ...string) tea.Cmd {
	c := exec.Command("claude", args...)
	c.Dir = dir
	return tea.ExecProcess(c, func(err error) tea.Msg { return claudeExitMsg{err} })
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.scanCmd(), checkClaudeCmd)
}

func checkClaudeCmd() tea.Msg {
	if _, err := exec.LookPath("claude"); err != nil {
		return claudeMissingMsg{}
	}
	return nil
}

func (m Model) scanCmd() tea.Cmd {
	dir := m.projectsDir
	return func() tea.Msg {
		sessions, err := store.Scan(dir)
		return scanDoneMsg{sessions: sessions, err: err}
	}
}

func waitEnrich(ch chan store.EnrichResult) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return enrichDoneMsg{}
		}
		return enrichMsg(r)
	}
}

func (m *Model) loadTranscriptCmd() tea.Cmd {
	s, _, ok := m.list.Selected()
	if !ok {
		m.preview.SetContent("")
		m.previewFor = ""
		return nil
	}
	if s.ID == m.previewFor {
		return nil
	}
	m.previewFor = s.ID
	cache, path, id := m.cache, s.Path, s.ID
	return func() tea.Msg {
		t, err := cache.Get(path)
		t.SessionID = id
		return transcriptMsg{id: id, t: t, err: err}
	}
}

// narrow reports whether the terminal is too narrow for two panes;
// below 80 columns the preview pane is hidden (per spec).
func (m Model) narrow() bool { return m.width < 80 }

// Layout: 1 header row + 1 filter row + body + 1 help row; borders eat
// 2 rows/cols per pane.
func (m *Model) layout() {
	bodyH := m.height - 5
	if bodyH < 3 {
		bodyH = 3
	}
	listW := m.width * 2 / 5
	if listW < 20 {
		listW = 20
	}
	if m.narrow() {
		listW = m.width - 2
	}
	previewW := m.width - listW - 4
	if previewW < 10 {
		previewW = 10
	}
	m.list.SetSize(listW-2, bodyH)
	if !m.ready {
		m.preview = viewport.New(previewW, bodyH)
		m.ready = true
	} else {
		m.preview.Width = previewW
		m.preview.Height = bodyH
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case scanDoneMsg:
		if msg.err != nil {
			m.dialog = dialogError
			m.errText = fmt.Sprintf("cannot read %s: %v", m.projectsDir, msg.err)
			return m, nil
		}
		m.list.SetSessions(msg.sessions)
		m.previewFor = ""
		if len(msg.sessions) == 0 {
			return m, nil
		}
		ch := make(chan store.EnrichResult, len(msg.sessions))
		m.enrichCh = ch
		m.loading = true
		store.Enrich(msg.sessions, 8, ch)
		return m, tea.Batch(waitEnrich(ch), m.loadTranscriptCmd())

	case enrichMsg:
		if msg.Err != nil {
			if msg.Index >= 0 && msg.Index < len(m.list.sessions) {
				m.list.sessions[msg.Index].Unreadable = true
				m.list.sessions[msg.Index].Enriched = true
			}
		} else {
			m.list.ApplyEnrich(msg.Index, msg.Meta)
		}
		cmd := m.loadTranscriptCmd()
		return m, tea.Batch(waitEnrich(m.enrichCh), cmd)

	case enrichDoneMsg:
		m.loading = false
		return m, m.loadTranscriptCmd()

	case transcriptMsg:
		if msg.id != m.previewFor {
			return m, nil // stale response for a de-selected session
		}
		if msg.err != nil {
			m.preview.SetContent(m.st.ErrorText.Render(msg.err.Error()))
			return m, nil
		}
		m.preview.SetContent(renderTranscript(msg.t, m.preview.Width, m.st))
		m.preview.GotoTop()
		return m, nil

	case claudeMissingMsg:
		m.dialog = dialogError
		m.errText = "claude not found on PATH — install Claude Code first"
		return m, nil

	case claudeExitMsg:
		if msg.err != nil {
			m.dialog = dialogError
			m.errText = "claude exited with error: " + msg.err.Error()
		}
		return m, m.scanCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.dialog != dialogNone {
		return m.handleDialogKey(msg)
	}
	switch m.focus {
	case focusFilter:
		switch msg.Type {
		case tea.KeyEsc:
			m.filterInput.SetValue("")
			m.filterInput.Blur()
			m.list.SetFilter("")
			m.focus = focusList
			return m, m.loadTranscriptCmd()
		case tea.KeyEnter:
			m.filterInput.Blur()
			m.focus = focusList
			return m, nil
		}
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		m.list.SetFilter(m.filterInput.Value())
		return m, tea.Batch(cmd, m.loadTranscriptCmd())

	case focusPreview:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "tab", "esc":
			m.focus = focusList
			return m, nil
		}
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd

	default: // focusList
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "tab":
			m.focus = focusPreview
			return m, nil
		case "/":
			m.focus = focusFilter
			m.filterInput.Focus()
			return m, nil
		case "j", "down":
			m.list.MoveCursor(1)
			return m, m.loadTranscriptCmd()
		case "k", "up":
			m.list.MoveCursor(-1)
			return m, m.loadTranscriptCmd()
		case "e":
			m.list.ToggleEmpty()
			return m, m.loadTranscriptCmd()
		case "r":
			return m, m.scanCmd()
		case "enter":
			return m.startResume()
		case "n":
			return m.openNewSession()
		case "d":
			return m.askDelete()
		}
	}
	return m, nil
}

// Stubs completed in the actions task.
func (m Model) startResume() (tea.Model, tea.Cmd)    { return m, nil }
func (m Model) openNewSession() (tea.Model, tea.Cmd) { return m, nil }
func (m Model) askDelete() (tea.Model, tea.Cmd)      { return m, nil }
func (m Model) handleDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.dialog = dialogNone
	return m, nil
}
func (m Model) dialogView() string { return "" }

func (m Model) View() string {
	if !m.ready {
		return "loading…"
	}
	count := fmt.Sprintf("%d sessions", m.list.Len())
	if m.loading {
		count += " · scanning…"
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		m.st.AppTitle.Render(" cs · Claude Sessions  "),
		m.st.Count.Render(count),
	)
	filterBar := m.filterInput.View()

	var body string
	if m.dialog != dialogNone {
		body = lipgloss.Place(m.width, m.height-3, lipgloss.Center, lipgloss.Center, m.dialogView())
	} else {
		listStyle, prevStyle := m.st.PaneBlurred, m.st.PaneBlurred
		if m.focus == focusPreview {
			prevStyle = m.st.PaneFocused
		} else {
			listStyle = m.st.PaneFocused
		}
		if m.narrow() {
			body = listStyle.Render(m.list.View())
		} else {
			body = lipgloss.JoinHorizontal(lipgloss.Top,
				listStyle.Render(m.list.View()),
				prevStyle.Render(m.preview.View()),
			)
		}
	}

	help := m.st.Help.Render(" ↵ resume  tab focus  n new  d delete  / filter  e empty  r rescan  q quit")
	return header + "\n" + filterBar + "\n" + body + "\n" + help
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -v`
Expected: PASS (all ui tests).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): root model with scan/enrich wiring, filter, focus, preview

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 9: ui — actions: resume, new session, delete, dialogs

**Files:**
- Modify: `internal/ui/model.go` (replace the five stub methods at the bottom; everything else in the file stays as Task 8 wrote it)
- Create: `internal/ui/actions_test.go`

**Interfaces:**
- Consumes: Model from Task 8; `store.TrashSession`, `store.KnownDirs`.
- Produces: working `enter`/`n`/`d` keys and dialog handling. No new exported API.

- [ ] **Step 1: Write the failing test**

`internal/ui/actions_test.go`:
```go
package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

// resumeRecorder stands in for runClaude and records invocations.
type resumeRecorder struct {
	dir  string
	args []string
}

func (r *resumeRecorder) cmd(dir string, args ...string) tea.Cmd {
	r.dir = dir
	r.args = args
	return func() tea.Msg { return nil }
}

func modelWithRealCWD(t *testing.T) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	m := newTestModel()
	// point first session at a directory that exists
	m.list.sessions[0].CWD = dir
	return m, dir
}

func TestEnterResumesInSessionCWD(t *testing.T) {
	m, dir := modelWithRealCWD(t)
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("enter should return a cmd")
	}
	if rec.dir != dir {
		t.Errorf("resume dir = %q, want %q", rec.dir, dir)
	}
	if len(rec.args) != 2 || rec.args[0] != "--resume" || rec.args[1] != "s1" {
		t.Errorf("args = %v, want [--resume s1]", rec.args)
	}
}

func TestEnterMissingCWDOpensPicker(t *testing.T) {
	m := newTestModel()
	m.list.sessions[0].CWD = "/definitely/not/a/real/dir"
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.dialog != dialogPickDir {
		t.Fatalf("dialog = %v, want dialogPickDir", m.dialog)
	}
	if m.pendingResume == nil || m.pendingResume.ID != "s1" {
		t.Error("pendingResume not set")
	}
}

func TestDeleteFlow(t *testing.T) {
	m := newTestModel()
	trashed := ""
	m.trashFn = func(projectsDir string, s store.Session) (string, error) {
		trashed = s.ID
		return "/trash/" + s.ID, nil
	}
	before := m.list.Len()
	m2, _ := m.Update(key("d"))
	m = m2.(Model)
	if m.dialog != dialogDelete {
		t.Fatalf("dialog = %v, want dialogDelete", m.dialog)
	}
	m2, _ = m.Update(key("y"))
	m = m2.(Model)
	if trashed != "s1" {
		t.Errorf("trashed %q, want s1", trashed)
	}
	if m.dialog != dialogNone || m.list.Len() != before-1 {
		t.Errorf("after delete: dialog=%v len=%d", m.dialog, m.list.Len())
	}
}

func TestDeleteCancel(t *testing.T) {
	m := newTestModel()
	m.trashFn = func(string, store.Session) (string, error) {
		t.Error("trashFn must not be called on cancel")
		return "", nil
	}
	m2, _ := m.Update(key("d"))
	m = m2.(Model)
	m2, _ = m.Update(key("n"))
	m = m2.(Model)
	if m.dialog != dialogNone || m.list.Len() != 2 {
		t.Errorf("cancel failed: dialog=%v len=%d", m.dialog, m.list.Len())
	}
}

func TestNewSessionPicker(t *testing.T) {
	dir := t.TempDir()
	m := newTestModel()
	m.list.sessions[0].CWD = dir // KnownDirs needs an existing dir
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
	m2, _ := m.Update(key("n"))
	m = m2.(Model)
	if m.dialog != dialogPickDir {
		t.Fatalf("dialog = %v, want dialogPickDir", m.dialog)
	}
	if len(m.dirs) == 0 || m.dirs[0] != dir {
		t.Fatalf("dirs = %v, want [%s]", m.dirs, dir)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if rec.dir != dir || len(rec.args) != 0 {
		t.Errorf("new session: dir=%q args=%v, want dir=%q args=[]", rec.dir, rec.args, dir)
	}
	if m.dialog != dialogNone {
		t.Error("dialog should close after picking")
	}
}

func TestNewSessionTypedPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	sub := filepath.Join(home, ".cache")
	if _, err := os.Stat(sub); err != nil {
		t.Skip("~/.cache missing")
	}
	m := newTestModel()
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
	m2, _ := m.Update(key("n"))
	m = m2.(Model)
	for _, r := range "~/.cache" {
		m2, _ = m.Update(key(string(r)))
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if rec.dir != sub {
		t.Errorf("typed path resumed in %q, want %q", rec.dir, sub)
	}
}

func TestErrorDialogDismiss(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(claudeMissingMsg{})
	m = m2.(Model)
	if m.dialog != dialogError {
		t.Fatal("claudeMissingMsg should open error dialog")
	}
	m2, _ = m.Update(key("x"))
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Error("any key should dismiss error dialog")
	}
}
```

Note: `newTestModel` (from `model_test.go`) builds sessions whose CWDs (`/x/alpha`, `/x/beta`) do not exist — tests above override CWD when they need an existing one.

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'TestEnter|TestDelete|TestNewSession|TestErrorDialog' -v`
Expected: FAIL (stub methods do nothing, so dialogs never open / recorder never called).

- [ ] **Step 3: Replace the stubs in `internal/ui/model.go`**

Delete these five stub definitions from the bottom of `model.go`:

```go
func (m Model) startResume() (tea.Model, tea.Cmd)    { return m, nil }
func (m Model) openNewSession() (tea.Model, tea.Cmd) { return m, nil }
func (m Model) askDelete() (tea.Model, tea.Cmd)      { return m, nil }
func (m Model) handleDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.dialog = dialogNone
	return m, nil
}
func (m Model) dialogView() string { return "" }
```

and add in their place (also add `"os"`, `"path/filepath"`, `"strings"` to the file's imports):

```go
func (m Model) startResume() (tea.Model, tea.Cmd) {
	s, _, ok := m.list.Selected()
	if !ok {
		return m, nil
	}
	if st, err := os.Stat(s.CWD); s.CWD == "" || err != nil || !st.IsDir() {
		sess := s
		m.pendingResume = &sess
		m.openDirPicker()
		return m, nil
	}
	return m, m.runClaude(s.CWD, "--resume", s.ID)
}

func (m Model) openNewSession() (tea.Model, tea.Cmd) {
	m.pendingResume = nil
	m.openDirPicker()
	return m, nil
}

func (m *Model) openDirPicker() {
	m.dirs = store.KnownDirs(m.list.Sessions())
	m.dirCursor = 0
	m.dirInput.SetValue("")
	m.dirInput.Focus()
	m.dialog = dialogPickDir
}

func (m Model) askDelete() (tea.Model, tea.Cmd) {
	if _, idx, ok := m.list.Selected(); ok {
		m.pendingDelete = idx
		m.dialog = dialogDelete
	}
	return m, nil
}

func (m Model) handleDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.dialog {
	case dialogError:
		m.dialog = dialogNone
		m.errText = ""
		return m, nil

	case dialogDelete:
		switch msg.String() {
		case "y", "enter":
			idx := m.pendingDelete
			m.pendingDelete = -1
			m.dialog = dialogNone
			if idx >= 0 && idx < len(m.list.sessions) {
				s := m.list.sessions[idx]
				if _, err := m.trashFn(m.projectsDir, s); err != nil {
					m.dialog = dialogError
					m.errText = "delete failed: " + err.Error()
					return m, nil
				}
				m.list.RemoveSession(idx)
				m.previewFor = ""
			}
			return m, m.loadTranscriptCmd()
		case "n", "esc":
			m.pendingDelete = -1
			m.dialog = dialogNone
			return m, nil
		}
		return m, nil

	case dialogPickDir:
		switch msg.Type {
		case tea.KeyEsc:
			m.dialog = dialogNone
			m.pendingResume = nil
			m.dirInput.Blur()
			return m, nil
		case tea.KeyUp, tea.KeyDown:
			delta := 1
			if msg.Type == tea.KeyUp {
				delta = -1
			}
			m.dirCursor += delta
			if m.dirCursor < 0 {
				m.dirCursor = 0
			}
			if m.dirCursor >= len(m.dirs) {
				m.dirCursor = len(m.dirs) - 1
			}
			return m, nil
		case tea.KeyEnter:
			dir := strings.TrimSpace(m.dirInput.Value())
			if dir == "" {
				if m.dirCursor < 0 || m.dirCursor >= len(m.dirs) {
					return m, nil
				}
				dir = m.dirs[m.dirCursor]
			}
			if strings.HasPrefix(dir, "~") {
				if home, err := os.UserHomeDir(); err == nil {
					dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
				}
			}
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				m.dialog = dialogError
				m.errText = "not a directory: " + dir
				m.pendingResume = nil
				return m, nil
			}
			pending := m.pendingResume
			m.pendingResume = nil
			m.dialog = dialogNone
			m.dirInput.Blur()
			if pending != nil {
				return m, m.runClaude(dir, "--resume", pending.ID)
			}
			return m, m.runClaude(dir)
		}
		var cmd tea.Cmd
		m.dirInput, cmd = m.dirInput.Update(msg)
		return m, cmd
	}
	m.dialog = dialogNone
	return m, nil
}

func (m Model) dialogView() string {
	switch m.dialog {
	case dialogError:
		return m.st.DialogBox.Render(
			m.st.ErrorText.Render("Error") + "\n\n" + m.errText + "\n\n" +
				m.st.Help.Render("press any key"))

	case dialogDelete:
		title := ""
		if m.pendingDelete >= 0 && m.pendingDelete < len(m.list.sessions) {
			s := m.list.sessions[m.pendingDelete]
			title = s.Title
			if title == "" {
				title = s.ID
			}
			title += "  (" + s.Project() + ")"
		}
		return m.st.DialogBox.Render(
			"Move session to trash?\n\n  " + title + "\n\n" +
				m.st.Help.Render("y confirm · n cancel"))

	case dialogPickDir:
		var b strings.Builder
		header := "Start new session in:"
		if m.pendingResume != nil {
			header = "Original directory is gone. Resume in:"
		}
		b.WriteString(header + "\n\n")
		if len(m.dirs) == 0 {
			b.WriteString(m.st.ListMeta.Render("  (no known directories)") + "\n")
		}
		for i, d := range m.dirs {
			line := "  " + d
			if i == m.dirCursor {
				line = m.st.ListTitleSel.Render("▶ " + d)
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n" + m.dirInput.View() + "\n\n")
		b.WriteString(m.st.Help.Render("↑/↓ pick · type a path · ↵ go · esc cancel"))
		return m.st.DialogBox.Render(b.String())
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./... `
Expected: PASS (all packages).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/actions_test.go
git commit -m "feat(ui): resume, new-session picker, delete-to-trash, error dialogs

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 10: wire main, README, install

**Files:**
- Modify: `cmd/cs/main.go` (full replacement below)
- Create: `README.md`

**Interfaces:**
- Consumes: `ui.New` from Task 8.
- Produces: the finished `cs` binary with `--projects-dir` and `--version` flags.

- [ ] **Step 1: Replace `cmd/cs/main.go`**

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/claude-sessions/internal/ui"
)

var version = "0.1.0"

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cs:", err)
		os.Exit(1)
	}
	projectsDir := flag.String("projects-dir", filepath.Join(home, ".claude", "projects"), "Claude Code projects directory")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("cs", version)
		return
	}
	p := tea.NewProgram(ui.New(*projectsDir), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "cs:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Write `README.md`**

```markdown
# cs — Claude Code session manager

A single-binary TUI that lists every Claude Code session on the machine,
previews transcripts, and resumes any session in its original directory.

## Install

    make install        # builds and copies to ~/.local/bin/cs

Requires Go ≥1.24 to build; the binary itself has no dependencies.

## Use

    cs                      # browse ~/.claude/projects
    cs --projects-dir DIR   # browse another location

| Key | Action |
|---|---|
| `↑/↓` `j/k` | move selection |
| `enter` | resume selected session (`claude --resume` in its cwd) |
| `tab` | toggle focus list ⇄ preview |
| `/` | fuzzy filter (enter keeps it, esc clears it) |
| `n` | new session in a picked directory |
| `d` | delete session (moved to `~/.claude/projects/.trash/`, never rm'd) |
| `e` | show/hide empty sessions |
| `r` | rescan |
| `q` | quit |

Deleted sessions are recoverable: move the `.jsonl` back out of `.trash/`.
```

- [ ] **Step 3: Verify build, vet, tests, flags**

```bash
export PATH=$HOME/.local/go/bin:$PATH
make vet && make test && make build
./cs --version
```

Expected: vet clean, all tests PASS, `cs 0.1.0` printed.

- [ ] **Step 4: Commit**

```bash
git add cmd/cs/main.go README.md
git commit -m "feat: wire TUI into main, add README

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 11: end-to-end verification (GeneralServer + auxserver)

**Files:** none (verification only).

**Interfaces:**
- Consumes: the built `./cs` binary.
- Produces: verified behavior against real session data on two machines.

- [ ] **Step 1: Run against real data locally**

```bash
export PATH=$HOME/.local/go/bin:$PATH && make build
./cs
```

Manual checklist (interactive; a human or an interactive-capable agent must drive it):
1. List shows ~120 sessions sorted by recency, with titles ("Create slides from Hyper_SAGNN notes" should appear near hyper-sagnn sessions).
2. `j/k` moves; preview shows the selected session's conversation.
3. `/` then `slides` filters to slide-related sessions; `esc` restores.
4. `e` reveals additional (empty) sessions.
5. `enter` on a session opens Claude Code in the right directory (check with `/status` inside claude, then exit) and returns to the list.
6. `n` shows the directory picker with real project dirs.
7. `d` on a disposable session moves its file into `~/.claude/projects/.trash/` (verify with `ls`).
8. `q` quits and restores the terminal.

If the TUI cannot be driven interactively in this environment, verify what is scriptable and report the rest as pending manual verification — do not claim it verified:

```bash
./cs --version
printf 'q' | ./cs --projects-dir ~/.claude/projects 2>&1 | head -5   # must not panic
```

- [ ] **Step 2: Verify on auxserver**

```bash
scp ./cs auxserver:/tmp/cs
ssh auxserver '/tmp/cs --version && ls ~/.claude/projects | head -3'
```

Expected: `cs 0.1.0`; auxserver has its own session dirs. Then instruct William to run `ssh -t auxserver /tmp/cs` for an interactive check there (its sessions belong to user `will`).

- [ ] **Step 3: Install and tag**

```bash
make install
~/.local/bin/cs --version
git tag v0.1.0
```

Expected: `cs 0.1.0`.
