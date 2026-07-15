# `open_in` (current terminal vs new tmux window) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `config.json` key `open_in: "current" | "window"` that makes resume (`enter`) and new session (`n`) launch the agent either in the current terminal (today's behavior) or in a new tmux window, with sm's tmux tracking extended to window-level so ● markers, `x` kill, and adoption keep working.

**Architecture:** `open_in` is orthogonal to `tmux.enabled` — it decides *where* the agent opens; `tmux.enabled` decides whether sm *tracks* it. Window mode runs `tmux new-window` via a new non-suspending runner (plain `exec.Command`, not `tea.ExecProcess`), so sm stays on screen. Tracking stays pure name-discovery: `Runner.List()` grows to also return `sm-`-prefixed *window* names, and `Kill`/`Rename`/`Path` resolve a name as session-first-then-window. Spec: `docs/superpowers/specs/2026-07-15-open-in-window-design.md`.

**Tech Stack:** Go, Bubble Tea (`tea.Cmd`/`tea.Msg`), tmux CLI. No new dependencies.

## Global Constraints

- Run tests with `go test ./...` from the repo root; everything must pass at every commit.
- Format with `gofmt -w` on any file you touch (CI assumes gofmt-clean).
- Commit messages follow the existing convention: `feat(scope): …`, `fix(scope): …`, `docs: …` (see `git log --oneline`).
- Comment style: package/function doc comments explain *why* and constraints, matching the existing density (see `internal/tmux/tmux.go`).
- Error dialog copy must be exactly as written in each task (tests assert substrings).
- The config default must remain `"current"` — existing users' behavior must not change.

---

### Task 1: config — `open_in` key

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `config.OpenInCurrent = "current"`, `config.OpenInWindow = "window"` (string consts), `Config.OpenIn string` field (always one of the two consts after `Load`/`Default`), `Default().OpenIn == OpenInCurrent`. Task 4 reads `cfg.OpenIn`.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestLoadOpenIn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"open_in": "window"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil || cfg.OpenIn != OpenInWindow {
		t.Fatalf(`open_in "window": cfg.OpenIn=%q err=%v`, cfg.OpenIn, err)
	}
	if err := os.WriteFile(p, []byte(`{"open_in": "bogus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(p)
	if err != nil || cfg.OpenIn != OpenInCurrent {
		t.Fatalf(`unknown open_in must fall back to "current": cfg.OpenIn=%q err=%v`, cfg.OpenIn, err)
	}
	if def := Default(); def.OpenIn != OpenInCurrent {
		t.Fatalf(`Default().OpenIn = %q, want "current"`, def.OpenIn)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadOpenIn -v`
Expected: FAIL — compile error `undefined: OpenInWindow` (and `OpenIn` field missing).

- [ ] **Step 3: Implement**

In `internal/config/config.go`:

Add consts right after the `import` block:

```go
// OpenIn values: where resume/new-session launches the agent.
const (
	OpenInCurrent = "current" // suspend sm, run in the current terminal (default)
	OpenInWindow  = "window"  // open a new tmux window; sm stays on screen
)
```

Add the field to `Config` (after `View`):

```go
	OpenIn string // where launches open: "current" (this terminal) or "window" (new tmux window)
```

Add to `Default()` (after `View: "list",`):

```go
		OpenIn: OpenInCurrent,
```

Update `DefaultFileJSON` — insert the key after the `"view"` line:

```go
const DefaultFileJSON = `{
  "view": "list",
  "open_in": "current",
  "tmux": { "enabled": false },
  "colors": {
    "claude": { "light": "#C15F3C", "dark": "#D97757" },
    "codex":  { "light": "#0A7C66", "dark": "#10A37F" }
  }
}
`
```

Add to `fileConfig` (after `View`):

```go
	OpenIn *string `json:"open_in"`
```

Add to `Load`, right after the `f.View` validation block (same pattern):

```go
	if f.OpenIn != nil && (*f.OpenIn == OpenInCurrent || *f.OpenIn == OpenInWindow) {
		cfg.OpenIn = *f.OpenIn
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS — including the pre-existing `TestDefaultFileJSONParsesToDefault`, which pins `DefaultFileJSON` to `Default()` (it would fail if you updated only one of the two).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add open_in key (current terminal vs new tmux window)"
```

---

### Task 2: tmux — `WindowArgs` argv builder

**Files:**
- Modify: `internal/tmux/tmux.go`
- Test: `internal/tmux/tmux_test.go`

**Interfaces:**
- Consumes: `Prefix` (existing).
- Produces: `tmux.WindowArgs(name, cwd, agentName string, agentArgs []string) []string`. Task 5 calls it with `name=""` (untracked) or `name=sm-…` (tracked/pending).

- [ ] **Step 1: Write the failing test**

Append to `internal/tmux/tmux_test.go`:

```go
func TestWindowArgs(t *testing.T) {
	got := WindowArgs("sm-claude-s1", "/x/alpha", "claude", []string{"--resume", "s1"})
	want := []string{"new-window", "-c", "/x/alpha", "-n", "sm-claude-s1", "claude", "--resume", "s1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WindowArgs named = %v", got)
	}
	got = WindowArgs("", "/x/beta", "codex", nil)
	want = []string{"new-window", "-c", "/x/beta", "codex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WindowArgs unnamed = %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tmux/ -run TestWindowArgs -v`
Expected: FAIL — `undefined: WindowArgs`.

- [ ] **Step 3: Implement**

Add to `internal/tmux/tmux.go`, after `NewArgs`:

```go
// WindowArgs builds the tmux argv (after the "tmux" binary) that opens a new
// window in the caller's current tmux session, running the agent command in
// cwd. A non-empty name tags the window for sm's tracking — -n also disables
// tmux's automatic-rename for that window, so the name stays stable until
// adoption renames it. An empty name leaves the window untracked.
func WindowArgs(name, cwd, agentName string, agentArgs []string) []string {
	args := []string{"new-window", "-c", cwd}
	if name != "" {
		args = append(args, "-n", name)
	}
	args = append(args, agentName)
	return append(args, agentArgs...)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tmux/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tmux/tmux.go internal/tmux/tmux_test.go
git commit -m "feat(tmux): WindowArgs builder for new-window launches"
```

---

### Task 3: tmux — window discovery and dual-form Runner operations

**Files:**
- Modify: `internal/tmux/tmux.go` (Runner interface, Exec methods)
- Modify: `internal/ui/model_test.go` (`fakeTmux` grows the new method — same commit or `go test ./internal/ui/` breaks)
- Test: `internal/tmux/tmux_test.go`

**Interfaces:**
- Consumes: `Prefix`, `parseList` (existing).
- Produces:
  - `Runner` gains `Window(name string) (id, session string, ok bool)` — resolves an sm window name to its tmux window id (`@N`) and owning session name; `ok=false` means "no such window" (name is session-form or dead).
  - `Exec.List()` returns the union of `sm-` session names and `sm-` window names.
  - `Exec.Kill/Rename/Path` transparently operate on whichever form the name resolves to (session checked first).
  - `parseWindows(out string) map[string][2]string` (unexported, tested directly).
  - ui's `fakeTmux` gains `windows map[string][2]string` (name → `{windowID, sessionName}`) and a `Window` method. Tasks 5–6 configure it.

- [ ] **Step 1: Write the failing test**

Append to `internal/tmux/tmux_test.go`:

```go
func TestParseWindows(t *testing.T) {
	out := "@1\tmain\tsm-claude-s1\n@2\tmain\tvim\n@3\twork\tsm-codex-pending-9\n\n"
	got := parseWindows(out)
	if w := got["sm-claude-s1"]; w != [2]string{"@1", "main"} {
		t.Errorf("sm-claude-s1 = %v, want {@1 main}", w)
	}
	if w := got["sm-codex-pending-9"]; w != [2]string{"@3", "work"} {
		t.Errorf("sm-codex-pending-9 = %v, want {@3 work}", w)
	}
	if _, ok := got["vim"]; ok {
		t.Error("parseWindows should drop non-sm window names")
	}
	if len(got) != 2 {
		t.Errorf("parseWindows size = %d, want 2", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tmux/ -run TestParseWindows -v`
Expected: FAIL — `undefined: parseWindows`.

- [ ] **Step 3: Implement the tmux package half**

In `internal/tmux/tmux.go`:

Extend the `Runner` interface — add after the `Rename` method:

```go
	// Window resolves an sm-prefixed *window* name to its tmux window id
	// ("@N") and owning session name. ok is false when no such window is
	// live — the name is session-form, pending-session-form, or dead.
	Window(name string) (id, session string, ok bool)
```

Update the `Runner` doc comment's opening to mention both forms:

```go
// Runner is the injectable tmux boundary. The real implementation is Exec;
// tests inject a fake. Names may denote tmux *sessions* (open_in "current")
// or tmux *windows* (open_in "window"); List discovers both, and the other
// operations resolve a name session-first, then window.
```

Replace `Exec.List` and add helpers + dual-form methods (replacing the existing `Kill`, `Rename`, `Path` bodies):

```go
func (Exec) List() (map[string]bool, error) {
	set := map[string]bool{}
	if out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output(); err == nil {
		for name := range parseList(string(out)) {
			set[name] = true
		}
	}
	for name := range listWindows() {
		set[name] = true
	}
	// No server running (or no sessions) is an empty set, not an error.
	return set, nil
}

func (Exec) Window(name string) (string, string, bool) {
	w, ok := listWindows()[name]
	if !ok {
		return "", "", false
	}
	return w[0], w[1], true
}

func (e Exec) Kill(name string) error {
	if !hasSession(name) {
		if id, _, ok := e.Window(name); ok {
			return exec.Command("tmux", "kill-window", "-t", id).Run()
		}
	}
	return exec.Command("tmux", "kill-session", "-t", name).Run()
}

func (e Exec) Rename(from, to string) error {
	if !hasSession(from) {
		if id, _, ok := e.Window(from); ok {
			return exec.Command("tmux", "rename-window", "-t", id, to).Run()
		}
	}
	return exec.Command("tmux", "rename-session", "-t", from, to).Run()
}

func (e Exec) Path(name string) (string, error) {
	target := name
	if !hasSession(name) {
		if id, _, ok := e.Window(name); ok {
			target = id
		}
	}
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target, "#{pane_current_path}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// hasSession reports whether a tmux *session* with exactly this name is live
// ("=" pins tmux's default prefix matching to an exact match).
func hasSession(name string) bool {
	return exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil
}

// listWindows returns every live sm-prefixed window, keyed by window name.
func listWindows() map[string][2]string {
	out, err := exec.Command("tmux", "list-windows", "-a", "-F",
		"#{window_id}\t#{session_name}\t#{window_name}").Output()
	if err != nil {
		return map[string][2]string{}
	}
	return parseWindows(string(out))
}

// parseWindows maps sm-prefixed window names to {window id, session name}.
// Input rows are "id\tsession\tname". Duplicate names keep the first row —
// callers always target the id, so a duplicate can hide but never mis-target.
func parseWindows(out string) map[string][2]string {
	m := map[string][2]string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 3)
		if len(parts) != 3 || !strings.HasPrefix(parts[2], Prefix) {
			continue
		}
		if _, dup := m[parts[2]]; !dup {
			m[parts[2]] = [2]string{parts[0], parts[1]}
		}
	}
	return m
}
```

- [ ] **Step 4: Update ui's fakeTmux so `./internal/ui` compiles**

In `internal/ui/model_test.go`, the `fakeTmux` struct (near line 462) gains a field and method:

```go
type fakeTmux struct {
	live    map[string]bool
	windows map[string][2]string // window name → {window id, session name}
	paths   map[string]string
	killed  []string
	renamed [][2]string
}
```

Add after the `Rename` method:

```go
func (f *fakeTmux) Window(name string) (string, string, bool) {
	w, ok := f.windows[name]
	if !ok {
		return "", "", false
	}
	return w[0], w[1], true
}
```

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: PASS — everything compiles, existing tmux/ui tests unaffected.

- [ ] **Step 6: Commit**

```bash
git add internal/tmux/tmux.go internal/tmux/tmux_test.go internal/ui/model_test.go
git commit -m "feat(tmux): discover and operate on sm-named windows alongside sessions"
```

---

### Task 4: ui — `openIn` wiring, `insideTmux`, consolidated `launchErr`

**Files:**
- Modify: `internal/ui/model.go`
- Test: `internal/ui/model_test.go`

**Interfaces:**
- Consumes: `config.OpenInWindow`, `config.OpenInCurrent` (Task 1).
- Produces:
  - `Model.openIn string` field, set from `cfg.OpenIn` in `New`.
  - `var insideTmux = func() bool` — package-level, test-stubbable (same pattern as `tmuxLookPath`).
  - `(m Model) launchErr(p store.Provider) string` — "" when launching is possible; otherwise the exact dialog text. Replaces the three inline `binLookPath` checks. Tasks 5–6 rely on window-mode preconditions being enforced *before* `runAgentCmd` runs.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/model_test.go`:

```go
func TestWindowModeOutsideTmuxErrors(t *testing.T) {
	origIn, origLook := insideTmux, tmuxLookPath
	insideTmux = func() bool { return false }
	tmuxLookPath = func() bool { return true }
	defer func() { insideTmux, tmuxLookPath = origIn, origLook }()
	m := newTestModel()
	m.openIn = config.OpenInWindow
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0)
	m2, _ := m.startResume()
	m = m2.(Model)
	if m.dialog != dialogError || !strings.Contains(m.errText, "inside tmux") {
		t.Errorf("dialog=%v errText=%q, want error mentioning inside tmux", m.dialog, m.errText)
	}
}

func TestWindowModeNeedsTmuxOnPath(t *testing.T) {
	origIn, origLook := insideTmux, tmuxLookPath
	insideTmux = func() bool { return true }
	tmuxLookPath = func() bool { return false }
	defer func() { insideTmux, tmuxLookPath = origIn, origLook }()
	m := newTestModel()
	m.openIn = config.OpenInWindow
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0)
	m2, _ := m.startResume()
	m = m2.(Model)
	if m.dialog != dialogError || !strings.Contains(m.errText, "tmux on PATH") {
		t.Errorf("dialog=%v errText=%q, want error mentioning tmux on PATH", m.dialog, m.errText)
	}
}

