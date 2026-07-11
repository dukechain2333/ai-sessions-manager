# Codex Session Support — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OpenAI Codex sessions to `sm` alongside Claude Code sessions — listed, colored distinctly, resumable, with an optional per-project agent-grouping view.

**Architecture:** Introduce a `Provider` interface in `internal/store`; the existing Claude logic moves behind `claudeProvider`, a new `codexProvider` reads `~/.codex/sessions/**/rollout-*.jsonl`. `Session` gains an `Agent` field. `store.ScanAll` merges providers; enrich/transcript/trash/resume dispatch on `Session.Agent`. The UI colors sessions by agent and adds an `a` agent-grouping toggle.

**Tech Stack:** Go ≥1.24, Bubble Tea v1, Lipgloss v1. No new dependencies.

## Global Constraints

- Module `github.com/dukechain2333/ai-sessions-manager`; binary `sm`. Go at `~/.local/go` (prefix shell steps with `export PATH=$HOME/.local/go/bin:$PATH`).
- Session files are NEVER deleted with `rm`/`os.Remove` — only `os.Rename` into a `.trash/` dir.
- All colors are `lipgloss.AdaptiveColor`. Claude accent stays `{Light:"#C15F3C", Dark:"#D97757"}`. Codex accent is OpenAI teal-green `{Light:"#0A7C66", Dark:"#10A37F"}`.
- Codex support activates only when `~/.codex` exists; Claude-only users see no change.
- `Project()` is `basename(CWD)` for both agents (shared project groups).
- `gofmt`/`go vet` clean; `go test -race ./...` green; cross-compiles linux+darwin / amd64+arm64.
- Every commit message ends with: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- After all tasks: `make install` to `~/.local/bin/sm`. DO NOT cut a release / push a tag / publish until the user explicitly asks.

## File map

- Create `internal/store/agent.go` — `Agent` type + constants.
- Create `internal/store/provider.go` — `Provider` interface, registry, `ScanAll`.
- Create `internal/store/claude.go` — `claudeProvider` wrapping existing Claude funcs.
- Create `internal/store/codex.go` + `internal/store/codex_test.go` — Codex provider.
- Create `internal/store/codex_testdata/*.jsonl` — fixtures.
- Modify `internal/store/session.go` — add `Agent` field.
- Modify `internal/store/scan.go` — `Enrich` dispatches parse via provider.
- Modify `internal/ui/styles.go` — Codex accent + per-agent styles.
- Modify `internal/ui/listpane.go` (+ `listpane_test.go`) — agent coloring, `groupByAgent`, subheader rows.
- Modify `internal/ui/model.go` (+ tests) — provider-aware scan/enrich, per-agent resume/new/delete, agent-pick dialog, `a` key.
- Modify `cmd/sm/main.go`, `README.md`.

---

### Task 1: Agent type and Session.Agent field

**Files:**
- Create: `internal/store/agent.go`
- Modify: `internal/store/session.go` (add field)
- Test: `internal/store/agent_test.go`

**Interfaces:**
- Produces: `type Agent string`; `const AgentClaude Agent = "claude"`, `AgentCodex Agent = "codex"`; `func (a Agent) Label() string`; `Session.Agent Agent` field.

- [ ] **Step 1: Write the failing test**

`internal/store/agent_test.go`:
```go
package store

import "testing"

func TestAgentLabel(t *testing.T) {
	if AgentClaude.Label() != "claude" || AgentCodex.Label() != "codex" {
		t.Errorf("labels: %q %q", AgentClaude.Label(), AgentCodex.Label())
	}
}

func TestSessionHasAgent(t *testing.T) {
	s := Session{Agent: AgentCodex}
	if s.Agent != AgentCodex {
		t.Errorf("Agent = %q", s.Agent)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run 'TestAgentLabel|TestSessionHasAgent'`
Expected: FAIL (Agent/AgentClaude undefined).

- [ ] **Step 3: Create agent.go and add the field**

`internal/store/agent.go`:
```go
package store

// Agent identifies which coding agent produced a session.
type Agent string

const (
	AgentClaude Agent = "claude"
	AgentCodex  Agent = "codex"
)

// Label is the lowercase agent name shown in the UI.
func (a Agent) Label() string { return string(a) }
```

In `internal/store/session.go`, add the field to the `Session` struct after `Slug`:
```go
	Slug          string // directory name under the projects dir (Claude only)
	Agent         Agent  // which agent produced the session
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run 'TestAgentLabel|TestSessionHasAgent' && go build ./...`
Expected: PASS, builds.

- [ ] **Step 5: Commit**

```bash
git add internal/store/agent.go internal/store/agent_test.go internal/store/session.go
git commit -m "feat(store): Agent type and Session.Agent field

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Provider interface, registry, claudeProvider

**Files:**
- Create: `internal/store/provider.go`
- Create: `internal/store/claude.go`
- Test: `internal/store/provider_test.go`

**Interfaces:**
- Consumes: existing `Scan`, `ParseMetadata`, `ParseTranscript`, `TrashSession`, `ResolveSlug`; `Agent` from Task 1.
- Produces:
  - `type Provider interface { Agent() Agent; Available() bool; Scan() ([]Session, error); ParseMetadata(path string) (Meta, error); ParseTranscript(path string) (Transcript, error); Trash(s Session) (string, error); ResumeCommand(s Session) (string, []string); NewCommand() (string, []string); Binary() string }`
  - `func NewClaudeProvider(projectsDir string) Provider`
  - `func ScanAll(providers []Provider) ([]Session, error)` — merged, LastActivity desc.
  - `func ProviderFor(providers []Provider, a Agent) Provider` — nil if none.

- [ ] **Step 1: Write the failing test**

`internal/store/provider_test.go`:
```go
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
```

Add this helper to `internal/store/scan_test.go` (used above and by later tasks):
```go
func touch(t *testing.T, path string, mt time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatal(err)
	}
}
```
(`os` and `time` are already imported in scan_test.go.)

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run 'TestClaudeProvider|TestScanAll'`
Expected: FAIL (NewClaudeProvider undefined).

- [ ] **Step 3: Write provider.go and claude.go**

`internal/store/provider.go`:
```go
package store

import "sort"

// Provider is one agent's view of session storage: how to find sessions,
// parse them, resume/create them, and trash them. Each provider owns its
// own base directory.
type Provider interface {
	Agent() Agent
	Available() bool
	Scan() ([]Session, error)
	ParseMetadata(path string) (Meta, error)
	ParseTranscript(path string) (Transcript, error)
	Trash(s Session) (string, error)
	ResumeCommand(s Session) (name string, args []string)
	NewCommand() (name string, args []string)
	Binary() string
}

// ProviderFor returns the provider handling agent a, or nil.
func ProviderFor(providers []Provider, a Agent) Provider {
	for _, p := range providers {
		if p.Agent() == a {
			return p
		}
	}
	return nil
}

// ScanAll runs every available provider's Scan and returns the merged
// sessions sorted by LastActivity descending.
func ScanAll(providers []Provider) ([]Session, error) {
	var all []Session
	for _, p := range providers {
		if !p.Available() {
			continue
		}
		ss, err := p.Scan()
		if err != nil {
			return nil, err
		}
		all = append(all, ss...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].LastActivity.After(all[j].LastActivity)
	})
	return all, nil
}
```

