# Warp Native Windows Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `open_in: "window"` opens native Warp windows — locally when sm runs inside Warp, and over SSH via the existing `sm ssh` bridge — as the third backend beside iTerm2 and Ghostty.

**Architecture:** A new `internal/warp` package mirrors `internal/ghostty`: an `Opener` whose `Open(key, line)` writes a Warp Tab Config TOML (fixed file `sm.toml`, atomic overwrite per launch) and triggers `warp://tab_config/sm?new_window=true` through `open`/`xdg-open`. The UI gains a `warpEnv`/`warpWindows()` pair mirroring ghostty's, `cmd/sm/main.go` mirrors the probe in its self-wrap exemption, and `sshmain.go`'s hardcoded `ghostty.New()` becomes a two-terminal opener selection. Spec: `docs/superpowers/specs/2026-07-30-warp-native-windows-design.md`.

**Tech Stack:** Go stdlib only. TOML is emitted by a hand-rolled ~20-line escaper — do NOT add a TOML dependency.

## Global Constraints

- Branch: `feat/warp-native-windows` off `main`. Strictly additive diff — no changes to iTerm2/Ghostty/bridge behavior, config schema, or the settings dialog.
- No new entries in `go.mod`.
- Error prefix for the new launcher: `warp window:` (matches ghostty's `ghostty window:` idiom).
- Every existing ui test keeps passing **unmodified except** the `TestMain` pin added in Task 4 — the dev machine's terminal IS Warp, so an unpinned `warpEnv` makes the suite environment-dependent (the f6d965e rule).
- Warp Stable only: URI scheme `warp://`, dirs `~/.warp/tab_configs` (macOS) and `${XDG_DATA_HOME:-~/.local/share}/warp-terminal/tab_configs` (Linux). Preview (`warppreview://`, `~/.warp-preview/`) is out of scope.
- All commits end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: `internal/warp` — package, detection, constructor

**Files:**
- Create: `internal/warp/warp.go`
- Create: `internal/warp/warp_test.go`

**Interfaces:**
- Consumes: nothing from sm (stdlib only).
- Produces: `warp.New() (*Opener, error)`; struct `Opener{goos string, dir string, run func(name string, args ...string) (string, error)}` (fields unexported; tests in-package construct it directly, like `ghostty_test.go`'s `opener()` helper).

- [ ] **Step 1: Write the failing tests**

`internal/warp/warp_test.go`:

```go
package warp

import (
	"strings"
	"testing"
)

func TestNewRejectsNonWarp(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("WARP_TERMINAL_SESSION_UUID", "")
	if _, err := New(); err == nil || !strings.Contains(err.Error(), "not Warp") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewAcceptsWarpEnv(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	t.Setenv("WARP_TERMINAL_SESSION_UUID", "")
	o, err := New()
	if err != nil {
		t.Skipf("environment cannot host Warp windows: %v", err) // linux without xdg-open
	}
	if o.dir == "" || !strings.Contains(o.dir, "tab_configs") {
		t.Errorf("dir = %q, want a tab_configs path", o.dir)
	}
}

// tmux rewrites TERM_PROGRAM but panes inherit WARP_*: the UUID marker
// alone must be enough.
func TestNewAcceptsTmuxInheritedMarker(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "tmux")
	t.Setenv("WARP_TERMINAL_SESSION_UUID", "abc-123")
	if _, err := New(); err != nil && strings.Contains(err.Error(), "not Warp") {
		t.Fatalf("UUID marker not honored: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/warp/`
Expected: FAIL — package does not exist / `New` undefined.

- [ ] **Step 3: Write the implementation**

`internal/warp/warp.go`:

```go
// Package warp opens native Warp windows and runs a command in them. Warp
// has no scripting dictionary and no local-window CLI (oz drives cloud
// agents only); the one programmatic "open a window and run a command"
// path is a Tab Config file whose commands run in the new pane, delivered
// through the warp://tab_config URI. Used by the `sm ssh` helper on the
// desktop and by a local window-mode sm.
package warp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Opener opens native Warp windows. Unlike Ghostty's it holds no dedupe
// state: Warp hands back no window handle, so every launch opens a fresh
// window (duplicate tracked launches still collapse in the target
// machine's tmux new-session -A).
type Opener struct {
	goos string
	dir  string                                            // tab_configs dir
	run  func(name string, args ...string) (string, error) // injected for tests
}

// New returns an Opener after checking this process can actually reach a
// Warp: TERM_PROGRAM must say so — or, inside a tmux attach (which
// rewrites TERM_PROGRAM to "tmux"), the inherited
// WARP_TERMINAL_SESSION_UUID — and on Linux xdg-open must be on PATH to
// deliver the warp:// URI.
func New() (*Opener, error) {
	if os.Getenv("TERM_PROGRAM") != "WarpTerminal" && os.Getenv("WARP_TERMINAL_SESSION_UUID") == "" {
		return nil, errors.New("this terminal is not Warp")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	o := &Opener{goos: runtime.GOOS, run: runOut}
	switch o.goos {
	case "darwin":
		o.dir = filepath.Join(home, ".warp", "tab_configs")
		return o, nil
	case "linux":
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return nil, errors.New("xdg-open not found on PATH")
		}
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		o.dir = filepath.Join(data, "warp-terminal", "tab_configs")
		return o, nil
	}
	return nil, errors.New("Warp windows are not supported on " + o.goos)
}

// runOut runs a command with a hang guard and returns its stdout; stderr
// is folded into the returned error. Same idiom as ghostty's.
func runOut(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	err := c.Run()
	if err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			err = errors.New(err.Error() + ": " + msg)
		}
	}
	return out.String(), err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/warp/`
Expected: PASS (TestNewAcceptsWarpEnv may SKIP on Linux without xdg-open; that is fine).

- [ ] **Step 5: Commit**

```bash
git add internal/warp/
git commit -m "feat(warp): package skeleton — Warp detection and constructor

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: `internal/warp` — Tab Config rendering and TOML escaping

**Files:**
- Modify: `internal/warp/warp.go` (append)
- Modify: `internal/warp/warp_test.go` (append)

**Interfaces:**
- Consumes: nothing new.
- Produces: `renderTabConfig(line string) string`, `tomlString(s string) string` (both unexported; consumed by Task 3's `Open`).

- [ ] **Step 1: Write the failing tests** (append to `warp_test.go`)

```go
func TestRenderTabConfig(t *testing.T) {
	got := renderTabConfig(`cd '/d' && exec 'claude' '--resume' 'x1'`)
	want := "name = \"sm\"\n\n[[panes]]\nid = \"main\"\ntype = \"terminal\"\ncommands = [\"cd '/d' && exec 'claude' '--resume' 'x1'\"]\n"
	if got != want {
		t.Errorf("renderTabConfig:\n got %q\nwant %q", got, want)
	}
}

func TestTomlStringEscapes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`plain`, `"plain"`},
		{`say "hi"`, `"say \"hi\""`},
		{`back\slash`, `"back\\slash"`},
		{"tab\there", `"tab\there"`},
		{"nl\nhere", `"nl\nhere"`},
		{"bell\x07here", `"bellhere"`},
		{"del\x7fhere", `"delhere"`},
	}
	for _, c := range cases {
		if got := tomlString(c.in); got != c.want {
			t.Errorf("tomlString(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/warp/`
Expected: FAIL — `renderTabConfig` / `tomlString` undefined.

- [ ] **Step 3: Write the implementation** (append to `warp.go`)

```go
// renderTabConfig builds the Tab Config that runs line in a fresh
// terminal pane. No directory key: line already carries its own cd
// (bridge.Line's local form) or is a complete ssh command. Verified live
// that Warp accepts a directory-less pane (Task 3 step 6); if a Warp
// update regresses that, add `directory = "~"` here — the cd in line
// still decides the real working dir.
func renderTabConfig(line string) string {
	return "name = \"sm\"\n\n[[panes]]\nid = \"main\"\ntype = \"terminal\"\ncommands = [" + tomlString(line) + "]\n"
}

// tomlString renders s as a TOML basic string: backslash and double
// quote escaped per the TOML spec; \t \n \r get their shorthand escapes.
// Other control characters are stripped, not escaped — they can never
// legitimately appear in a launch line (bridge.Line rejects them
// upstream), and \u-escaping would deliver raw control bytes into a
// shell command line. (Ruling 2026-07-30: stripping governs.)
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			// stripped — see doc comment
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/warp/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/warp/
git commit -m "feat(warp): Tab Config rendering with TOML basic-string escaping

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: `internal/warp` — `Open`: atomic config write + URI delivery

**Files:**
- Modify: `internal/warp/warp.go` (append)
- Modify: `internal/warp/warp_test.go` (append)

**Interfaces:**
- Consumes: `renderTabConfig` (Task 2).
- Produces: `func (o *Opener) Open(key, line string) error` — the shape `bridge.Handler` and `Model.warpOpen` need (Tasks 4 and 6).

- [ ] **Step 1: Write the failing tests** (append to `warp_test.go`; add `"os"`, `"path/filepath"` to test imports)

```go
type call struct {
	name string
	args []string
}

func opener(goos, dir string, run func(name string, args ...string) (string, error)) *Opener {
	return &Opener{goos: goos, dir: dir, run: run}
}

func TestOpenWritesConfigAndDeliversURI(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tab_configs") // must not pre-exist
	var got call
	o := opener("darwin", dir, func(name string, args ...string) (string, error) {
		got = call{name, args}
		return "", nil
	})
	line := `cd '/d' && exec 'claude'`
	if err := o.Open("ignored-key", line); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "sm.toml"))
	if err != nil {
		t.Fatalf("sm.toml not written: %v", err)
	}
	if want := renderTabConfig(line); string(b) != want {
		t.Errorf("sm.toml = %q, want %q", b, want)
	}
	if got.name != "open" || len(got.args) != 1 || got.args[0] != "warp://tab_config/sm?new_window=true" {
		t.Errorf("ran %q %q", got.name, got.args)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "sm-*"))
	if len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestOpenLinuxUsesXdgOpen(t *testing.T) {
	var got call
	o := opener("linux", filepath.Join(t.TempDir(), "tab_configs"), func(name string, args ...string) (string, error) {
		got = call{name, args}
		return "", nil
	})
	if err := o.Open("k", "line"); err != nil {
		t.Fatal(err)
	}
	if got.name != "xdg-open" || got.args[0] != "warp://tab_config/sm?new_window=true" {
		t.Errorf("ran %q %q", got.name, got.args)
	}
}

func TestOpenSurfacesOpenerError(t *testing.T) {
	o := opener("darwin", filepath.Join(t.TempDir(), "tc"), func(string, ...string) (string, error) {
		return "", os.ErrNotExist
	})
	if err := o.Open("k", "line"); err == nil || !strings.Contains(err.Error(), "warp window:") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/warp/`
Expected: FAIL — `Open` undefined.

- [ ] **Step 3: Write the implementation** (append to `warp.go`)

```go
// Open runs line in a native Warp window: land the Tab Config, then
// deliver the URI. key is accepted for the bridge.Handler shape and
// ignored — no window handle comes back, so there is nothing to refocus.
// Past a successful open/xdg-open exit the launch is fire-and-forget.
func (o *Opener) Open(_, line string) error {
	if err := o.writeConfig(renderTabConfig(line)); err != nil {
		return errors.New("warp window: " + err.Error())
	}
	opener := "open"
	if o.goos == "linux" {
		opener = "xdg-open"
	}
	if _, err := o.run(opener, "warp://tab_config/sm?new_window=true"); err != nil {
		return errors.New("warp window: " + err.Error())
	}
	return nil
}

// writeConfig lands content at <dir>/sm.toml via temp-file-plus-rename in
// the same directory, so Warp can never read a half-written file.
func (o *Opener) writeConfig(content string) error {
	if err := os.MkdirAll(o.dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(o.dir, "sm-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(o.dir, "sm.toml"))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/warp/`
Expected: PASS.

- [ ] **Step 5: Live check on this machine — the spec's directory-less-pane note**

The spike's TOML carried a `directory` key; `renderTabConfig` emits none.
Verify Warp accepts it (this dev machine runs Warp Stable):

```bash
cat > ~/.warp/tab_configs/sm.toml <<'EOF'
name = "sm"

[[panes]]
id = "main"
type = "terminal"
commands = ["cd '/Users/anji/Desktop/ai-sessions-manager' && date > /tmp/sm-dirless-marker && echo sm-dirless-ok"]
EOF
open "warp://tab_config/sm?new_window=true"
```

Wait ~3s, then: `ls -l /tmp/sm-dirless-marker`

- Marker exists → directory-less panes work; clean up
  (`rm ~/.warp/tab_configs/sm.toml /tmp/sm-dirless-marker`, close the
  Warp window) and continue.
- Marker missing → add `directory = "~"\n` to `renderTabConfig`'s pane
  block, update `TestRenderTabConfig`'s `want`, update the code comment,
  re-run step 4, and note the finding in the commit message.

- [ ] **Step 6: Commit**

```bash
git add internal/warp/
git commit -m "feat(warp): Open — atomic Tab Config write + warp:// URI delivery

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: UI wiring — `warpEnv`, `warpWindows`, dispatch, injected runner

**Files:**
- Modify: `internal/ui/model.go` (import `internal/warp`; struct field near `ghosttyOpen` at ~`model.go:146`; `New()` injection at ~`model.go:220`; predicates at ~`model.go:303-332`; helpers at ~`model.go:336-357`; env var at ~`model.go:403`)
- Modify: `internal/ui/model_test.go` (`TestMain` pin at ~`model_test.go:32`; two new tests beside `TestGhosttyLocalOpensWindow` at ~`model_test.go:1528`)

**Interfaces:**
- Consumes: `warp.New() (*warp.Opener, error)`, `(*warp.Opener).Open(key, line string) error` (Tasks 1/3); `bridge.Line(l iterm2.Launch, dest string, sshArgs []string) (key, line string, err error)` (existing).
- Produces: `Model.warpOpen func(l iterm2.Launch) tea.Cmd` (test seam), `warpEnv` package var (test seam), `Model.warpWindows() bool`.

- [ ] **Step 1: Write the failing tests**

In `TestMain` (`model_test.go:32`), directly under the `ghosttyEnv` pin, add:

```go
	warpEnv = func() bool { return false }
```

Beside the ghostty tests (~line 1528), add:

```go
// A local window-mode sm inside Warp opens windows itself — no ssh, no
// escapes, host always empty.
func TestWarpLocalOpensWindow(t *testing.T) {
	origIn, origWp, origSSH := insideTmux, warpEnv, overSSH
	insideTmux = func() bool { return false }
	warpEnv = func() bool { return true }
	overSSH = func() bool { return false }
	t.Cleanup(func() { insideTmux, warpEnv, overSSH = origIn, origWp, origSSH })
	m := newTestModel()
	m.openIn = config.OpenInWindow
	m.tmuxEnabled = true
	launches := &[]iterm2.Launch{}
	m.warpOpen = func(l iterm2.Launch) tea.Cmd {
		*launches = append(*launches, l)
		return nil
	}
	m.ghosttyOpen = func(l iterm2.Launch) tea.Cmd {
		t.Errorf("warp mode must not route to ghostty: %+v", l)
		return nil
	}
	m.emitSeq = func(seq string) tea.Cmd {
		t.Errorf("warp mode must not emit escapes: %q", seq)
		return nil
	}
	m.runSilent = func(name, dir string, args ...string) tea.Cmd {
		t.Errorf("warp mode must not runSilent: %s %v", name, args)
		return nil
	}
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0)
	m.startResume()
	if len(*launches) != 1 {
		t.Fatalf("want 1 launch, got %v", *launches)
	}
	if l := (*launches)[0]; l.Host != "" || l.Name != "sm-claude-s1" || !l.Tmux {
		t.Errorf("launch = %+v, want empty host", l)
	}
}

// A forwarded Warp env over ssh must not count: windows can only open
// client-side, which is the bridge's job.
func TestWarpOverSSHStaysOff(t *testing.T) {
	origWp, origSSH, origIT := warpEnv, overSSH, iTerm2Env
	warpEnv = func() bool { return true }
	overSSH = func() bool { return true }
	iTerm2Env = func() bool { return false }
	t.Cleanup(func() { warpEnv, overSSH, iTerm2Env = origWp, origSSH, origIT })
	m := newTestModel()
	m.openIn = config.OpenInWindow
	if m.nativeWindows() {
		t.Error("forwarded Warp env over ssh must not enable native windows")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/`
Expected: FAIL to compile — `warpEnv`, `m.warpOpen` undefined.

- [ ] **Step 3: Implement the wiring in `internal/ui/model.go`**

Import: add `"github.com/dukechain2333/ai-sessions-manager/internal/warp"`.

Struct (directly under `ghosttyOpen func(l iterm2.Launch) tea.Cmd`):

```go
	warpOpen    func(l iterm2.Launch) tea.Cmd
```

`New()` (directly under `ghosttyOpen:  ghosttyOpenCmd,`):

```go
		warpOpen:     warpOpenCmd,
```

Under `ghosttyWindows()` (~`model.go:306`):

```go
// warpWindows reports whether window-mode launches open native Warp
// windows on this same machine. Over ssh a forwarded Warp env must not
// count — windows can only open client-side, which is the bridge's job.
func (m Model) warpWindows() bool {
	return m.openIn == config.OpenInWindow && !overSSH() && warpEnv()
}
```

`nativeWindows()` (~`model.go:312`) becomes:

```go
	return m.bridgeWindows() || m.ghosttyWindows() || m.warpWindows() || m.iterm2Windows()
```

`openWindowCmd` (~`model.go:319`) gains one case above the default:

```go
	case m.warpWindows():
		return m.warpOpen(l)
```

Under `ghosttyOpenCmd` (~`model.go:357`):

```go
// localWarp builds the process-wide local Warp opener once.
var localWarp = sync.OnceValues(func() (*warp.Opener, error) { return warp.New() })

// warpOpenCmd opens l in a native Warp window on this machine. The empty
// destination selects bridge.Line's local form — run directly, no ssh
// and no PATH prepend (a fresh local shell already has the user's PATH).
func warpOpenCmd(l iterm2.Launch) tea.Cmd {
	return func() tea.Msg {
		key, line, err := bridge.Line(l, "", nil)
		if err == nil {
			var op *warp.Opener
			if op, err = localWarp(); err == nil {
				err = op.Open(key, line)
			}
		}
		return silentDoneMsg{err: err}
	}
}
```

Under `ghosttyEnv` (~`model.go:403`):

```go
// warpEnv reports whether the terminal is Warp AND this machine can open
// its windows (Linux needs xdg-open to deliver the warp:// URI). tmux
// rewrites TERM_PROGRAM, so the inherited WARP_TERMINAL_SESSION_UUID is
// the fallback marker. Only trusted for local launches — warpWindows
// pairs it with !overSSH(). Overridable in tests.
var warpEnv = func() bool {
	if os.Getenv("TERM_PROGRAM") != "WarpTerminal" && os.Getenv("WARP_TERMINAL_SESSION_UUID") == "" {
		return false
	}
	if runtime.GOOS == "linux" {
		_, err := exec.LookPath("xdg-open")
		return err == nil
	}
	return true
}
```

- [ ] **Step 4: Run the full ui suite**

Run: `go test ./internal/ui/`
Expected: PASS — including every pre-existing test (the `TestMain` pin keeps them Warp-host-independent).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/
git commit -m "feat(ui): route window-mode launches to native Warp windows

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: `cmd/sm/main.go` — self-wrap exemption probe

**Files:**
- Modify: `cmd/sm/main.go:65-78` (comment block + probes)

**Interfaces:**
- Consumes: env vars only (the probes deliberately mirror ui's inline — see the comment at `main.go:66-71`).
- Produces: nothing new.

- [ ] **Step 1: Edit the probe block**

Replace the two comment lines at `main.go:66-70` ("… — via the sm / ssh reverse-tunnel bridge, a local Ghostty, or the iTerm2 escapes. / The probes mirror ui's exactly (bridge.Socket, the triple ssh-marker / check, the Linux ghostty-binary requirement) …") so they read:

```go
	// Native window launchers are exempt from the wrap: sm stays in this
	// terminal and launches open real OS windows client-side — via the sm
	// ssh reverse-tunnel bridge, a local Ghostty or Warp, or the iTerm2
	// escapes. The probes mirror ui's exactly (bridge.Socket, the triple
	// ssh-marker check, the Linux opener-binary requirements) so the wrap
	// decision and the in-app launcher choice can never disagree.
```

After the `ghosttyWin` block (`main.go:72-76`), add:

```go
	warpWin := (os.Getenv("TERM_PROGRAM") == "WarpTerminal" || os.Getenv("WARP_TERMINAL_SESSION_UUID") != "") && !sshEnv
	if warpWin && runtime.GOOS == "linux" {
		_, err := exec.LookPath("xdg-open")
		warpWin = err == nil
	}
```

And extend `nativeWin` (`main.go:77`):

```go
	nativeWin := bridge.Socket() != "" || ghosttyWin || warpWin ||
		(os.Getenv("LC_TERMINAL") == "iTerm2" && (cfg.ITerm2SSH != "" || !sshEnv))
```

- [ ] **Step 2: Build and run the full suite**

Run: `go build ./... && go test ./...`
Expected: builds; all tests PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/sm/main.go
git commit -m "feat(main): exempt Warp from the window-mode tmux self-wrap

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: `sm ssh` — opener selection (Ghostty → Warp → plain ssh)

**Files:**
- Modify: `internal/bridge/sshmain.go` (usage text ~line 17-24; opener pick ~line 42-48; ready message ~line 90)
- Modify: `internal/bridge/bridge.go:1-13` (package comment: "opens local Ghostty windows" → both terminals)
- Test: `internal/bridge/sshmain_test.go` (create)

**Interfaces:**
- Consumes: `ghostty.New()`, `warp.New()`, both `Open(key, line string) error`.
- Produces: `desktopOpener() (Handler, string, error)` (unexported; `Handler` already exists at `bridge.go:183`).

- [ ] **Step 1: Write the failing test**

`internal/bridge/sshmain_test.go`:

```go
package bridge

import (
	"runtime"
	"strings"
	"testing"
)

func TestDesktopOpenerSelection(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "")
	t.Setenv("WARP_TERMINAL_SESSION_UUID", "")
	if _, _, err := desktopOpener(); err == nil || !strings.Contains(err.Error(), "Ghostty or Warp") {
		t.Fatalf("no-terminal err = %v", err)
	}
	if runtime.GOOS != "darwin" {
		// Positive cases need the opener binaries Linux CI lacks.
		t.Skip("positive selection cases are darwin-only")
	}
	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	if open, term, err := desktopOpener(); err != nil || term != "Warp" || open == nil {
		t.Fatalf("warp pick: term=%q err=%v", term, err)
	}
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("WARP_TERMINAL_SESSION_UUID", "")
	if open, term, err := desktopOpener(); err != nil || term != "Ghostty" || open == nil {
		t.Fatalf("ghostty pick: term=%q err=%v", term, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bridge/`
Expected: FAIL to compile — `desktopOpener` undefined.

- [ ] **Step 3: Implement**

In `sshmain.go`, add the import
`"github.com/dukechain2333/ai-sessions-manager/internal/warp"` and, above
`SSHMain`:

```go
// desktopOpener picks the native-window opener for the terminal this
// helper runs in: Ghostty first, then Warp. The returned name feeds
// user-facing messages.
func desktopOpener() (Handler, string, error) {
	if g, err := ghostty.New(); err == nil {
		return g.Open, "Ghostty", nil
	}
	if w, err := warp.New(); err == nil {
		return w.Open, "Warp", nil
	}
	return nil, "", errors.New("this terminal is not Ghostty or Warp")
}
```

(`errors` joins the imports.) In `SSHMain`, replace

```go
	opener, err := ghostty.New()
```

with

```go
	open, term, err := desktopOpener()
```

pass `open` to Serve — `go Serve(ln, dest, extra, open, logf)`. The three degrade-to-plain-ssh branches keep their current shape; the first one's `%v` now prints desktopOpener's error. Make the ready line name the picked terminal:

```go
	fmt.Fprintf(os.Stderr, "sm ssh: window bridge ready — window-mode launches on %s open %s windows here\n", dest, term)
```

In the `sshUsage` string, change "opens a native / Ghostty window on THIS machine" to "opens a native / terminal window (Ghostty or Warp) on THIS machine". In `bridge.go`'s package comment (line 4), change "opens local Ghostty windows" to "opens local terminal windows (Ghostty or Warp)".

- [ ] **Step 4: Run the bridge suite**

Run: `go test ./internal/bridge/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bridge/
git commit -m "feat(bridge): sm ssh picks its opener — Ghostty, then Warp

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: Documentation

**Files:**
- Modify: `docs/native-windows.md` (intro line 6; shared bullet lines 17-18; new section after the Ghostty troubleshooting list, before `## Security model`)
- Modify: `README.md:41`, `README.md:114`, `README.md:216` (table), `README.md:225`, `README.md:270`

**Interfaces:** none — prose only.

- [ ] **Step 1: `docs/native-windows.md`**

Line 6: "covers the two supported terminals in depth" → "covers the three
supported terminals in depth".

Lines 17-18, the shared-behavior bullet, becomes:

```markdown
- Repeating a launch whose window is still open focuses that window
  instead of opening a duplicate (iTerm2, and Ghostty on macOS; Warp
  opens a fresh window each time — see its section).
```

Insert before `## Security model`:

```markdown
---

## Warp (macOS & Linux)

Same behavior again, third plumbing: Warp has no scripting dictionary and
no local-window CLI, so `sm` writes a **Tab Config** file —
`~/.warp/tab_configs/sm.toml` (Linux:
`~/.local/share/warp-terminal/tab_configs/`), rewritten on every launch —
and asks Warp to open it via the `warp://tab_config/sm?new_window=true`
URI.

### Local (sm and Warp on the same machine)

Nothing to install. Set the mode and you're done:

```json
{ "open_in": "window" }
```

### Over SSH

Identical to the Ghostty flow: install `sm` on the desktop, connect with
`sm ssh myserver`, and window-mode launches on the server open native
Warp windows on the desktop that dial back. Requirements and the bridge
troubleshooting list in the Ghostty section above apply unchanged.

### Warp-specific notes

- **No window refocus:** repeating a launch opens another window instead
  of focusing the still-open one — Warp hands back no window handle.
  Tracked launches still collapse into the same tmux session, so the
  duplicate windows are mirrors, not duplicate agents.
- A Tab Config named **`sm`** appears in Warp's pickers (command palette,
  the `+` tab menu). It is sm's launch vehicle; deleting it is harmless —
  the next launch recreates it.
- Once the URI is handed off, errors are only visible inside Warp — the
  launch is fire-and-forget, like the iTerm2 escapes.
- **Warp Preview is unsupported** (it registers `warppreview://` and reads
  `~/.warp-preview/`); use Warp Stable.
- Linux: some Wayland setups break `xdg-open` from inside Warp
  (compositor connection refused — a known Warp issue); launches then
  fail silently.
```

- [ ] **Step 2: `README.md`**

- Line 41: "launches open native iTerm2 or Ghostty windows" → "launches
  open native iTerm2, Ghostty, or Warp windows".
- Line 114: `sm ssh HOST             # ssh + Ghostty window bridge, see "Real OS windows"` →
  `sm ssh HOST             # ssh + native window bridge (Ghostty/Warp), see "Real OS windows"`.
- After the Ghostty row (line 216) add:

```markdown
| **Warp** (macOS & Linux) | native window | native window on the desktop | none locally; over SSH just connect with **`sm ssh <host>`** |
```

- Line 225: "works as-is for local iTerm2, local Ghostty, Ghostty over
  `sm ssh`, and" → "works as-is for local iTerm2, local Ghostty, local
  Warp, Ghostty or Warp over `sm ssh`, and".
- Line 270: "`internal/bridge` + `internal/ghostty` + `scripts/iterm2/`" →
  "`internal/bridge` + `internal/ghostty` + `internal/warp` +
  `scripts/iterm2/`".

- [ ] **Step 3: Full verification**

Run: `go test ./... && gofmt -l . && go vet ./...`
Expected: tests PASS, gofmt prints nothing, vet clean.

- [ ] **Step 4: Commit**

```bash
git add docs/native-windows.md README.md
git commit -m "docs: Warp native windows — guide section and README rows

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 8: Live QA on this machine (Warp Stable installed)

**Files:** none — manual verification, results reported to the user.

- [ ] **Step 1: Build the branch binary**

```bash
go build -o /tmp/sm-warp ./cmd/sm
```

- [ ] **Step 2: Local Warp, `tmux.enabled: true`** (default config already
  has it; run from a Warp tab, NOT inside tmux)

Run `/tmp/sm-warp` with `"open_in": "window"` in `~/.config/sm/config.json`, then:

- `enter` on a session → a native Warp window opens in the session's dir
  running `tmux new-session -A -s sm-…` with the agent resumed; sm stays
  put and shows the `●` live marker within ~2s.
- `enter` on the same session again → a second window attaches to the
  same tmux session (mirror — expected, no refocus).
- `n` → new-session flow opens a Warp window; the pending tmux name is
  adopted to the real id within ~2s of the agent starting.
- `x` on the live session kills it; the Warp window's tmux exits.

- [ ] **Step 3: Local Warp, `tmux.enabled: false`**

Set `"tmux": { "enabled": false }`, rerun: `enter` opens a Warp window
running the bare agent (no tmux, no marker). Restore `true` after.

- [ ] **Step 4: `sm ssh` from Warp**

Pick a reachable host with key-based login and sm's requirements
(`AcceptEnv LC_*`, tmux for tracking):

```bash
/tmp/sm-warp ssh <host>
```

Expect the ready line to say "open Warp windows here". On the server (sm
≥ this branch not required — any bridge-capable sm): `"open_in": "window"`,
run sm, `enter` a session → a native **Warp** window opens on the desktop
dialing `ssh -t -- <host> …` back into the tracked tmux session.
`SM_BRIDGE_DEBUG=1` on the `sm ssh` side prints accepted payloads if
anything is silent.

- [ ] **Step 5: Fallback spot-check inside tmux**

Native windows must still win inside a Warp-hosted tmux — the pane
inherits `WARP_TERMINAL_SESSION_UUID` even though tmux rewrites
`TERM_PROGRAM`. Run `/tmp/sm-warp` inside a tmux attach in Warp and
confirm `enter` still opens a native Warp window. Then relaunch with the
markers stripped —
`env -u WARP_TERMINAL_SESSION_UUID /tmp/sm-warp` — and confirm window
mode falls back to plain tmux windows as before.

- [ ] **Step 6: Report**

Report QA results to the user; on green, hand off to
superpowers:finishing-a-development-branch (merge to `main` and/or PR
upstream per the spec's Rollout section).