func TestNewCarriesOpenInFromConfig(t *testing.T) {
	cfg := config.Default()
	cfg.OpenIn = config.OpenInWindow
	m := New("/nope", "/nope", cfg)
	if m.openIn != config.OpenInWindow {
		t.Errorf("openIn = %q, want window", m.openIn)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestWindowMode|TestNewCarriesOpenIn' -v`
Expected: FAIL — compile errors: `undefined: insideTmux`, `m.openIn undefined`.

- [ ] **Step 3: Implement**

In `internal/ui/model.go`:

Add the field to `Model` (after `tmuxLive    map[string]bool`):

```go
	openIn      string // config.OpenInCurrent or config.OpenInWindow
```

In `New`, add to the `Model` literal (after `tmuxEnabled:   cfg.TmuxEnabled,`):

```go
		openIn:        cfg.OpenIn,
```

Add the stubbable env check next to `tmuxLookPath` (after its definition, ~line 211):

```go
// insideTmux reports whether sm itself runs inside a tmux client — the
// precondition for open_in "window", which targets the attached session.
// Overridable in tests.
var insideTmux = func() bool {
	return os.Getenv("TMUX") != ""
}
```

Add `launchErr` right after `binLookPath`'s definition:

```go
// launchErr reports why launching p cannot proceed right now ("" = it can):
// the agent binary is missing, or open_in "window" lacks its tmux
// preconditions. Checked at launch time, not startup, so a config problem
// only bites the action that needs it.
func (m Model) launchErr(p store.Provider) string {
	if err := binLookPath(p.Binary()); err != nil {
		return p.Binary() + " not found on PATH"
	}
	if m.openIn == config.OpenInWindow {
		if !tmuxLookPath() {
			return `open_in "window" requires tmux on PATH`
		}
		if !insideTmux() {
			return `open_in "window" requires running sm inside tmux`
		}
	}
	return ""
}
```

Replace the three inline `binLookPath` checks with `launchErr`:

In `startResume` (~line 1093), replace:

```go
	if err := binLookPath(p.Binary()); err != nil {
		m.dialog = dialogError
		m.errText = p.Binary() + " not found on PATH"
		return m, nil
	}
```

with:

```go
	if msg := m.launchErr(p); msg != "" {
		m.dialog = dialogError
		m.errText = msg
		return m, nil
	}
```

In `launchDirectly` (~line 1115), replace:

```go
	if err := binLookPath(p.Binary()); err != nil {
		m.dialog = dialogError
		m.errText = p.Binary() + " not found on PATH"
		return m, nil
	}
```

with:

```go
	if msg := m.launchErr(p); msg != "" {
		m.dialog = dialogError
		m.errText = msg
		return m, nil
	}
```

In the `dialogPickDir` enter handler (~line 1243), replace:

```go
			if err := binLookPath(p.Binary()); err != nil {
				m.dialog = dialogError
				m.errText = p.Binary() + " not found on PATH"
				return m, nil
			}
```

with:

```go
			if msg := m.launchErr(p); msg != "" {
				m.dialog = dialogError
				m.errText = msg
				return m, nil
			}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS — including the pre-existing `TestResumeErrorsWhenBinaryMissing` (the "not found on PATH" copy is unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): open_in config plumbing and window-mode launch preconditions"
```

---

### Task 5: ui — window-mode launches via a non-suspending runner

**Files:**
- Modify: `internal/ui/model.go`
- Test: `internal/ui/model_test.go`

**Interfaces:**
- Consumes: `tmux.WindowArgs` (Task 2), `m.openIn` + preconditions (Task 4).
- Produces:
  - `Model.runSilent func(name, dir string, args ...string) tea.Cmd` injectable (default `execSilent`) — runs a command without suspending the TUI.
  - `silentDoneMsg struct{ err error }` handled in `Update`: error dialog on failure, then rescan + tmux refresh.
  - `runAgentCmd` grows window-mode branches for both resume and new. The live-tmux resume branch (`m.tmuxLive[sess]`) attaches in the current terminal for now — Task 6 refines the window form.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/model_test.go`:

```go
// newWindowModel is newTestModel in open_in "window" mode: tmux preconditions
// stubbed true, runSilent captured, runCmd trapped (window mode must never
// suspend the TUI via ExecProcess).
func newWindowModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	origIn, origLook := insideTmux, tmuxLookPath
	insideTmux = func() bool { return true }
	tmuxLookPath = func() bool { return true }
	t.Cleanup(func() { insideTmux, tmuxLookPath = origIn, origLook })
	m := newTestModel()
	m.openIn = config.OpenInWindow
	captured := &[]string{}
	m.runSilent = func(name, dir string, args ...string) tea.Cmd {
		*captured = append([]string{name, dir}, args...)
		return nil
	}
	m.runCmd = func(name, dir string, args ...string) tea.Cmd {
		t.Errorf("window mode must not suspend via runCmd: %s %s %v", name, dir, args)
		return nil
	}
	m.now = func() time.Time { return time.Unix(0, 1234) }
	return m, captured
}

func TestResumeWindowModeTracked(t *testing.T) {
	m, cap := newWindowModel(t)
	m.tmuxEnabled = true
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0)
	m.startResume()
	joined := strings.Join(*cap, " ")
	if !strings.Contains(joined, "new-window -c "+dir+" -n sm-claude-s1 claude --resume s1") {
		t.Errorf("tracked window resume argv = %v", *cap)
	}
}