`internal/store/claude.go`:
```go
package store

import "os"

// claudeProvider serves Claude Code sessions under projectsDir
// (~/.claude/projects). It reuses the package's existing Claude parsers.
type claudeProvider struct{ projectsDir string }

// NewClaudeProvider builds the Claude provider for projectsDir.
func NewClaudeProvider(projectsDir string) Provider {
	return claudeProvider{projectsDir: projectsDir}
}

func (claudeProvider) Agent() Agent   { return AgentClaude }
func (claudeProvider) Binary() string { return "claude" }

func (p claudeProvider) Available() bool {
	info, err := os.Stat(p.projectsDir)
	return err == nil && info.IsDir()
}

func (p claudeProvider) Scan() ([]Session, error) {
	ss, err := Scan(p.projectsDir)
	if err != nil {
		return nil, err
	}
	for i := range ss {
		ss[i].Agent = AgentClaude
	}
	return ss, nil
}

func (claudeProvider) ParseMetadata(path string) (Meta, error) { return ParseMetadata(path) }
func (claudeProvider) ParseTranscript(path string) (Transcript, error) {
	return ParseTranscript(path)
}
func (p claudeProvider) Trash(s Session) (string, error) {
	return TrashSession(p.projectsDir, s)
}
func (claudeProvider) ResumeCommand(s Session) (string, []string) {
	return "claude", []string{"--resume", s.ID}
}
func (claudeProvider) NewCommand() (string, []string) { return "claude", nil }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run 'TestClaudeProvider|TestScanAll' && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/provider.go internal/store/claude.go internal/store/provider_test.go internal/store/scan_test.go
git commit -m "feat(store): Provider interface, ScanAll, claudeProvider

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: codexProvider — scan and metadata

**Files:**
- Create: `internal/store/codex.go`
- Create: `internal/store/codex_test.go`
- Create: `internal/store/codex_testdata/rollout-basic.jsonl`, `internal/store/codex_testdata/rollout-empty.jsonl`

**Interfaces:**
- Consumes: `Meta`, `Session`, `Truncate`, `Agent`, `newScanner`.
- Produces: `func NewCodexProvider(sessionsDir string) Provider` with working `Available`, `Scan`, `ParseMetadata` (Trash/ParseTranscript/commands are stubs completed in Task 4). `codexProvider.Scan` sets `Agent=AgentCodex`, `ID` = uuid from filename, `Path`, `LastActivity` = file mtime.

- [ ] **Step 1: Write fixtures**

`internal/store/codex_testdata/rollout-basic.jsonl` (matches real Codex format; line 4 malformed on purpose):
```
{"timestamp":"2026-06-26T03:52:47.516Z","type":"session_meta","payload":{"session_id":"019f020e-d6ab-7ff2-99b4-c3274454ea14","cwd":"/home/w/proj","timestamp":"2026-06-26T03:52:34.743Z","cli_version":"0.142.2"}}
{"timestamp":"2026-06-26T03:52:48.000Z","type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"<permissions instructions>ignore me"}]}}
{"timestamp":"2026-06-26T03:52:49.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context><cwd>/home/w/proj</cwd></environment_context>"}]}}
not valid json {{{
{"timestamp":"2026-06-26T03:52:50.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Identify the bug in this project"}]}}
{"timestamp":"2026-06-26T03:52:55.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I'll inspect the repo first."}]}}
{"timestamp":"2026-06-26T03:52:56.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"git status --short\"}"}}
{"timestamp":"2026-06-26T03:52:57.000Z","type":"response_item","payload":{"type":"reasoning","content":[{"type":"reasoning_text","text":"thinking..."}]}}
{"timestamp":"2026-06-26T03:52:58.000Z","type":"event_msg","payload":{"type":"task_started"}}
```

`internal/store/codex_testdata/rollout-empty.jsonl` (no real user prompt):
```
{"timestamp":"2026-06-26T03:52:47.516Z","type":"session_meta","payload":{"session_id":"aaaa","cwd":"/home/w/proj","timestamp":"2026-06-26T03:52:34.743Z"}}
{"timestamp":"2026-06-26T03:52:49.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>only context</environment_context>"}]}}
```

- [ ] **Step 2: Write the failing test**

`internal/store/codex_test.go`:
```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run TestCodex`
Expected: FAIL (NewCodexProvider undefined).

- [ ] **Step 4: Write codex.go (scan + metadata; other methods stubbed)**

`internal/store/codex.go`:
```go
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// codexProvider serves OpenAI Codex sessions under sessionsDir
// (~/.codex/sessions), stored as rollout-<ts>-<uuid>.jsonl files nested
// by date.
type codexProvider struct{ sessionsDir string }

// NewCodexProvider builds the Codex provider for sessionsDir.
func NewCodexProvider(sessionsDir string) Provider {
	return codexProvider{sessionsDir: sessionsDir}
}

func (codexProvider) Agent() Agent   { return AgentCodex }
func (codexProvider) Binary() string { return "codex" }

func (p codexProvider) Available() bool {
	info, err := os.Stat(p.sessionsDir)
	return err == nil && info.IsDir()
}