func TestResumeWindowModeUntracked(t *testing.T) {
	m, cap := newWindowModel(t) // tmuxEnabled stays false
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0)
	m.startResume()
	joined := strings.Join(*cap, " ")
	if !strings.Contains(joined, "new-window -c "+dir+" claude --resume s1") {
		t.Errorf("untracked window resume argv = %v", *cap)
	}
	if strings.Contains(joined, "-n sm-") {
		t.Error("untracked window must not carry an sm- name")
	}
}

func TestNewSessionWindowModePending(t *testing.T) {
	m, cap := newWindowModel(t)
	m.tmuxEnabled = true
	m.launchNewSession("/x/alpha") // single provider (claude-only test model)
	joined := strings.Join(*cap, " ")
	if !strings.Contains(joined, "new-window -c /x/alpha -n sm-claude-pending-1234 claude") {
		t.Errorf("pending window new argv = %v", *cap)
	}
}

func TestSilentFailureShowsErrorAndRefreshes(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(silentDoneMsg{err: errors.New("boom")})
	m = m2.(Model)
	if m.dialog != dialogError || !strings.Contains(m.errText, "boom") {
		t.Errorf("dialog=%v errText=%q, want error containing boom", m.dialog, m.errText)
	}
	m = newTestModel()
	m2, _ = m.Update(silentDoneMsg{})
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Errorf("clean exit should not raise a dialog, got %v", m.dialog)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'WindowMode|SilentFailure' -v`
Expected: FAIL — compile errors: `m.runSilent undefined`, `undefined: silentDoneMsg`.

- [ ] **Step 3: Implement**

In `internal/ui/model.go`:

Add the message type next to the other msg declarations (search for `agentExitMsg` type definition; add alongside):

```go
// silentDoneMsg reports a fire-and-forget command (tmux new-window /
// select-window) finishing. Unlike agentExitMsg there is no ExecProcess —
// the TUI never suspended.
type silentDoneMsg struct{ err error }
```

Add the injectable to `Model` (after `runCmd  func(...) tea.Cmd`):

```go
	runSilent func(name, dir string, args ...string) tea.Cmd
```

Set it in `New`'s literal (after `runCmd:       execCmd,`):

```go
		runSilent:    execSilent,
```

Add `execSilent` after `execCmd`:

```go
// execSilent runs a quick command without suspending the TUI the way
// execCmd's ExecProcess does — for tmux new-window and friends, which
// return immediately while the agent keeps running in its window.
func execSilent(name, dir string, args ...string) tea.Cmd {
	c := exec.Command(name, args...)
	c.Dir = dir
	return func() tea.Msg { return silentDoneMsg{err: c.Run()} }
}
```

Handle the message in `Update` — add a case next to `case agentExitMsg:` (same refresh shape):

```go
	case silentDoneMsg:
		if msg.err != nil {
			m.dialog = dialogError
			m.errText = "tmux window failed: " + msg.err.Error()
		}
		if m.tmuxEnabled {
			return m, tea.Batch(m.scanCmd(), m.refreshTmuxCmd())
		}
		return m, m.scanCmd()
```

Replace `runAgentCmd` entirely:

```go
// runAgentCmd launches an agent. resume != nil resumes that session;
// resume == nil starts a new session with p in cwd. open_in decides where it
// opens (current terminal vs new tmux window); tmux.enabled decides whether
// the launch carries an sm- name and is therefore tracked. A live tracked
// tmux always wins: enter jumps to it instead of creating a second one.
func (m Model) runAgentCmd(p store.Provider, cwd string, resume *store.Session) tea.Cmd {
	if resume != nil {
		name, args := p.ResumeCommand(*resume)
		sess := tmux.Name(string(resume.Agent), tmux.Short(resume.ID))
		if m.tmuxEnabled && m.tmuxLive[sess] {
			// Session form: new-session -A attaches. (Window form jump
			// lands in attachLiveCmd — Task 6.)
			return m.runCmd("tmux", cwd, tmux.ResumeArgs(sess, cwd, name, args)...)
		}
		if m.openIn == config.OpenInWindow {
			win := ""
			if m.tmuxEnabled {
				win = sess
			}
			return m.runSilent("tmux", cwd, tmux.WindowArgs(win, cwd, name, args)...)
		}
		if m.tmuxEnabled {
			return m.runCmd("tmux", cwd, tmux.ResumeArgs(sess, cwd, name, args)...)
		}
		return m.runCmd(name, cwd, args...)
	}
	name, args := p.NewCommand()
	if m.openIn == config.OpenInWindow {
		win := ""
		if m.tmuxEnabled {
			win = tmux.PendingName(string(p.Agent()), m.now().UnixNano())
		}
		return m.runSilent("tmux", cwd, tmux.WindowArgs(win, cwd, name, args)...)
	}
	if m.tmuxEnabled {
		pend := tmux.PendingName(string(p.Agent()), m.now().UnixNano())
		return m.runCmd("tmux", cwd, tmux.NewArgs(pend, cwd, name, args)...)
	}
	return m.runCmd(name, cwd, args...)
}
```

- [ ] **Step 4: Run the full suite**

Run: `go test ./...`
Expected: PASS. The pre-existing tmux-mode tests (`TestResumeWrapsInTmuxWhenEnabled`, `TestNewWrapsInTmuxWhenEnabled`, `TestResumeStaysInlineWhenDisabled`) still pass — `openIn` defaults to `"current"` in `newTestModel`.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): launch resume/new sessions in a new tmux window (open_in: window)"
```

---

### Task 6: ui — jump to a live window-form tmux

**Files:**
- Modify: `internal/ui/model.go`
- Test: `internal/ui/model_test.go`

**Interfaces:**
- Consumes: `Runner.Window` (Task 3), `insideTmux` (Task 4), `runSilent` (Task 5).
- Produces: `(m Model) attachLiveCmd(sess, cwd, agentName string, agentArgs []string) tea.Cmd`, called from `runAgentCmd`'s live branch. Behavior: window form + inside tmux → `select-window` + `switch-client` (silent); window form + outside tmux → `select-window` + `attach-session` (ExecProcess); session form → `new-session -A` attach, exactly as before.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/model_test.go`:

```go
func TestEnterLiveWindowJumpsInsideTmux(t *testing.T) {
	m, cap := newWindowModel(t)
	m.tmuxEnabled = true
	m.tmux = &fakeTmux{windows: map[string][2]string{"sm-claude-s1": {"@7", "main"}}}
	m.tmuxLive = map[string]bool{"sm-claude-s1": true}
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0)
	m.startResume()
	joined := strings.Join(*cap, " ")
	if !strings.Contains(joined, "select-window -t @7 ; switch-client -t main") {
		t.Errorf("live window jump argv = %v", *cap)
	}
}

// Jumping to a live window must work regardless of open_in — here the config
// says "current", sm runs outside tmux, and enter still lands in the window
// by attaching the terminal to its owning session.
func TestEnterLiveWindowAttachesOutsideTmux(t *testing.T) {
	origIn := insideTmux
	insideTmux = func() bool { return false }
	defer func() { insideTmux = origIn }()
	m := newTestModel() // openIn stays "current"
	m.tmuxEnabled = true
	m.tmux = &fakeTmux{windows: map[string][2]string{"sm-claude-s1": {"@7", "main"}}}
	m.tmuxLive = map[string]bool{"sm-claude-s1": true}
	captured := &[]string{}
	m.runCmd = func(name, dir string, args ...string) tea.Cmd {
		*captured = append([]string{name, dir}, args...)
		return nil
	}
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0)
	m.startResume()
	joined := strings.Join(*captured, " ")
	if !strings.Contains(joined, "select-window -t @7 ; attach-session -t main") {
		t.Errorf("outside-tmux window jump argv = %v", *captured)
	}
}

// A live session-form tmux must attach (new-session -A) even in window mode:
// creating a window with the same sm- name would fork the id across two
// tmux entities.
func TestEnterLiveSessionFormAttachesEvenInWindowMode(t *testing.T) {
	m, _ := newWindowModel(t)
	m.tmuxEnabled = true
	m.tmux = &fakeTmux{live: map[string]bool{"sm-claude-s1": true}} // no windows
	m.tmuxLive = map[string]bool{"sm-claude-s1": true}
	captured := &[]string{}
	m.runCmd = func(name, dir string, args ...string) tea.Cmd { // replace the trap
		*captured = append([]string{name, dir}, args...)
		return nil
	}
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0)
	m.startResume()
	joined := strings.Join(*captured, " ")
	if !strings.Contains(joined, "new-session -A -s sm-claude-s1") {
		t.Errorf("live session-form resume argv = %v", *captured)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run TestEnterLive -v`
Expected: `TestEnterLiveWindowJumpsInsideTmux` FAILS (the trap `runCmd` fires — Task 5's live branch attaches via ExecProcess). `TestEnterLiveSessionFormAttachesEvenInWindowMode` passes already (Task 5 handles it); keep it as a regression pin. `TestEnterLiveWindowAttachesOutsideTmux` FAILS (argv is `new-session -A …`, not the window jump).

- [ ] **Step 3: Implement**

In `internal/ui/model.go`, add after `runAgentCmd`:

```go
// attachLiveCmd jumps to the live tmux backing sess. Window form: select the
// window and switch the client to its owning session (a same-session switch
// is a no-op); when sm itself runs outside tmux, attach the terminal to that
// session instead. The ";" argv element is tmux's command separator, so both
// steps ride one invocation. Session form: attach via new-session -A, exactly
// as before.
func (m Model) attachLiveCmd(sess, cwd, agentName string, agentArgs []string) tea.Cmd {
	if id, owner, ok := m.tmux.Window(sess); ok {
		if insideTmux() {
			return m.runSilent("tmux", cwd, "select-window", "-t", id, ";", "switch-client", "-t", owner)
		}
		return m.runCmd("tmux", cwd, "select-window", "-t", id, ";", "attach-session", "-t", owner)
	}
	return m.runCmd("tmux", cwd, tmux.ResumeArgs(sess, cwd, agentName, agentArgs)...)
}
```

In `runAgentCmd`, replace the live branch:

```go
		if m.tmuxEnabled && m.tmuxLive[sess] {
			// Session form: new-session -A attaches. (Window form jump
			// lands in attachLiveCmd — Task 6.)
			return m.runCmd("tmux", cwd, tmux.ResumeArgs(sess, cwd, name, args)...)
		}
```

with:

```go
		if m.tmuxEnabled && m.tmuxLive[sess] {
			return m.attachLiveCmd(sess, cwd, name, args)
		}
```

- [ ] **Step 4: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): enter jumps to a live window-form tmux (select-window/switch-client)"
```

---

### Task 7: docs — README

**Files:**
- Modify: `README.md`

**Interfaces:** none — documentation of Tasks 1–6.

- [ ] **Step 1: Update the Configuration section**

In the config example JSON (~line 235), add the `open_in` line after `"view"`:

```json
{
  "view": "list",
  "open_in": "current",
  "tmux": { "enabled": false },
  ...
}
```

After the `tmux.enabled` bullet (~line 245), add:

```markdown
- `open_in` (default `"current"`) — where `enter` (resume) and `n` (new
  session) launch the agent. `"current"` suspends `sm` and runs it in this
  terminal. `"window"` opens a new tmux window in the tmux session you are
  attached to — `sm` stays on screen, and over SSH the window naturally lives
  on the same connection. Requires running `sm` inside tmux (and `tmux` on
  `PATH`); `sm` shows an error otherwise. Works independently of
  `tmux.enabled`: with tmux integration on, the window is named `sm-…` and
  gets the ● marker / `x` kill treatment; with it off, it is a plain
  untracked window.
```

- [ ] **Step 2: Update the tmux integration section**

In the tmux integration bullets (~line 256), add one bullet:

```markdown
- With `open_in: "window"`, tracked launches are tmux *windows* (named
  `sm-<agent>-<id8>`) instead of detached sessions; ●, `x`, and adoption
  work the same, and `enter` on a live one switches to its window.
```

- [ ] **Step 3: Verify and commit**

Run: `go test ./...` (docs-only change; sanity that nothing else drifted)
Expected: PASS.

```bash
git add README.md
git commit -m "docs: document open_in (current terminal vs new tmux window)"
```

---

## Self-review notes (already applied)

- **Spec coverage:** config key + validation (Task 1); launch matrix all four cells (Task 5 tests: tracked/untracked window resume, pending window new; existing tests pin the two `current` cells); window preconditions and exact error copy (Task 4); List/Kill/Rename/Path dual-form + window ids for duplicate-name safety (Task 3); adoption via unchanged `adoptPending` (name-based — Task 3's `Rename`/`Path` handle the window form; no new adoption code needed); live-tmux jump incl. the sm-outside-tmux attach edge and the session-form-in-window-mode pin (Task 6); non-suspending runner + failure surfacing (Task 5); README (Task 7).
- **Deliberate scope note:** `killProjectCmd` and `killOneCmd` need no changes — they call `Runner.Kill`/`Path` by name, which Task 3 makes form-agnostic.
- **Type consistency:** `Window(name) (id, session string, ok bool)` is used identically in Task 3 (Exec + fake), and Task 6 (`m.tmux.Window`). `runSilent` signature matches `runCmd`'s shape everywhere.

---

## Amendment (2026-07-15, post-review): startup auto-wrap

User feedback after live testing: requiring sm to already run inside tmux is
too awkward. Approved design change (spec section 5): with
`open_in: "window"`, sm started outside tmux replaces itself with a tmux
client attached to sm's own session (named `sm`), creating the session or a
fresh sm window as needed; a live detached workspace is reattached.

### Task 8: tmux — self-wrap argv builders and server probe

**Files:**
- Modify: `internal/tmux/tmux.go`
- Test: `internal/tmux/tmux_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `tmux.SelfSession = "sm"`, `tmux.SelfWindow = "sm"` (consts);
  `tmux.SelfWrapArgs(selfCmd []string, cwd string, sessionExists bool, smWindowID string) []string`;
  `tmux.SelfState() (sessionExists bool, smWindowID string)` (shells out);
  `parseSelfWindow(out string) string` (unexported, tested). Task 9's main.go
  calls `SelfState` + `SelfWrapArgs`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tmux/tmux_test.go`:

```go
func TestSelfWrapArgs(t *testing.T) {
	self := []string{"/usr/local/bin/sm", "--config", "/x/c.json"}
	got := SelfWrapArgs(self, "/work", false, "")
	want := []string{"new-session", "-s", "sm", "-n", "sm", "-c", "/work",
		"/usr/local/bin/sm", "--config", "/x/c.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fresh session = %v", got)
	}
	got = SelfWrapArgs(self, "/work", true, "@3")
	want = []string{"select-window", "-t", "@3", ";", "attach-session", "-t", "=sm"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reattach = %v", got)
	}
	got = SelfWrapArgs(self, "/work", true, "")
	want = []string{"new-window", "-t", "=sm:", "-n", "sm", "-c", "/work",
		"/usr/local/bin/sm", "--config", "/x/c.json", ";", "attach-session", "-t", "=sm"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("respawn = %v", got)
	}
}