// Scan walks sessionsDir for rollout-*.jsonl files (skipping any .trash),
// building entries from the filename + mtime only (no file contents).
func (p codexProvider) Scan() ([]Session, error) {
	var sessions []Session
	err := filepath.WalkDir(p.sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != p.sessionsDir {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		s := Session{
			ID:    codexID(name),
			Path:  path,
			Agent: AgentCodex,
		}
		if info, err := d.Info(); err == nil {
			s.LastActivity = info.ModTime()
		}
		sessions = append(sessions, s)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

// codexID extracts the trailing UUID from a rollout filename like
// rollout-2026-06-26T03-52-34-<uuid>.jsonl. Falls back to the whole stem.
func codexID(filename string) string {
	stem := strings.TrimSuffix(filename, ".jsonl")
	stem = strings.TrimPrefix(stem, "rollout-")
	// The UUID is the last 5 dash-separated groups (8-4-4-4-12).
	parts := strings.Split(stem, "-")
	if len(parts) >= 5 {
		return strings.Join(parts[len(parts)-5:], "-")
	}
	return stem
}

type codexRecord struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

type codexMeta struct {
	CWD       string `json:"cwd"`
	Timestamp string `json:"timestamp"`
}

type codexPayload struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content []codexContent  `json:"content"`
	Name    string          `json:"name"`      // function_call
	Arguments string        `json:"arguments"` // function_call (JSON string)
}

type codexContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (p codexProvider) ParseMetadata(path string) (Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	var m Meta
	sc := newScanner(f)
	for sc.Scan() {
		var rec codexRecord
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		switch rec.Type {
		case "session_meta":
			var sm codexMeta
			if json.Unmarshal(rec.Payload, &sm) == nil {
				if sm.CWD != "" {
					m.CWD = sm.CWD
				}
				if t, err := time.Parse(time.RFC3339, sm.Timestamp); err == nil {
					m.LastActivity = t
				}
			}
		case "response_item":
			var pl codexPayload
			if json.Unmarshal(rec.Payload, &pl) != nil || pl.Type != "message" {
				continue
			}
			text := codexText(pl)
			switch pl.Role {
			case "assistant":
				if text != "" {
					m.TotalMessages++
				}
			case "user":
				if p := codexRealPrompt(text); p != "" {
					m.TotalMessages++
					m.UserMessages++
					if m.FirstPrompt == "" {
						m.FirstPrompt = p
					}
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

// codexText returns the first non-empty text block of a message payload.
func codexText(pl codexPayload) string {
	for _, c := range pl.Content {
		if strings.TrimSpace(c.Text) != "" {
			return c.Text
		}
	}
	return ""
}

// codexRealPrompt returns text iff it is a human prompt: not harness-injected
// context (which is wrapped in <...>).
func codexRealPrompt(text string) string {
	t := strings.TrimSpace(text)
	if t == "" || strings.HasPrefix(t, "<") {
		return ""
	}
	return t
}

// --- stubs completed in Task 4 ---
func (codexProvider) ParseTranscript(path string) (Transcript, error) { return Transcript{}, nil }
func (codexProvider) Trash(s Session) (string, error)                 { return "", nil }
func (codexProvider) ResumeCommand(s Session) (string, []string)      { return "codex", []string{"resume", s.ID} }
func (codexProvider) NewCommand() (string, []string)                  { return "codex", nil }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run TestCodex -v`
Expected: PASS (3 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/store/codex.go internal/store/codex_test.go internal/store/codex_testdata/
git commit -m "feat(store): codexProvider scan and metadata

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: codexProvider — transcript, trash, commands

**Files:**
- Modify: `internal/store/codex.go` (replace the three stubs; keep the `ResumeCommand`/`NewCommand` stubs as the real impls — they already are)
- Modify: `internal/store/codex_test.go` (add tests)

**Interfaces:**
- Consumes: `Transcript`, `Message`, `KindUser`/`KindAssistant`/`KindTool`, `Truncate`, `codexRecord`/`codexPayload`/`codexText` from Task 3.
- Produces: working `ParseTranscript` and `Trash` (to `<sessionsDir>/.trash/<basename>`).

- [ ] **Step 1: Write the failing test**

Append to `internal/store/codex_test.go`:
```go
func TestCodexParseTranscript(t *testing.T) {
	tr, err := NewCodexProvider(t.TempDir()).ParseTranscript("codex_testdata/rollout-basic.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	want := []Message{
		{KindUser, "Identify the bug in this project"},
		{KindAssistant, "I'll inspect the repo first."},
		{KindTool, "exec_command: git status --short"},
	}
	if len(tr.Messages) != len(want) {
		t.Fatalf("got %d messages, want %d: %+v", len(tr.Messages), len(want), tr.Messages)
	}
	for i := range want {
		if tr.Messages[i] != want[i] {
			t.Errorf("message %d = %+v, want %+v", i, tr.Messages[i], want[i])
		}
	}
}

func TestCodexTrash(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "2026", "06", "26", "rollout-x-aaaa.jsonl")
	writeFile(t, src, "{}\n")
	p := NewCodexProvider(dir)
	dest, err := p.Trash(Session{Path: src, Agent: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	if dest != filepath.Join(dir, ".trash", "rollout-x-aaaa.jsonl") {
		t.Errorf("dest = %q", dest)
	}
	if _, err := osStat(src); !os.IsNotExist(err) {
		t.Error("source still exists after trash")
	}
	if _, err := osStat(dest); err != nil {
		t.Errorf("trashed file missing: %v", err)
	}
}
```

Add near the top of `codex_test.go` (import `os` and alias its Stat to keep the test terse):
```go
import "os"

var osStat = os.Stat
```
(If `os` is already imported in the file after Task 3 edits, just add `var osStat = os.Stat` once and drop the duplicate import.)

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run 'TestCodexParseTranscript|TestCodexTrash'`
Expected: FAIL (ParseTranscript returns empty; Trash returns "").

- [ ] **Step 3: Replace the ParseTranscript and Trash stubs in codex.go**

Replace the two stub lines
```go
func (codexProvider) ParseTranscript(path string) (Transcript, error) { return Transcript{}, nil }
func (codexProvider) Trash(s Session) (string, error)                 { return "", nil }
```
with:
```go
// ParseTranscript extracts the human-readable conversation: real user
// prompts, assistant text, and tool (function_call) one-liners. Developer
// messages, reasoning, and <...> context are excluded.
func (codexProvider) ParseTranscript(path string) (Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return Transcript{}, err
	}
	defer f.Close()
	var tr Transcript
	sc := newScanner(f)
	for sc.Scan() {
		var rec codexRecord
		if json.Unmarshal(sc.Bytes(), &rec) != nil || rec.Type != "response_item" {
			continue
		}
		var pl codexPayload
		if json.Unmarshal(rec.Payload, &pl) != nil {
			continue
		}
		switch pl.Type {
		case "message":
			text := strings.TrimSpace(codexText(pl))
			switch pl.Role {
			case "user":
				if p := codexRealPrompt(text); p != "" {
					tr.Messages = append(tr.Messages, Message{KindUser, p})
				}
			case "assistant":
				if text != "" {
					tr.Messages = append(tr.Messages, Message{KindAssistant, text})
				}
			}
		case "function_call":
			tr.Messages = append(tr.Messages, Message{KindTool, codexTool(pl)})
		}
	}
	return tr, sc.Err()
}

// codexTool renders a function_call as "name: <first arg or cmd>".
func codexTool(pl codexPayload) string {
	var args map[string]any
	json.Unmarshal([]byte(pl.Arguments), &args)
	for _, k := range []string{"cmd", "command", "description", "path", "query", "input"} {
		if v, ok := args[k].(string); ok && v != "" {
			return pl.Name + ": " + Truncate(v, 80)
		}
	}
	return pl.Name
}

// Trash moves a rollout file into <sessionsDir>/.trash/ (never rm).
func (p codexProvider) Trash(s Session) (string, error) {
	dest := filepath.Join(p.sessionsDir, ".trash", filepath.Base(s.Path))
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

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run TestCodex -v && go vet ./...`
Expected: PASS (all codex tests), vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/store/codex.go internal/store/codex_test.go
git commit -m "feat(store): codex transcript and trash

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Provider-dispatched Enrich, transcript cache, and search index

Store parsing must dispatch by agent everywhere: metadata (Enrich), the
preview cache (`TranscriptCache`), and the full-text index (`SearchIndex`).
Enrich gains a `providers` argument; the cache and index instead take an
injected parse closure so they stay provider-agnostic and the caller
(model.go) supplies the per-agent dispatch.

**Files:**
- Modify: `internal/store/scan.go` (Enrich signature + dispatch)
- Modify: `internal/store/transcript.go` (`TranscriptCache.Get` takes a parse closure)
- Modify: `internal/store/searchindex.go` (`EnsureSession`/`EnsureAll` take parse closures)
- Test: `internal/store/scan_test.go`, `internal/store/transcript_test.go`, `internal/store/searchindex_test.go`

**Interfaces:**
- Consumes: `Provider`, `ProviderFor` from Task 2; `Agent` from Task 1.
- Produces:
  - `func Enrich(sessions []Session, providers []Provider, workers int, results chan<- EnrichResult)` — parses each session via its agent's provider.
  - `func (c *TranscriptCache) Get(path string, parse func() (Transcript, error)) (Transcript, error)`
  - `func (ix SearchIndex) EnsureSession(path string, parse func() (Transcript, error)) error`
  - `func (ix SearchIndex) EnsureAll(sessions []Session, parse func(Session) (Transcript, error), workers int, results chan<- IndexProgress)`

- [ ] **Step 1: Update the existing Enrich test and add a dispatch test**

In `internal/store/scan_test.go`, the existing `TestEnrich` and `TestEnrichConcurrentWithSliceMutation` call `Enrich(sessions, N, results)`. Update those call sites to pass a Claude provider:
```go
prov := []Provider{NewClaudeProvider(dir)}
Enrich(sessions, prov, 2, results)
```
(Use the `dir` each test already has; for `TestEnrichConcurrentWithSliceMutation` pass `[]Provider{NewClaudeProvider(dir)}` with its temp dir.)

Add a new test:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ -run TestEnrich`
Expected: FAIL (Enrich signature mismatch / dispatch test fails to compile).

- [ ] **Step 3: Rewrite Enrich to dispatch via provider**

In `internal/store/scan.go`, replace the whole `Enrich` function with:
```go
// Enrich parses metadata for every session concurrently, dispatching to the
// provider that handles each session's Agent, and sends one result per
// session (closing results when done). Sessions whose agent has no provider
// yield an error result.
func Enrich(sessions []Session, providers []Provider, workers int, results chan<- EnrichResult) {
	if workers < 1 {
		workers = 1
	}
	type job struct {
		path  string
		slug  string
		agent Agent
	}
	snap := make([]job, len(sessions))
	for i, s := range sessions {
		snap[i] = job{s.Path, s.Slug, s.Agent}
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				p := ProviderFor(providers, snap[i].agent)
				if p == nil {
					results <- EnrichResult{Index: i, Err: fmt.Errorf("no provider for agent %q", snap[i].agent)}
					continue
				}
				m, err := p.ParseMetadata(snap[i].path)
				if err == nil && m.CWD == "" && snap[i].agent == AgentClaude {
					m.CWD = ResolveSlug("/", snap[i].slug)
				}
				results <- EnrichResult{Index: i, Meta: m, Err: err}
			}
		}()
	}
	go func() {
		for i := range snap {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
}
```
Add `"fmt"` to scan.go's imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/ && go build ./...`
Expected: PASS. (The `./...` build will fail in `internal/ui` because model.go still calls the old `Enrich` — that's fixed in Task 8. Run just the store package here: `go test ./internal/store/`.)

- [ ] **Step 5: Commit**

```bash
git add internal/store/scan.go internal/store/scan_test.go
git commit -m "feat(store): Enrich dispatches metadata parse by provider

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [ ] **Step 6: Inject the parse closure into TranscriptCache and SearchIndex**

`internal/store/transcript.go` — change `Get` to take a parse closure and drop the direct `ParseTranscript(path)` call:
```go
func (c *TranscriptCache) Get(path string, parse func() (Transcript, error)) (Transcript, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, err := os.Stat(path)
	if err != nil {
		return Transcript{}, err
	}
	key := fmt.Sprintf("%s|%d", path, st.ModTime().UnixNano())
	if t, ok := c.entries[key]; ok {
		c.touch(key)
		return t, nil
	}
	t, err := parse()
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
```

`internal/store/searchindex.go` — `EnsureSession` takes a parse closure instead of calling `ParseTranscript(path)`; `EnsureAll` takes a per-session dispatcher. Change the `EnsureSession` signature and its transcript line:
```go
func (ix SearchIndex) EnsureSession(path string, parse func() (Transcript, error)) error {
	// ... unchanged validity-key check ...
	tr, err := parse()   // was: ParseTranscript(path)
	// ... unchanged extraction/write ...
}
```
and `EnsureAll`:
```go
func (ix SearchIndex) EnsureAll(sessions []Session, parse func(Session) (Transcript, error), workers int, results chan<- IndexProgress) {
	// ... unchanged worker pool, but each job calls: ...
	//   s := sessions[i]
	//   done <- ix.EnsureSession(s.Path, func() (Transcript, error) { return parse(s) })
	// Snapshot sessions up front (as Enrich does) so workers don't read the live slice.
}
```
(Snapshot the needed fields — `Path` and the `Session` value for the closure — into a local slice before spawning workers, mirroring `Enrich`.)

- [ ] **Step 7: Update the store tests for the new signatures**

In `internal/store/transcript_test.go` and `internal/store/searchindex_test.go`, update every `Get(path)` / `EnsureSession(path)` / `EnsureAll(sessions, N, results)` call to pass the closure form, e.g.:
```go
c.Get(p, func() (Transcript, error) { return ParseTranscript(p) })
ix.EnsureSession(path, func() (Transcript, error) { return ParseTranscript(path) })
ix.EnsureAll(sessions, func(s Session) (Transcript, error) { return ParseTranscript(s.Path) }, 4, results)
```

- [ ] **Step 8: Run store tests + commit**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/store/`
Expected: PASS.
```bash
git add internal/store/transcript.go internal/store/searchindex.go internal/store/transcript_test.go internal/store/searchindex_test.go
git commit -m "feat(store): inject transcript parse into cache and index

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: UI — Codex accent and per-agent coloring

**Files:**
- Modify: `internal/ui/styles.go`
- Modify: `internal/ui/listpane.go` (render title/tag by agent)
- Test: `internal/ui/listpane_test.go`

**Interfaces:**
- Consumes: `store.Session.Agent`, `store.AgentCodex`.
- Produces: styles `CodexTitle`, `CodexTitleSel`, `CodexTag`, `ClaudeTag`; `listPane.View()` colors a session's title + appends a `claude`/`codex` tag by agent.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/listpane_test.go`:
```go
func TestListShowsAgentTag(t *testing.T) {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(80, 40)
	l.SetSessions([]store.Session{
		{ID: "c1", CWD: "/x/p", Title: "claude one", Agent: store.AgentClaude, UserMessages: 1, Enriched: true, LastActivity: time.Now()},
		{ID: "x1", CWD: "/x/p", Title: "codex one", Agent: store.AgentCodex, UserMessages: 1, Enriched: true, LastActivity: time.Now().Add(-time.Minute)},
	})
	v := l.View()
	if !strings.Contains(v, "claude") {
		t.Errorf("view missing claude tag:\n%s", v)
	}
	if !strings.Contains(v, "codex") {
		t.Errorf("view missing codex tag:\n%s", v)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestListShowsAgentTag`
Expected: FAIL (no `codex`/`claude` tag in the meta line yet).

- [ ] **Step 3: Add Codex styles**

In `internal/ui/styles.go`, inside `defaultStyles()` where the palette colors are declared, add:
```go
	codex := lipgloss.AdaptiveColor{Light: "#0A7C66", Dark: "#10A37F"}
```
Add these fields to the `styles` struct (after `ListMetaSel`):
```go
	ClaudeTitle    lipgloss.Style
	ClaudeTitleSel lipgloss.Style
	CodexTitle     lipgloss.Style
	CodexTitleSel  lipgloss.Style
	ClaudeTag      lipgloss.Style
	CodexTag       lipgloss.Style
```
And in the returned struct literal, set them:
```go
	ClaudeTitle:    lipgloss.NewStyle().Foreground(text),
	ClaudeTitleSel: lipgloss.NewStyle().Bold(true).Foreground(accent),
	CodexTitle:     lipgloss.NewStyle().Foreground(text),
	CodexTitleSel:  lipgloss.NewStyle().Bold(true).Foreground(codex),
	ClaudeTag:      lipgloss.NewStyle().Foreground(accent),
	CodexTag:       lipgloss.NewStyle().Foreground(codex),
```
(Claude titles keep the existing `text`/`accent` behavior; only the selected Codex title switches to teal-green, and the tag is always agent-colored.)

- [ ] **Step 4: Render the tag and per-agent title in listpane.go**

In `internal/ui/listpane.go` `View()`, find the session-row rendering block that builds `title`, `meta`, and applies `titleStyle`/`metaStyle`. Replace the style-selection + meta-construction so the agent drives it. Locate:
```go
		meta := s.Project() + " · " + humanTime(s.LastActivity, time.Now())
```
Immediately after the existing `meta` is fully built (after the branch/unreadable/hits additions, right before the `prefix`/`titleStyle` selection), append the agent tag:
```go
		tag := s.Agent.Label()
		tagStyle := l.styles.ClaudeTag
		if s.Agent == store.AgentCodex {
			tagStyle = l.styles.CodexTag
		}
```
Then, where the selected style is chosen (`if i == l.cursor { prefix = "▶ "; titleStyle, metaStyle = l.styles.ListTitleSel, l.styles.ListMetaSel }`), make the selected title agent-aware:
```go
		if i == l.cursor {
			prefix = "▶ "
			titleStyle, metaStyle = l.styles.ListTitleSel, l.styles.ListMetaSel
			if s.Agent == store.AgentCodex {
				titleStyle = l.styles.CodexTitleSel
			}
		}
```
Finally, change the meta line append so the tag is included, colored. Where the meta line is currently appended (e.g. `metaStyle.Render(store.Truncate("  "+meta, l.width))`), replace with a version that renders the tag separately:
```go
		metaText := store.Truncate("  "+meta, l.width-len(tag)-3)
		lines = append(lines,
			titleStyle.Render(store.Truncate(prefix+title, l.width)),
			metaStyle.Render(metaText)+" "+tagStyle.Render(tag),
			"")
```
(Adjust to match the exact existing three-line append; keep the blank separator line.)

- [ ] **Step 5: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestListShowsAgentTag -v`
Expected: PASS. Also run the full ui suite to catch snapshot drift: `go test ./internal/ui/`.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/styles.go internal/ui/listpane.go internal/ui/listpane_test.go
git commit -m "feat(ui): per-agent title color and claude/codex tag

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: UI — agent-grouping toggle and subheader rows

**Files:**
- Modify: `internal/ui/listpane.go` (row model, refresh, layout, MoveCursor, View, `ToggleAgentGroup`)
- Test: `internal/ui/listpane_test.go`

**Interfaces:**
- Consumes: `store.Agent`, per-agent styles from Task 6.
- Produces: `listPane.groupByAgent bool`; `func (l *listPane) ToggleAgentGroup()`; a `subheader` row kind rendered as `─ Claude ─`/`─ Codex ─`; cursor skips subheaders.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/listpane_test.go`:
```go
func agentMixSessions() []store.Session {
	now := time.Now()
	return []store.Session{
		{ID: "c1", CWD: "/x/mix", Title: "claude a", Agent: store.AgentClaude, UserMessages: 1, Enriched: true, LastActivity: now},
		{ID: "x1", CWD: "/x/mix", Title: "codex a", Agent: store.AgentCodex, UserMessages: 1, Enriched: true, LastActivity: now.Add(-time.Minute)},
		{ID: "c2", CWD: "/x/solo", Title: "claude solo", Agent: store.AgentClaude, UserMessages: 1, Enriched: true, LastActivity: now.Add(-2 * time.Minute)},
	}
}

func TestAgentGroupingSubheaders(t *testing.T) {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(80, 60)
	l.SetSessions(agentMixSessions())
	if strings.Contains(l.View(), "Claude ─") {
		t.Error("no subheaders before toggle")
	}
	l.ToggleAgentGroup()
	v := l.View()
	if !strings.Contains(v, "─ Claude ─") || !strings.Contains(v, "─ Codex ─") {
		t.Errorf("mixed project should show both subheaders:\n%s", v)
	}
	// single-agent project 'solo' must NOT get a subheader
	soloIdx := strings.Index(v, "claude solo")
	seg := v[strings.Index(v, "solo ("):soloIdx]
	if strings.Contains(seg, "─ Claude ─") {
		t.Errorf("single-agent project should not show a subheader:\n%s", v)
	}
}

func TestAgentSubheaderNotCursorable(t *testing.T) {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(80, 60)
	l.SetSessions(agentMixSessions())
	l.ToggleAgentGroup()
	// walk down through all rows; Selected must never be a subheader (ok true only on sessions)
	for n := 0; n < 12; n++ {
		if _, _, ok := l.Selected(); ok {
			// fine — a session
		}
		l.MoveCursor(1)
	}
	// no panic + cursor still resolves
	if l.Len() != 3 {
		t.Errorf("Len = %d, want 3", l.Len())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'TestAgentGrouping|TestAgentSubheader'`
Expected: FAIL (ToggleAgentGroup undefined).

- [ ] **Step 3: Extend the row model**

In `internal/ui/listpane.go`, change the `row` struct to carry a kind and an optional subheader label:
```go
type row struct {
	header    bool
	subheader bool   // agent subsection label (inert)
	label     string // subheader text, e.g. "─ Claude ─"
	project   string
	session   int // index into listPane.sessions; valid when !header && !subheader
}
```
Add the field to the struct:
```go
	groupByProject bool
	groupByAgent   bool
```
Add the toggle (near `ToggleGroup`):
```go
// ToggleAgentGroup turns per-project agent subgrouping on/off.
func (l *listPane) ToggleAgentGroup() {
	sel, ok := l.selectedSession()
	l.groupByAgent = !l.groupByAgent
	l.refresh()
	if ok {
		l.selectSession(sel)
	} else {
		l.cursorToFirstSession()
	}
}
```

In `refresh()`, the grouped branch iterates `for _, p := range order` and, after appending the project header (and a `if l.folded[p] { continue }` skip), emits sessions with `for _, si := range buckets[p] { l.rows = append(l.rows, row{project: p, session: si}) }`. Replace **that inner session-emission loop** with agent subsections when `l.groupByAgent` is on and the project has more than one agent:
```go
		projSessions := buckets[p] // the []int of session indexes for this project
		if l.groupByAgent && projectHasBothAgents(l.sessions, projSessions) {
			for _, ag := range []store.Agent{store.AgentClaude, store.AgentCodex} {
				var seg []int
				for _, si := range projSessions {
					if l.sessions[si].Agent == ag {
						seg = append(seg, si)
					}
				}
				if len(seg) == 0 {
					continue
				}
				l.rows = append(l.rows, row{subheader: true, project: p, label: "─ " + agentTitle(ag) + " ─"})
				for _, si := range seg {
					l.rows = append(l.rows, row{project: p, session: si})
				}
			}
		} else {
			for _, si := range projSessions {
				l.rows = append(l.rows, row{project: p, session: si})
			}
		}
```
Add helpers at the bottom of listpane.go:
```go
func projectHasBothAgents(sessions []store.Session, idx []int) bool {
	seen := map[store.Agent]bool{}
	for _, si := range idx {
		seen[sessions[si].Agent] = true
	}
	return len(seen) > 1
}

func agentTitle(a store.Agent) string {
	if a == store.AgentCodex {
		return "Codex"
	}
	return "Claude"
}
```
(Adapt `grouped[p]`/`projSessions` to the actual variable the existing refresh uses when clustering by project. The existing code already builds a per-project ordered list of session indexes; wrap that emission with the above.)

- [ ] **Step 4: Make subheaders inert in navigation, layout, and rendering**

In `MoveCursor`, after computing the new cursor, skip subheader rows in the direction of travel. Replace the clamp body with:
```go
func (l *listPane) MoveCursor(delta int) {
	if len(l.rows) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	n := delta
	if n < 0 {
		n = -n
	}
	for ; n > 0; n-- {
		i := l.cursor + step
		for i >= 0 && i < len(l.rows) && l.rows[i].subheader {
			i += step // hop over inert subheaders
		}
		if i < 0 || i >= len(l.rows) {
			break
		}
		l.cursor = i
	}
	l.ensureVisible()
}
```
In `layout()` (line-height computation) treat a subheader as a 1-line row, same as a header. Find the switch on row kind and add:
```go
		if r.header || r.subheader {
			pos++
		} else {
			pos += sessionLines
		}
```
In `View()`, render subheaders. In the row loop, before the header/session branches add:
```go
		if r.subheader {
			lines = append(lines, l.styles.GroupCount.Render(store.Truncate("  "+r.label, l.width)))
			continue
		}
```
Ensure `Selected()`, `OnHeader()`, `selectedSession()` treat subheaders like headers (not a session): they already guard on `l.rows[l.cursor].header` — extend those guards to `|| l.rows[l.cursor].subheader`. Grep for `.header` in those three methods and change each `l.rows[l.cursor].header` to `(l.rows[l.cursor].header || l.rows[l.cursor].subheader)`. Also `cursorToFirstSession`/`selectSession` iterate for `!r.header` — change to `!r.header && !r.subheader`.

- [ ] **Step 5: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'TestAgentGrouping|TestAgentSubheader' -v && go test ./internal/ui/`
Expected: PASS, full ui suite green.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/listpane.go internal/ui/listpane_test.go
git commit -m "feat(ui): per-project agent subgrouping with inert subheaders

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 8: UI — provider-aware scan and enrich wiring

**Files:**
- Modify: `internal/ui/model.go` (Model gains `providers`; scan/enrich use ScanAll/provider Enrich)
- Modify: `internal/ui/model_test.go` (newTestModel; helpers)
- Modify: `cmd/sm/main.go` will follow in Task 10; here `New` gains a codex dir param with a default.

**Interfaces:**
- Consumes: `store.ScanAll`, `store.NewClaudeProvider`, `store.NewCodexProvider`, `store.Enrich(sessions, providers, workers, ch)`.
- Produces: `Model.providers []store.Provider`; `func New(projectsDir, codexDir string) Model`.

- [ ] **Step 1: Update newTestModel and add a build-check test**

In `internal/ui/model_test.go`, `newTestModel` calls `New("/nonexistent-projects-dir")`. Change to `New("/nonexistent-projects-dir", "/nonexistent-codex-dir")`. In any other `New(` call in tests, add the second arg.

Add:
```go
func TestNewBuildsProviders(t *testing.T) {
	m := New("/nope/claude", "/nope/codex")
	if len(m.providers) == 0 {
		t.Error("expected at least the claude provider")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestNewBuildsProviders`
Expected: FAIL (New takes one arg; m.providers undefined).

- [ ] **Step 3: Wire providers into the Model**

In `internal/ui/model.go`:
- Add to the `Model` struct: `providers []store.Provider`.
- Change `func New(projectsDir string) Model` to `func New(projectsDir, codexDir string) Model`, and inside build the provider set:
```go
	provs := []store.Provider{store.NewClaudeProvider(projectsDir)}
	if cp := store.NewCodexProvider(codexDir); cp.Available() {
		provs = append(provs, cp)
	}
	ret.providers = provs
```
(Keep `projectsDir` on the Model if other code uses it; it stays for the dir picker's known dirs.)
- In `scanCmd`, replace `store.Scan(dir)` with `store.ScanAll(m.providers)`:
```go
func (m Model) scanCmd() tea.Cmd {
	provs := m.providers
	return func() tea.Msg {
		sessions, err := store.ScanAll(provs)
		return scanDoneMsg{sessions: sessions, err: err}
	}
}
```
- In the `scanDoneMsg` handler, replace `store.Enrich(msg.sessions, 8, ch)` with `store.Enrich(msg.sessions, m.providers, 8, ch)`.
- In `loadTranscriptCmd`, the `cache.Get(path)` call now needs a parse closure that dispatches by the selected session's agent. Where it currently reads `t, err := cache.Get(path)`, capture the session's agent/path and pass a closure:
```go
	cache, path, id, agent := m.cache, s.Path, s.ID, s.Agent
	provs := m.providers
	return func() tea.Msg {
		t, err := cache.Get(path, func() (store.Transcript, error) {
			p := store.ProviderFor(provs, agent)
			if p == nil {
				return store.Transcript{}, fmt.Errorf("no provider for %s", agent.Label())
			}
			return p.ParseTranscript(path)
		})
		t.SessionID = id
		return transcriptMsg{id: id, t: t, err: err}
	}
```
- Where the search index is built, change the `EnsureAll(sessions, 8, ch)` call to pass a dispatcher: `index.EnsureAll(sessions, func(s store.Session) (store.Transcript, error) { p := store.ProviderFor(m.providers, s.Agent); if p == nil { return store.Transcript{}, fmt.Errorf("no provider for %s", s.Agent.Label()) }; return p.ParseTranscript(s.Path) }, 8, ch)` (snapshot `m.providers` into a local like the transcript closure does; add `"fmt"` to model.go imports if not already present).

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestNewBuildsProviders && go build ./cmd/... 2>&1 | head`
Expected: PASS. (cmd build fails until Task 10 passes two args to `New` — that's expected; the ui package test passes.)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): provider-aware scan and enrich

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 9: UI — per-agent resume, new-session agent picker, delete; the `a` key

**Files:**
- Modify: `internal/ui/model.go`
- Modify: `internal/ui/model_test.go` / `internal/ui/actions_test.go`

**Interfaces:**
- Consumes: provider `ResumeCommand`/`NewCommand`/`Trash`/`Binary`; `store.ProviderFor`.
- Produces: `runCmd func(name, dir string, args ...string) tea.Cmd` (replaces `runClaude`); `dialogPickAgent` dialog; `a` key toggles agent grouping; delete/resume dispatch by agent.

- [ ] **Step 1: Write the failing tests**

In `internal/ui/actions_test.go`, the existing `resumeRecorder` records `(dir, args)`. Extend it to also record the binary name. Change its `cmd` method signature and the field:
```go
type resumeRecorder struct {
	name string
	dir  string
	args []string
}

func (r *resumeRecorder) cmd(name, dir string, args ...string) tea.Cmd {
	r.name, r.dir, r.args = name, dir, args
	return func() tea.Msg { return nil }
}
```
Update existing resume tests that assert `rec.args == [--resume s1]` to also allow `rec.name == "claude"`. Add:
```go
func TestResumeCodexUsesCodexCommand(t *testing.T) {
	m := newTestModel()
	dir := t.TempDir()
	// make the selected session a codex session in an existing dir
	m.list.sessions[0].Agent = store.AgentCodex
	m.list.sessions[0].CWD = dir
	// ensure a codex provider is registered
	m.providers = append(m.providers, store.NewCodexProvider(t.TempDir()))
	rec := &resumeRecorder{}
	m.runCmd = rec.cmd
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if rec.name != "codex" || len(rec.args) != 2 || rec.args[0] != "resume" {
		t.Errorf("codex resume = %s %v", rec.name, rec.args)
	}
	if rec.dir != dir {
		t.Errorf("dir = %q, want %q", rec.dir, dir)
	}
}

func TestNewSessionOpensAgentPicker(t *testing.T) {
	m := newTestModel()
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m2, _ := m.Update(key("n"))
	m = m2.(Model)
	if m.dialog != dialogPickAgent {
		t.Fatalf("dialog = %v, want dialogPickAgent", m.dialog)
	}
	// pick Claude (key "1") launches claude in the selected project dir
	rec := &resumeRecorder{}
	m.runCmd = rec.cmd
	m2, _ = m.Update(key("1"))
	m = m2.(Model)
	if rec.name != "claude" || rec.dir != dir || len(rec.args) != 0 {
		t.Errorf("new claude = %s %q %v", rec.name, rec.dir, rec.args)
	}
}

func TestAgentGroupKeyToggles(t *testing.T) {
	m := newTestModel()
	before := m.list.groupByAgent
	m2, _ := m.Update(key("a"))
	m = m2.(Model)
	if m.list.groupByAgent == before {
		t.Error("`a` should toggle agent grouping")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'TestResumeCodex|TestNewSessionOpensAgentPicker|TestAgentGroupKeyToggles'`
Expected: FAIL (runCmd/dialogPickAgent undefined).

- [ ] **Step 3: Rename the runner and add agent dispatch**

In `internal/ui/model.go`:
- Rename the injected field `runClaude func(dir string, args ...string) tea.Cmd` to `runCmd func(name, dir string, args ...string) tea.Cmd`, and update `New` to set `runCmd: execCmd`.
- Replace `execClaude` with:
```go
func execCmd(name, dir string, args ...string) tea.Cmd {
	c := exec.Command(name, args...)
	c.Dir = dir
	return tea.ExecProcess(c, func(err error) tea.Msg { return agentExitMsg{err} })
}
```
- Rename `claudeExitMsg` → `agentExitMsg`; update its handler case and keep the launch-error text generic (`"could not launch: " + msg.err.Error()`).
- Remove the startup Claude-only check: delete `claudeMissingMsg`, `checkClaudeCmd`, and its handler case, and change `Init()` to `return m.scanCmd()`. Missing binaries are now caught per-agent at resume/new time via the `exec.LookPath(p.Binary())` checks below (a Codex-only user must not see a "claude not found" warning at startup).
- Add the dialog kind: in the `dialogKind` const block add `dialogPickAgent`.
- Rewrite `startResume` (the `enter` on a session path). Find the current:
```go
	return m, m.runClaude(s.CWD, "--resume", s.ID)
```
Replace with provider dispatch:
```go
	p := store.ProviderFor(m.providers, s.Agent)
	if p == nil {
		m.dialog = dialogError
		m.errText = "no handler for agent " + s.Agent.Label()
		return m, nil
	}
	if _, err := exec.LookPath(p.Binary()); err != nil {
		m.dialog = dialogError
		m.errText = p.Binary() + " not found on PATH"
		return m, nil
	}
	name, args := p.ResumeCommand(s)
	return m, m.runCmd(name, s.CWD, args...)
```
(Apply the same `runClaude(...)` → provider-dispatch change at the pending-resume site in the dir-picker path around the old `runClaude(dir, "--resume", pending.ID)` — resolve the pending session's provider the same way.)

- [ ] **Step 4: New-session agent picker + delete dispatch + `a` key**

- Replace `openNewSession` so it defaults to the selected project's dir and opens the agent picker:
```go
func (m Model) openNewSession() (tea.Model, tea.Cmd) {
	if s, _, ok := m.list.Selected(); ok && s.CWD != "" {
		if st, err := os.Stat(s.CWD); err == nil && st.IsDir() {
			m.pendingNewDir = s.CWD
			m.dialog = dialogPickAgent
			return m, nil
		}
	}
	m.pendingResume = nil
	m.openDirPicker() // no selection: fall back to dir picker, then agent pick
	return m, nil
}
```
Add `pendingNewDir string` to the Model. In the dir-picker confirm path (where a new-session dir is chosen with no pending resume), instead of launching immediately, set `m.pendingNewDir = dir; m.dialog = dialogPickAgent`.
- Handle the agent-pick dialog in `handleDialogKey`:
```go
	case dialogPickAgent:
		var agent store.Agent
		switch msg.String() {
		case "1", "c":
			agent = store.AgentClaude
		case "2", "x":
			agent = store.AgentCodex
		case "esc", "n":
			m.dialog = dialogNone
			m.pendingNewDir = ""
			return m, nil
		default:
			return m, nil
		}
		p := store.ProviderFor(m.providers, agent)
		dir := m.pendingNewDir
		m.dialog = dialogNone
		m.pendingNewDir = ""
		if p == nil {
			m.dialog = dialogError
			m.errText = agent.Label() + " is not available"
			return m, nil
		}
		if _, err := exec.LookPath(p.Binary()); err != nil {
			m.dialog = dialogError
			m.errText = p.Binary() + " not found on PATH"
			return m, nil
		}
		name, args := p.NewCommand()
		return m, m.runCmd(name, dir, args...)
```
- Render the agent-pick dialog in `dialogView()`:
```go
	case dialogPickAgent:
		return m.st.DialogBox.Render(
			"New session in " + m.pendingNewDir + "\n\n" +
				"  [1] Claude    [2] Codex\n\n" +
				m.st.Help.Render("1/2 choose · esc cancel"))
```
- Delete: in the `dialogDelete` confirm branch, replace `m.trashFn(m.projectsDir, s)` with provider dispatch. Change the injected `trashFn` to `func(store.Session) (string, error)` and default it to a dispatcher:
```go
	trashFn: func(s store.Session) (string, error) {
		p := store.ProviderFor(provs, s.Agent) // provs captured in New
		if p == nil {
			return "", fmt.Errorf("no provider for %s", s.Agent.Label())
		}
		return p.Trash(s)
	},
```
and the call site becomes `m.trashFn(s)`. Update `TestDeleteFlow`'s injected `trashFn` to the new one-arg signature.
- Add the `a` key in the `focusList` key switch (near the `g` case):
```go
		case "a":
			m.list.ToggleAgentGroup()
			return m, m.loadTranscriptCmd()
```
- Add `a agent` to the help line text (`helpLine()`), after `g group`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ && go vet ./internal/ui/`
Expected: PASS (all ui tests including the new ones), vet clean.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go internal/ui/actions_test.go
git commit -m "feat(ui): per-agent resume/new/delete, agent-pick dialog, a key

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 10: main wiring, README, e2e, install

**Files:**
- Modify: `cmd/sm/main.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: `ui.New(projectsDir, codexDir string)`.

- [ ] **Step 1: Wire main.go**

In `cmd/sm/main.go`, add a `--codex-dir` flag and pass both dirs:
```go
	projectsDir := flag.String("projects-dir", filepath.Join(home, ".claude", "projects"), "Claude Code projects directory")
	codexDir := flag.String("codex-dir", filepath.Join(home, ".codex", "sessions"), "Codex sessions directory")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("sm", version)
		return
	}
	p := tea.NewProgram(ui.New(*projectsDir, *codexDir), tea.WithAltScreen())
```

- [ ] **Step 2: Full suite, vet, cross-compile, build**

Run:
```bash
export PATH=$HOME/.local/go/bin:$PATH
gofmt -l internal cmd
go vet ./... && go test -race ./...
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -o /dev/null ./cmd/sm && echo "OK $t"; done
make build && ./sm --version
```
Expected: gofmt clean, vet clean, all tests pass (incl. `-race`), 4 targets build, prints `sm 0.2.0`.

- [ ] **Step 3: Update README**

In `README.md`, add a short "Agents" note under Features and in Usage: mention Codex sessions appear when `~/.codex` exists, are shown in teal-green, and add keys `a` (group by agent) to the key table; note `n` asks Claude/Codex. Add `--codex-dir` to the flags list.

- [ ] **Step 4: End-to-end against real data (this machine has both)**

```bash
export PATH=$HOME/.local/go/bin:$PATH && make build
# fake codex to capture the resume command without launching a real session
mkdir -p /tmp/asm-fakebin
cat > /tmp/asm-fakebin/codex <<'EOF'
#!/bin/bash
echo "FAKE_CODEX cwd=$(pwd) args=$*" > /tmp/asm-codex-capture.txt
echo resumed; read _
EOF
chmod +x /tmp/asm-fakebin/codex
tmux kill-session -t codextest 2>/dev/null
tmux new-session -d -s codextest -x 130 -y 42 "PATH=/tmp/asm-fakebin:$PATH ./sm"
sleep 3
tmux capture-pane -t codextest -p | sed -n '1,20p'   # expect Codex sessions (teal 'codex' tags) intermixed
```
Manual checklist (drive via tmux send-keys or by hand):
1. Codex sessions appear in the list with a teal `codex` tag; Claude with coral `claude`.
2. `a` toggles per-project agent subheaders (`─ Claude ─` / `─ Codex ─`) in mixed projects; single-agent projects stay flat.
3. Selecting a Codex session and pressing `enter` writes `FAKE_CODEX cwd=<origin> args=resume <uuid>` to `/tmp/asm-codex-capture.txt`.
4. `n` on a Codex-or-Claude project shows the `[1] Claude [2] Codex` dialog and launches the chosen agent in that project dir.
5. `d` on a Codex session moves its rollout into `~/.codex/sessions/.trash/` (verify with `ls`); on a Claude session, into `~/.claude/projects/.trash/`.
6. Fuzzy filter, full-text search, fold, scroll, narrow mode all still work.

Report any checklist item that fails; fix before install.

- [ ] **Step 5: Install (no publish)**

```bash
export PATH=$HOME/.local/go/bin:$PATH && make install && ~/.local/bin/sm --version
```
Expected: installs to `~/.local/bin/sm`, prints `sm 0.2.0`. **Do NOT tag/release/publish.** Report to the user that it's installed and ready to try.

- [ ] **Step 6: Commit**

```bash
git add cmd/sm/main.go README.md
git commit -m "feat: wire Codex support into main, document agents

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```