func TestParseSelfWindow(t *testing.T) {
	out := "@1\tvim\n@2\tsm\n"
	if got := parseSelfWindow(out); got != "@2" {
		t.Errorf("parseSelfWindow = %q, want @2", got)
	}
	if got := parseSelfWindow("@1\tother\n\n"); got != "" {
		t.Errorf("no sm window should yield empty, got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tmux/ -run 'TestSelfWrapArgs|TestParseSelfWindow' -v`
Expected: FAIL — `undefined: SelfWrapArgs`, `undefined: parseSelfWindow`.

- [ ] **Step 3: Implement**

Add to `internal/tmux/tmux.go` (after `WindowArgs`):

```go
// SelfSession / SelfWindow name sm's own tmux home used by the open_in
// "window" startup wrap. Deliberately NOT "sm-" prefixed: agent tracking
// discovers by that prefix, and sm's own tmux must stay invisible to
// ●/x/adoption. Reattachment probes by exact match instead.
const (
	SelfSession = "sm"
	SelfWindow  = "sm"
)

// SelfWrapArgs builds the tmux argv (after the "tmux" binary) that lands the
// user inside sm's own tmux session, (re)starting sm as needed. selfCmd is
// sm's own binary and args; cwd pins the window so relative flag paths keep
// resolving. Three server states (probed by SelfState): no session — create
// it running sm; session with a live sm window — select it and attach (the
// detached-workspace reattach); session whose sm window has exited — spawn a
// fresh sm window (new-window makes it current) and attach. The last branch
// is why a bare `new-session -A` is not enough: quitting sm kills its window
// while agent windows keep the session alive.
func SelfWrapArgs(selfCmd []string, cwd string, sessionExists bool, smWindowID string) []string {
	switch {
	case !sessionExists:
		return append([]string{"new-session", "-s", SelfSession, "-n", SelfWindow, "-c", cwd}, selfCmd...)
	case smWindowID != "":
		return []string{"select-window", "-t", smWindowID, ";", "attach-session", "-t", "=" + SelfSession}
	default:
		args := append([]string{"new-window", "-t", "=" + SelfSession + ":", "-n", SelfWindow, "-c", cwd}, selfCmd...)
		return append(args, ";", "attach-session", "-t", "="+SelfSession)
	}
}

// SelfState probes the tmux server for sm's own session and its live sm
// window id ("" when absent). A missing server is (false, "").
func SelfState() (sessionExists bool, smWindowID string) {
	if exec.Command("tmux", "has-session", "-t", "="+SelfSession).Run() != nil {
		return false, ""
	}
	out, err := exec.Command("tmux", "list-windows", "-t", "="+SelfSession, "-F",
		"#{window_id}\t#{window_name}").Output()
	if err != nil {
		return true, ""
	}
	return true, parseSelfWindow(string(out))
}

// parseSelfWindow finds the SelfWindow-named window id in list-windows
// "id\tname" output, or "".
func parseSelfWindow(out string) string {
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) == 2 && parts[1] == SelfWindow {
			return parts[0]
		}
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tmux/ -v` then `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tmux/tmux.go internal/tmux/tmux_test.go
git commit -m "feat(tmux): self-wrap argv builders for open_in window startup"
```

---

### Task 9: startup wiring — main.go self-exec, ui fallback dialog, README

**Files:**
- Modify: `cmd/sm/main.go`
- Modify: `internal/ui/model.go` (New: tmux-missing downgrade)
- Modify: `README.md` (open_in bullet)
- Test: `internal/ui/model_test.go`

**Interfaces:**
- Consumes: `tmux.SelfState`, `tmux.SelfWrapArgs`, `tmux.SelfSession` (Task 8);
  `config.OpenInWindow/OpenInCurrent`.
- Produces: no new exported surface.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/model_test.go`:

```go
func TestWindowModeWithoutTmuxFallsBackAtStartup(t *testing.T) {
	orig := tmuxLookPath
	tmuxLookPath = func() bool { return false }
	defer func() { tmuxLookPath = orig }()
	cfg := config.Default()
	cfg.OpenIn = config.OpenInWindow
	m := New("/nope", "/nope", cfg)
	if m.openIn != config.OpenInCurrent {
		t.Errorf("openIn = %q, want fallback to current", m.openIn)
	}
	if m.dialog != dialogError || !strings.Contains(m.errText, "tmux on PATH") {
		t.Errorf("expected startup error dialog, got dialog=%v err=%q", m.dialog, m.errText)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestWindowModeWithoutTmuxFallsBackAtStartup -v`
Expected: FAIL — openIn stays `"window"`, no dialog.

- [ ] **Step 3: Implement the ui downgrade**

In `internal/ui/model.go`, in `New`, directly after the existing
`if ret.tmuxEnabled && !tmuxLookPath() { … }` block:

```go
	if ret.openIn == config.OpenInWindow && !tmuxLookPath() {
		ret.openIn = config.OpenInCurrent
		ret.dialog = dialogError
		ret.errText = `open_in "window" requires tmux on PATH — using "current" for this run`
	}
```

- [ ] **Step 4: Implement the main.go self-exec wrap**

In `cmd/sm/main.go`, after the `cfg, cfgErr := config.Load(path)` error
handling and before `tea.NewProgram`:

```go
	// open_in "window" wants sm living inside tmux — the windows it opens
	// land in the attached session. Started outside tmux, sm replaces itself
	// with a tmux client on its own session (creating the session or a fresh
	// sm window as needed; a live detached workspace is reattached). Any
	// failure falls through to a normal run: ui.New shows the tmux-missing
	// dialog and downgrades, and the in-app launch check still guards $TMUX.
	if cfg.OpenIn == config.OpenInWindow && os.Getenv("TMUX") == "" {
		if tmuxPath, err := exec.LookPath("tmux"); err == nil {
			if self, err := os.Executable(); err == nil {
				cwd, _ := os.Getwd()
				exists, winID := tmux.SelfState()
				selfCmd := append([]string{self}, os.Args[1:]...)
				argv := append([]string{"tmux"}, tmux.SelfWrapArgs(selfCmd, cwd, exists, winID)...)
				_ = syscall.Exec(tmuxPath, argv, os.Environ())
			}
		}
	}
```

Add imports: `"os/exec"`, `"syscall"`, and
`"github.com/dukechain2333/ai-sessions-manager/internal/tmux"`.

- [ ] **Step 5: Update README**

In the `open_in` bullet added by Task 7, replace the sentence
`Requires running `sm` inside tmux (and `tmux` on `PATH`); `sm` shows an error otherwise.`
with:

```markdown
  Started outside tmux, `sm` automatically re-launches itself inside its own
  tmux session (named `sm`) and reattaches it if one is already running — an
  SSH drop later, `sm` brings the whole workspace (sm plus every agent
  window) back. Needs `tmux` on `PATH`; without it `sm` shows a notice and
  falls back to `"current"` for the run.
```

- [ ] **Step 6: Run the full suite and verify the wrap manually**

Run: `go test ./...` — expected: PASS.
Run: `gofmt -l cmd internal` — expected: empty.

- [ ] **Step 7: Commit**

```bash
git add cmd/sm/main.go internal/ui/model.go internal/ui/model_test.go README.md
git commit -m "feat: auto-wrap sm into its own tmux session when open_in is window"
```

---

## Amendment 2 (2026-07-15): iTerm2 native windows via custom control sequences

Live testing rejected `-CC` (it pops sm's own window too). Approved design:
spec section 6. sm stays plain in the invoking terminal; window-mode launches
emit an invisible iTerm2 custom control sequence; a local AutoLaunch script
opens a native window ssh-ing back. Explicit opt-in: `"iterm2": {"ssh": "…"}`.

### Task 10: config — `iterm2.ssh` key

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `Config.ITerm2SSH string` (default `""` = disabled). Task 12 reads it.

- [ ] **Step 1: Failing test** — append to `internal/config/config_test.go`:

```go
func TestLoadITerm2SSH(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"iterm2": {"ssh": "myhost"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil || cfg.ITerm2SSH != "myhost" {
		t.Fatalf(`iterm2.ssh: cfg.ITerm2SSH=%q err=%v`, cfg.ITerm2SSH, err)
	}
	if def := Default(); def.ITerm2SSH != "" {
		t.Fatalf(`Default().ITerm2SSH = %q, want ""`, def.ITerm2SSH)
	}
}
```

- [ ] **Step 2:** `go test ./internal/config/ -run TestLoadITerm2SSH -v` → FAIL (undefined field).

- [ ] **Step 3: Implement** in `internal/config/config.go`:
  - `Config` gains (after `OpenIn`): `ITerm2SSH string // ssh destination for iTerm2 native-window launches ("" = disabled)`
  - `Default()` gains `ITerm2SSH: "",`
  - `DefaultFileJSON`: after the `"open_in"` line add `  "iterm2": { "ssh": "" },`
  - `fileConfig` gains:
    ```go
    	ITerm2 *struct {
    		SSH *string `json:"ssh"`
    	} `json:"iterm2"`
    ```
  - `Load` after the OpenIn block:
    ```go
    	if f.ITerm2 != nil && f.ITerm2.SSH != nil {
    		cfg.ITerm2SSH = *f.ITerm2.SSH
    	}
    ```

- [ ] **Step 4:** `go test ./internal/config/ -v` → PASS incl. the DefaultFileJSON pin test.

- [ ] **Step 5: Commit** `feat(config): iterm2.ssh key for native-window launches`

---

### Task 11: new package — `internal/iterm2` sequence builder

**Files:**
- Create: `internal/iterm2/iterm2.go`
- Test: `internal/iterm2/iterm2_test.go`

**Interfaces:**
- Produces: `iterm2.Launch{Host, Dir, Name string; Argv []string; Tmux, Attach bool}` and `iterm2.Sequence(l Launch, insideTmux bool) string`. Task 12 calls them; Task 13's Python script parses the payload.

- [ ] **Step 1: Failing test** — create `internal/iterm2/iterm2_test.go`:

```go
package iterm2

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func decode(t *testing.T, seq string) Launch {
	t.Helper()
	const pre = "\x1b]1337;Custom=id=sm:"
	if !strings.HasPrefix(seq, pre) || !strings.HasSuffix(seq, "\a") {
		t.Fatalf("sequence framing wrong: %q", seq)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(seq, pre), "\a"))
	if err != nil {
		t.Fatal(err)
	}
	var l Launch
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestSequenceRoundTrips(t *testing.T) {
	in := Launch{Host: "myhost", Dir: "/x/alpha", Name: "sm-claude-s1",
		Argv: []string{"claude", "--resume", "s1"}, Tmux: true}
	got := decode(t, Sequence(in, false))
	if got.Host != in.Host || got.Dir != in.Dir || got.Name != in.Name ||
		!got.Tmux || got.Attach || len(got.Argv) != 3 || got.Argv[0] != "claude" {
		t.Errorf("round trip = %+v", got)
	}
}

func TestSequencePassthroughEnvelope(t *testing.T) {
	plain := Sequence(Launch{Host: "h", Attach: true, Name: "sm-claude-s1"}, false)
	wrapped := Sequence(Launch{Host: "h", Attach: true, Name: "sm-claude-s1"}, true)
	if !strings.HasPrefix(wrapped, "\x1bPtmux;") || !strings.HasSuffix(wrapped, "\x1b\\") {
		t.Fatalf("passthrough framing wrong: %q", wrapped)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(wrapped, "\x1bPtmux;"), "\x1b\\")
	if strings.ReplaceAll(body, "\x1b\x1b", "\x1b") != plain {
		t.Errorf("ESC doubling wrong: %q vs %q", body, plain)
	}
}
```

- [ ] **Step 2:** `go test ./internal/iterm2/ -v` → FAIL (package missing).

- [ ] **Step 3: Implement** — create `internal/iterm2/iterm2.go`:

```go
// Package iterm2 builds the OSC 1337 custom control sequences that ask a
// companion AutoLaunch script inside the user's local iTerm2 to open a
// native window ssh-ing back into this host. sm emits them to the tty; a
// terminal without the script simply ignores them.
package iterm2

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// Identity is the custom-sequence id the AutoLaunch script filters on.
const Identity = "sm"

// Launch describes one native window the local script should open. Tracked
// launches carry the tmux session name and Tmux=true so the remote agent
// lands in the usual session-form tmux; Attach=true jumps to a live one.
type Launch struct {
	Host   string   `json:"host"`
	Dir    string   `json:"dir,omitempty"`
	Name   string   `json:"name,omitempty"`
	Argv   []string `json:"argv,omitempty"`
	Tmux   bool     `json:"tmux,omitempty"`
	Attach bool     `json:"attach,omitempty"`
}

// Sequence renders the escape sequence for l. insideTmux wraps it in tmux's
// passthrough envelope (ESC Ptmux; … ESC \ with inner ESCs doubled) so it
// survives a plain tmux attach; the pane needs allow-passthrough on, which
// sm best-effort enables at startup.
func Sequence(l Launch, insideTmux bool) string {
	b, err := json.Marshal(l)
	if err != nil { // all fields are marshalable; defensive only
		b = []byte("{}")
	}
	seq := "\x1b]1337;Custom=id=" + Identity + ":" + base64.StdEncoding.EncodeToString(b) + "\a"
	if insideTmux {
		return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return seq
}
```

- [ ] **Step 4:** `go test ./internal/iterm2/ -v` then `go test ./...` → PASS.
- [ ] **Step 5: Commit** `feat(iterm2): custom control sequence builder`

---

### Task 12: ui — emit sequences; retire -CC and the plain-attach warning

**Files:**
- Modify: `internal/ui/model.go`, `cmd/sm/main.go`, `internal/tmux/tmux.go`
- Test: `internal/ui/model_test.go`, `internal/tmux/tmux_test.go`

**Interfaces:**
- Consumes: `config.Config.ITerm2SSH` (Task 10), `iterm2.Sequence`/`iterm2.Launch` (Task 11), existing `iTerm2Env`, `insideTmux`.
- Produces: `Model.iterm2Host string`; `Model.emitSeq func(string) tea.Cmd` injectable; `(m Model) iterm2Windows() bool`.

- [ ] **Step 1: Failing tests** — append to `internal/ui/model_test.go` (helper + three tests):

```go
// newITerm2Model is a window-mode model with the iTerm2 mechanism active:
// iterm2.ssh configured, LC_TERMINAL detected, outside tmux, and both
// runners trapped — iTerm2 launches must not run anything, only emit.
func newITerm2Model(t *testing.T) (Model, *[]string) {
	t.Helper()
	origIn, origIT := insideTmux, iTerm2Env
	insideTmux = func() bool { return false }
	iTerm2Env = func() bool { return true }
	t.Cleanup(func() { insideTmux, iTerm2Env = origIn, origIT })
	m := newTestModel()
	m.openIn = config.OpenInWindow
	m.iterm2Host = "myhost"
	seqs := &[]string{}
	m.emitSeq = func(seq string) tea.Cmd { *seqs = append(*seqs, seq); return nil }
	m.runCmd = func(name, dir string, args ...string) tea.Cmd {
		t.Errorf("iTerm2 mode must not runCmd: %s %v", name, args)
		return nil
	}
	m.runSilent = func(name, dir string, args ...string) tea.Cmd {
		t.Errorf("iTerm2 mode must not runSilent: %s %v", name, args)
		return nil
	}
	m.now = func() time.Time { return time.Unix(0, 1234) }
	return m, seqs
}

func decodeLaunch(t *testing.T, seq string) iterm2.Launch {
	t.Helper()
	const pre = "\x1b]1337;Custom=id=sm:"
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(seq, pre), "\a"))
	if err != nil {
		t.Fatalf("cannot decode %q: %v", seq, err)
	}
	var l iterm2.Launch
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestResumeITerm2EmitsTrackedLaunch(t *testing.T) {
	m, seqs := newITerm2Model(t)
	m.tmuxEnabled = true
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0)
	m.startResume()
	if len(*seqs) != 1 {
		t.Fatalf("want 1 sequence, got %v", *seqs)
	}
	l := decodeLaunch(t, (*seqs)[0])
	if l.Host != "myhost" || l.Dir != dir || l.Name != "sm-claude-s1" || !l.Tmux ||
		l.Attach || len(l.Argv) < 2 || l.Argv[0] != "claude" {
		t.Errorf("launch = %+v", l)
	}
}

func TestEnterLiveITerm2EmitsAttach(t *testing.T) {
	m, seqs := newITerm2Model(t)
	m.tmuxEnabled = true
	m.tmux = &fakeTmux{live: map[string]bool{"sm-claude-s1": true}}
	m.tmuxLive = map[string]bool{"sm-claude-s1": true}
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0)
	m.startResume()
	if len(*seqs) != 1 {
		t.Fatalf("want 1 sequence, got %v", *seqs)
	}
	l := decodeLaunch(t, (*seqs)[0])
	if !l.Attach || l.Name != "sm-claude-s1" || l.Host != "myhost" {
		t.Errorf("attach launch = %+v", l)
	}
}

func TestITerm2SkipsWindowTmuxPreconditions(t *testing.T) {
	origLook := tmuxLookPath
	tmuxLookPath = func() bool { return true } // irrelevant; insideTmux=false is the check
	defer func() { tmuxLookPath = origLook }()
	m, seqs := newITerm2Model(t) // outside tmux — would error without iTerm2 mechanism
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0)
	m2, _ := m.startResume()
	m = m2.(Model)
	if m.dialog == dialogError {
		t.Fatalf("iTerm2 mode must skip inside-tmux precondition, got %q", m.errText)
	}
	if len(*seqs) != 1 {
		t.Errorf("expected an emitted sequence, got %v", *seqs)
	}
}
```

Add imports `"encoding/base64"`, `"encoding/json"`, and the iterm2 package to model_test.go's import block. DELETE `TestWindowModePlainClientOniTerm2Warns` (superseded) and `TestCCFlag` in `internal/tmux/tmux_test.go`.

- [ ] **Step 2:** run the three new tests → FAIL (missing fields).

- [ ] **Step 3: Implement** in `internal/ui/model.go`:
  - Struct fields (after `openIn`): `iterm2Host string` — and next to `runSilent`: `emitSeq func(string) tea.Cmd`.
  - `New()` literal: `iterm2Host:   cfg.ITerm2SSH,` and `emitSeq:      emitEscape,`.
  - DELETE the plain-attach warning block in `New()` (the one setting `errText` about "control mode") and the `plainTmuxClient` var.
  - In `New()`, after the openIn-downgrade block, best-effort passthrough (so sequences survive a plain tmux attach):
    ```go
    	if ret.openIn == config.OpenInWindow && ret.iterm2Host != "" && iTerm2Env() && insideTmux() {
    		_ = exec.Command("tmux", "set-option", "-p", "allow-passthrough", "on").Run()
    	}
    ```
  - Add:
    ```go
    // emitEscape writes a control sequence to stderr — the tty, not Bubble
    // Tea's stdout frame — same reasoning as ringBell.
    func emitEscape(seq string) tea.Cmd {
    	return func() tea.Msg {
    		fmt.Fprint(os.Stderr, seq)
    		return nil
    	}
    }

    // iterm2Windows reports whether window-mode launches go through the
    // local iTerm2 AutoLaunch script instead of tmux windows: explicit
    // config opt-in plus a detected iTerm2.
    func (m Model) iterm2Windows() bool {
    	return m.openIn == config.OpenInWindow && m.iterm2Host != "" && iTerm2Env()
    }
    ```
  - `launchErr`: change the window-mode precondition gate to `if m.openIn == config.OpenInWindow && !m.iterm2Windows() {` (iTerm2 mode needs neither tmux on PATH nor $TMUX).
  - `runAgentCmd` resume branch — inside the `if m.openIn == config.OpenInWindow {` block, before the runSilent path:
    ```go
    		if m.iterm2Windows() {
    			l := iterm2.Launch{Host: m.iterm2Host, Dir: cwd, Argv: append([]string{name}, args...)}
    			if m.tmuxEnabled {
    				l.Name, l.Tmux = sess, true
    			}
    			return m.emitSeq(iterm2.Sequence(l, insideTmux()))
    		}
    ```
  - `runAgentCmd` new-session branch — same shape with `tmux.PendingName(string(p.Agent()), m.now().UnixNano())` as the tracked name.
  - `attachLiveCmd` — first line:
    ```go
    	if m.iterm2Windows() {
    		return m.emitSeq(iterm2.Sequence(iterm2.Launch{Host: m.iterm2Host, Name: sess, Attach: true}, insideTmux()))
    	}
    ```
  - Import the iterm2 package.
  - `cmd/sm/main.go`: wrap gate becomes
    ```go
    	if cfg.OpenIn == config.OpenInWindow && os.Getenv("TMUX") == "" &&
    		!(cfg.ITerm2SSH != "" && os.Getenv("LC_TERMINAL") == "iTerm2") {
    ```
    and the argv drops CCFlag: `argv := append([]string{"tmux"}, tmux.SelfWrapArgs(selfCmd, cwd, exists, winID)...)`.
  - `internal/tmux/tmux.go`: DELETE `CCFlag`.

- [ ] **Step 4:** `go test ./...` → PASS; `gofmt -l cmd internal` → empty.
- [ ] **Step 5: Commit** `feat(ui): emit iTerm2 custom sequences for native-window launches; retire -CC`

---

### Task 13: local script, installer, README

**Files:**
- Create: `scripts/iterm2/sm_open_window.py`
- Create: `scripts/install-iterm2.sh`
- Modify: `README.md`

- [ ] **Step 1: Create `scripts/iterm2/sm_open_window.py`:**

```python
#!/usr/bin/env python3
"""sm → iTerm2 bridge.

Listens for sm's custom control sequences (OSC 1337 Custom=id=sm:<base64>)
and opens a native iTerm2 window that ssh-es back into the host to run the
agent. Terminal output is untrusted input: every payload field is validated
against a strict pattern before anything runs, and the only command shape
ever executed is `ssh -t <host> <cd+tmux+agent>`.

Install: ~/Library/Application Support/iTerm2/Scripts/AutoLaunch/ and enable
Settings → General → Magic → "Enable Python API".
"""
import base64
import json
import re
import shlex

import iterm2

HOST_RE = re.compile(r"^[A-Za-z0-9._@-]{1,128}$")
NAME_RE = re.compile(r"^sm(-[a-z0-9]+)+$")
AGENTS = ("claude", "codex")
ARG_RE = re.compile(r"^[A-Za-z0-9._/=@-]{1,256}$")

windows = {}


def remote_command(spec):
    """Build the validated remote command, or None to reject the payload."""
    host = spec.get("host", "")
    name = spec.get("name", "")
    dir_ = spec.get("dir", "")
    argv = spec.get("argv") or []
    if not HOST_RE.match(host):
        return None, None, None
    if spec.get("attach"):
        if not NAME_RE.match(name):
            return None, None, None
        return host, name, "exec tmux attach-session -t " + shlex.quote("=" + name)
    if not argv or argv[0] not in AGENTS or not all(ARG_RE.match(a) for a in argv):
        return None, None, None
    inner = " ".join(shlex.quote(a) for a in argv)
    if spec.get("tmux"):
        if not NAME_RE.match(name):
            return None, None, None
        cmd = "cd {d} && exec tmux new-session -A -s {n} -c {d} {i}".format(
            d=shlex.quote(dir_), n=shlex.quote(name), i=inner)
    else:
        cmd = "cd {d} && exec {i}".format(d=shlex.quote(dir_), i=inner)
    return host, name or inner, cmd


async def handle(connection, payload):
    spec = json.loads(base64.b64decode(payload))
    host, key, cmd = remote_command(spec)
    if not cmd:
        return
    old = windows.get(key)
    if old is not None:
        try:
            await old.async_activate()
            return
        except Exception:
            windows.pop(key, None)
    ssh = "/usr/bin/env ssh -t {h} {c}".format(h=shlex.quote(host), c=shlex.quote(cmd))
    win = await iterm2.Window.async_create(connection, command=ssh)
    if win is not None:
        windows[key] = win


async def main(connection):
    async with iterm2.CustomControlSequenceMonitor(
            connection, "sm", r"^(.+)$") as mon:
        while True:
            match = await mon.async_get()
            try:
                await handle(connection, match.group(1))
            except Exception:
                pass  # a bad payload must never kill the listener


iterm2.run_forever(main)
```

- [ ] **Step 2: Create `scripts/install-iterm2.sh`:**

```sh
#!/bin/sh
# One-command installer for sm's iTerm2 native-window bridge (run on the Mac):
#   curl -fsSL https://raw.githubusercontent.com/dukechain2333/ai-sessions-manager/main/scripts/install-iterm2.sh | sh
set -e
DIR="$HOME/Library/Application Support/iTerm2/Scripts/AutoLaunch"
URL="https://raw.githubusercontent.com/dukechain2333/ai-sessions-manager/main/scripts/iterm2/sm_open_window.py"
mkdir -p "$DIR"
curl -fsSL "$URL" -o "$DIR/sm_open_window.py"
echo "Installed $DIR/sm_open_window.py"
echo
echo "Two manual steps in iTerm2:"
echo "  1. Settings > General > Magic > check 'Enable Python API'"
echo "  2. Scripts > AutoLaunch > sm_open_window.py (or restart iTerm2)."
echo "     First run offers to download the iTerm2 Python runtime - accept."
echo
echo "Then on the remote host, set in ~/.config/sm/config.json:"
echo '  "open_in": "window",'
echo '  "iterm2": { "ssh": "<how this Mac sshes there, e.g. myserver>" }'
```

`chmod +x scripts/install-iterm2.sh`.

- [ ] **Step 3: README** — in the `open_in` bullet, replace the iTerm2 paragraph (added in Task 9's amendment) with a pointer, and add a dedicated section after the Configuration section (before `### tmux integration`):

Replace the `**iTerm2 users get real OS windows:** …` sentence block with:

```markdown
  **iTerm2 users can get real OS windows** — see
  [iTerm2 native windows](#iterm2-native-windows-macos).
```

New section:

```markdown
### iTerm2 native windows (macOS)

With one small companion script, `open_in: "window"` opens every resume/new
session as a **genuine iTerm2 window** — `sm` itself stays exactly where you
ran it, even over SSH.

How it works: pressing `enter` makes `sm` write an invisible [custom control
sequence](https://iterm2.com/python-api/customcontrol.html) to your
terminal. An AutoLaunch script inside your local iTerm2 picks it up and
opens a native window that runs
`ssh -t <host> "cd <dir> && tmux new-session -A -s sm-<agent>-<id8> <agent> --resume <id>"`,
so the agent lands in the same tracked tmux session `sm` already knows how
to mark (●), kill (`x`), and re-enter. No tmux needed around `sm` itself.

**Install (on the Mac, one command):**

```sh
curl -fsSL https://raw.githubusercontent.com/dukechain2333/ai-sessions-manager/main/scripts/install-iterm2.sh | sh
```

Then, in iTerm2: *Settings → General → Magic → Enable Python API*, and start
the script under *Scripts → AutoLaunch → sm_open_window.py* (auto-starts on
the next iTerm2 launch; the first run offers to install iTerm2's Python
runtime — accept it).

**Configure (on the remote host)** in `~/.config/sm/config.json`:

```json
{
  "open_in": "window",
  "iterm2": { "ssh": "myserver" }
}
```

`iterm2.ssh` is whatever you type after `ssh` on the Mac to reach the host
(alias from `~/.ssh/config`, hostname, or IP) — the new window dials a fresh
connection, so key-based login should already work.

**Troubleshooting** — pressing `enter` does nothing:
- The AutoLaunch script isn't running (iTerm2 *Scripts → Manage → Console*
  shows it) or the Python API checkbox is off.
- `iterm2.ssh` missing from config, or `$LC_TERMINAL` not reaching the host
  (check `echo $LC_TERMINAL` there; your ssh config must not strip `LC_*`).
- Running `sm` inside a tmux attach: `sm` auto-enables pane passthrough, but
  a tmux older than 3.3 lacks `allow-passthrough` — run `sm` outside tmux.

Security note: the script treats terminal output as untrusted — payloads are
validated against strict patterns and the only thing it will ever run is
`ssh -t <host>` with a `cd`+`tmux`+`claude|codex` command line.
```

- [ ] **Step 4:** `go test ./...` (sanity) → PASS. `sh -n scripts/install-iterm2.sh` → clean. `python3 -m py_compile scripts/iterm2/sm_open_window.py` → clean (compile only; the iterm2 module exists only on the Mac).
- [ ] **Step 5: Commit** `feat(iterm2): AutoLaunch bridge script, one-command installer, README`
