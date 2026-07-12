# tmux integration, configurable colors, and config.json Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `config.json` that (a) enables optional tmux-backed resume/new sessions with panel markers and a kill key, and (b) overrides per-agent theme colors.

**Architecture:** A leaf `internal/config` package loads/validates the JSON. A leaf `internal/tmux` package owns session naming, the argv builders, and an injectable `Runner` boundary (real impl shells out). The Bubble Tea `Model` gains a live-tmux set discovered by polling `tmux list-sessions`, wraps resume/new commands in `tmux new-session` when enabled, renders `●` markers, kills on `x`, and adopts provisional new-session tmux on rescan. Colors flow from config into `defaultStyles`.

**Tech Stack:** Go ≥1.24, Bubble Tea v1, Lipgloss v1, tmux (external, runtime-optional). No new Go dependencies.

## Global Constraints

- Module `github.com/dukechain2333/ai-sessions-manager`; binary `sm`. Go at `~/.local/go` (prefix shell steps with `export PATH=$HOME/.local/go/bin:$PATH`).
- Config path: `$XDG_CONFIG_HOME/sm/config.json`, else `~/.config/sm/config.json`; overridable with `--config <path>`.
- Config defaults: `tmux.enabled=false`; Claude `{Light:"#C15F3C", Dark:"#D97757"}`; Codex `{Light:"#0A7C66", Dark:"#10A37F"}`.
- Missing file → defaults, no error. Malformed JSON → defaults + error (one startup dialog). Missing/invalid field → that field's default. Valid hex = `#` + exactly 6 hex digits.
- tmux session name = `sm-<agent>-<id8>` where `<id8>` = first 8 lowercased chars of the session UUID; `<agent>` ∈ `claude`/`codex`. Provisional new-session name = `sm-<agent>-pending-<nonce>`.
- Discovery is the single source of truth (no persisted tmux state): `tmux list-sessions -F '#{session_name}'`, `sm-`-filtered; "no server running" = empty set, not an error. Refresh on a 2s tick, after attach-return, and after a kill.
- Resume argv: `new-session -A -s <name> -c <cwd> <agent-cmd…>`. New argv: `new-session -s <name> -c <cwd> <agent-cmd…>`.
- `tmux.enabled=true` but `tmux` absent from PATH → one startup error dialog + integration off for the run.
- Marker: trailing `●` in the agent accent on a session row; on a project header when any child has a live tmux. `x` kills (session: immediate; header: confirm then kill-all). `x kill` help item + key exist only when integration is on.
- gofmt-clean; `go vet ./...`, `go test -race ./...` green; commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- After the final task: `make install`. DO NOT publish/tag/release until the user asks. Do NOT bump the version var.

---

### Task 1: `internal/config` package

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type AgentColors struct { Light, Dark string }`
  - `type Config struct { TmuxEnabled bool; Claude, Codex AgentColors }`
  - `func Default() Config`
  - `func Path(override string) (string, error)`
  - `func Load(path string) (Config, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/config/config_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.TmuxEnabled {
		t.Error("tmux should default off")
	}
	if c.Claude.Light != "#C15F3C" || c.Claude.Dark != "#D97757" {
		t.Errorf("claude default = %+v", c.Claude)
	}
	if c.Codex.Light != "#0A7C66" || c.Codex.Dark != "#10A37F" {
		t.Errorf("codex default = %+v", c.Codex)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should be no error, got %v", err)
	}
	if c != Default() {
		t.Errorf("missing file = %+v, want Default()", c)
	}
}

func TestLoadPartialOverride(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	os.WriteFile(p, []byte(`{"tmux":{"enabled":true},"colors":{"codex":{"dark":"#123456"}}}`), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !c.TmuxEnabled {
		t.Error("tmux.enabled should be true")
	}
	if c.Codex.Dark != "#123456" {
		t.Errorf("codex.dark = %q, want #123456", c.Codex.Dark)
	}
	if c.Codex.Light != "#0A7C66" {
		t.Errorf("codex.light should keep default, got %q", c.Codex.Light)
	}
	if c.Claude != Default().Claude {
		t.Errorf("claude untouched should stay default, got %+v", c.Claude)
	}
}

func TestLoadMalformedReturnsDefaultsAndError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(p, []byte(`{ this is not json `), 0o644)
	c, err := Load(p)
	if err == nil {
		t.Error("malformed JSON should return an error")
	}
	if c != Default() {
		t.Errorf("malformed should fall back to Default(), got %+v", c)
	}
}

func TestLoadInvalidHexFallsBackPerField(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	os.WriteFile(p, []byte(`{"colors":{"claude":{"light":"blue","dark":"#ABCDEF"}}}`), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Claude.Light != "#C15F3C" {
		t.Errorf("invalid light should fall back to default, got %q", c.Claude.Light)
	}
	if c.Claude.Dark != "#ABCDEF" {
		t.Errorf("valid dark should apply, got %q", c.Claude.Dark)
	}
}

func TestPathHonorsXDGAndOverride(t *testing.T) {
	if got, _ := Path("/tmp/my.json"); got != "/tmp/my.json" {
		t.Errorf("override = %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	got, err := Path("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/xdg/sm/config.json" {
		t.Errorf("XDG path = %q, want /xdg/sm/config.json", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/config/`
Expected: FAIL (package/functions undefined).

- [ ] **Step 3: Implement the config package**

Create `internal/config/config.go`:
```go
// Package config loads sm's optional config.json (tmux toggle and per-agent
// colors). Every key is optional; missing or invalid values fall back to the
// built-in defaults.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

// AgentColors is one agent's light/dark accent hex.
type AgentColors struct{ Light, Dark string }

// Config is the resolved configuration (defaults already filled in).
type Config struct {
	TmuxEnabled bool
	Claude      AgentColors
	Codex       AgentColors
}

// Default is the built-in configuration used when no file (or no key) is set.
func Default() Config {
	return Config{
		TmuxEnabled: false,
		Claude:      AgentColors{Light: "#C15F3C", Dark: "#D97757"},
		Codex:       AgentColors{Light: "#0A7C66", Dark: "#10A37F"},
	}
}

// Path resolves the config path: override if non-empty, else
// $XDG_CONFIG_HOME/sm/config.json, else ~/.config/sm/config.json.
func Path(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "sm", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "sm", "config.json"), nil
}

type fileColors struct {
	Light string `json:"light"`
	Dark  string `json:"dark"`
}

type fileConfig struct {
	Tmux *struct {
		Enabled bool `json:"enabled"`
	} `json:"tmux"`
	Colors *struct {
		Claude *fileColors `json:"claude"`
		Codex  *fileColors `json:"codex"`
	} `json:"colors"`
}

var hexRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Load reads and validates the config at path. A missing file returns
// Default() and nil. Malformed JSON returns Default() and an error so the
// caller can surface one dialog. Missing/invalid fields keep their defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	var f fileConfig
	if err := json.Unmarshal(data, &f); err != nil {
		return Default(), err
	}
	if f.Tmux != nil {
		cfg.TmuxEnabled = f.Tmux.Enabled
	}
	if f.Colors != nil {
		applyColors(&cfg.Claude, f.Colors.Claude)
		applyColors(&cfg.Codex, f.Colors.Codex)
	}
	return cfg, nil
}

// applyColors overrides dst with any present, valid hex fields in src.
func applyColors(dst *AgentColors, src *fileColors) {
	if src == nil {
		return
	}
	if hexRE.MatchString(src.Light) {
		dst.Light = src.Light
	}
	if hexRE.MatchString(src.Dark) {
		dst.Dark = src.Dark
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): load config.json (tmux toggle, per-agent colors)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Wire config into styles, `New`, and `main` (configurable colors)

**Files:**
- Modify: `internal/ui/styles.go` (add `stylesWithColors`; `defaultStyles` delegates)
- Modify: `internal/ui/model.go` (`New` takes `config.Config`; store `tmuxEnabled`)
- Modify: `cmd/sm/main.go` (`--config` flag, load config, pass to `New`)
- Modify: `internal/ui/model_test.go` (update the two `New(...)` call sites)
- Test: `internal/ui/styles_test.go` (color-override test)

**Interfaces:**
- Consumes: `config.Config`, `config.AgentColors`, `config.Default()`.
- Produces: `func stylesWithColors(claude, codex config.AgentColors) styles`; `func New(projectsDir, codexDir string, cfg config.Config) Model`; `Model.tmuxEnabled bool` (raw config value; the PATH check lands in Task 5).

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/styles_test.go`:
```go
func TestStylesWithColorsOverridesAccents(t *testing.T) {
	st := stylesWithColors(
		config.AgentColors{Light: "#111111", Dark: "#222222"},
		config.AgentColors{Light: "#333333", Dark: "#444444"},
	)
	if st.Accent.Light != "#111111" || st.Accent.Dark != "#222222" {
		t.Errorf("claude accent = %+v", st.Accent)
	}
	if st.CodexAccent.Light != "#333333" || st.CodexAccent.Dark != "#444444" {
		t.Errorf("codex accent = %+v", st.CodexAccent)
	}
	// A derived style must pick up the override too.
	if st.ClaudeTag.GetForeground() != lipgloss.AdaptiveColor(st.Accent) {
		t.Error("ClaudeTag should use the overridden accent")
	}
}
```
Add imports to `internal/ui/styles_test.go` if missing: `"github.com/charmbracelet/lipgloss"` and `"github.com/dukechain2333/ai-sessions-manager/internal/config"`.

- [ ] **Step 2: Run it to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestStylesWithColorsOverrides`
Expected: FAIL (`stylesWithColors` undefined).

- [ ] **Step 3: Add `stylesWithColors` and delegate `defaultStyles`**

In `internal/ui/styles.go`, add the import `"github.com/dukechain2333/ai-sessions-manager/internal/config"`. Replace the whole `defaultStyles()` function with a color-parameterized builder plus a defaults wrapper:
```go
func defaultStyles() styles {
	d := config.Default()
	return stylesWithColors(d.Claude, d.Codex)
}

// stylesWithColors builds the style set using the given per-agent accents.
func stylesWithColors(claude, codex config.AgentColors) styles {
	accent := lipgloss.AdaptiveColor{Light: claude.Light, Dark: claude.Dark}
	codexAccent := lipgloss.AdaptiveColor{Light: codex.Light, Dark: codex.Dark}
	text := lipgloss.AdaptiveColor{Light: "#333333", Dark: "#DEDEDE"}
	dim := lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#767676"}
	faint := lipgloss.AdaptiveColor{Light: "#D0D0D0", Dark: "#3A3A3A"}
	warn := lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	return styles{
		Accent:         accent,
		CodexAccent:    codexAccent,
		AppTitle:       lipgloss.NewStyle().Bold(true).Foreground(text),
		Count:          lipgloss.NewStyle().Foreground(dim),
		ListTitle:      lipgloss.NewStyle().Foreground(text),
		ListTitleSel:   lipgloss.NewStyle().Bold(true).Foreground(accent),
		ListMeta:       lipgloss.NewStyle().Foreground(dim),
		ListMetaSel:    lipgloss.NewStyle().Foreground(accent),
		CodexTitleSel:  lipgloss.NewStyle().Bold(true).Foreground(codexAccent),
		ClaudeTag:      lipgloss.NewStyle().Foreground(accent),
		CodexTag:       lipgloss.NewStyle().Foreground(codexAccent),
		GroupHeader:    lipgloss.NewStyle().Bold(true).Foreground(text),
		GroupHeaderSel: lipgloss.NewStyle().Bold(true).Foreground(accent),
		GroupCount:     lipgloss.NewStyle().Foreground(accent),
		UserMsg:        lipgloss.NewStyle().Bold(true).Foreground(text),
		AssistantMsg:   lipgloss.NewStyle().Foreground(text),
		ToolMsg:        lipgloss.NewStyle().Foreground(dim),
		PaneBlurred:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(faint),
		Help:           lipgloss.NewStyle().Foreground(dim),
		ErrorText:      lipgloss.NewStyle().Bold(true).Foreground(warn),
		DialogBox:      lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
	}
}
```

- [ ] **Step 4: Change `New` to take config and apply colors**

In `internal/ui/model.go`, add the import `"github.com/dukechain2333/ai-sessions-manager/internal/config"`, add the field `tmuxEnabled bool` to the `Model` struct (place it near the `providers` field), and change `New`:
```go
func New(projectsDir, codexDir string, cfg config.Config) Model {
	st := stylesWithColors(cfg.Claude, cfg.Codex)
```
(Keep the rest of `New`'s body unchanged except: set `tmuxEnabled: cfg.TmuxEnabled,` in the `Model{...}` literal. The raw value is fine here; Task 5 adds the tmux-on-PATH gate.)

- [ ] **Step 5: Update `main.go`**

In `cmd/sm/main.go`, add the `--config` flag, load the config, and pass it. Replace the flag/parse/run section:
```go
	projectsDir := flag.String("projects-dir", filepath.Join(home, ".claude", "projects"), "Claude Code projects directory")
	codexDir := flag.String("codex-dir", filepath.Join(home, ".codex", "sessions"), "Codex sessions directory")
	configPath := flag.String("config", "", "path to config.json (default: ~/.config/sm/config.json)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("sm", version)
		return
	}
	path, err := config.Path(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sm:", err)
		os.Exit(1)
	}
	cfg, cfgErr := config.Load(path)
	if cfgErr != nil {
		fmt.Fprintln(os.Stderr, "sm: config:", cfgErr, "(using defaults)")
	}
	p := tea.NewProgram(ui.New(*projectsDir, *codexDir, cfg), tea.WithAltScreen(), tea.WithMouseCellMotion())
```
Add the import `"github.com/dukechain2333/ai-sessions-manager/internal/config"`.

- [ ] **Step 6: Update the two test `New(...)` call sites**

In `internal/ui/model_test.go`, add the import `"github.com/dukechain2333/ai-sessions-manager/internal/config"`, then:
- line ~47: `m := New("/nope/claude", "/nope/codex", config.Default())`
- line ~54 (inside `newTestModel`): `m := New("/nonexistent-projects-dir", "/nonexistent-codex-dir", config.Default())`

- [ ] **Step 7: Full verification**

Run:
```bash
export PATH=$HOME/.local/go/bin:$PATH
gofmt -w internal/ui/styles.go internal/ui/model.go internal/ui/styles_test.go internal/ui/model_test.go cmd/sm/main.go
gofmt -l internal cmd            # expect empty
go build ./... && go vet ./... && go test ./internal/ui/ ./internal/config/
```
Expected: builds; gofmt clean; tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/styles.go internal/ui/model.go internal/ui/styles_test.go internal/ui/model_test.go cmd/sm/main.go
git commit -m "feat(ui): apply per-agent colors from config.json

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: `internal/tmux` package (naming, argv builders, Runner)

**Files:**
- Create: `internal/tmux/tmux.go`
- Test: `internal/tmux/tmux_test.go`

**Interfaces:**
- Produces:
  - `const Prefix = "sm-"`
  - `func Short(id string) string`
  - `func Name(agent, id8 string) string`
  - `func PendingName(agent string, nonce int64) string`
  - `func IsPending(name string) bool`
  - `func PendingAgent(name string) string`
  - `func ResumeArgs(name, cwd, agentName string, agentArgs []string) []string`
  - `func NewArgs(name, cwd, agentName string, agentArgs []string) []string`
  - `type Runner interface { List() (map[string]bool, error); Path(name string) (string, error); Kill(name string) error; Rename(from, to string) error }`
  - `type Exec struct{}` implementing `Runner`
  - `func parseList(out string) map[string]bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/tmux/tmux_test.go`:
```go
package tmux

import (
	"reflect"
	"testing"
)

func TestShort(t *testing.T) {
	if got := Short("ABCD1234-9c8f-43a7"); got != "abcd1234" {
		t.Errorf("Short = %q, want abcd1234", got)
	}
	if got := Short("s1"); got != "s1" {
		t.Errorf("Short short id = %q, want s1", got)
	}
}

func TestName(t *testing.T) {
	if got := Name("claude", "abcd1234"); got != "sm-claude-abcd1234" {
		t.Errorf("Name = %q", got)
	}
}

func TestPending(t *testing.T) {
	n := PendingName("codex", 42)
	if n != "sm-codex-pending-42" {
		t.Errorf("PendingName = %q", n)
	}
	if !IsPending(n) {
		t.Error("IsPending should be true for a pending name")
	}
	if IsPending("sm-codex-abcd1234") {
		t.Error("IsPending should be false for a normal name")
	}
	if got := PendingAgent(n); got != "codex" {
		t.Errorf("PendingAgent = %q, want codex", got)
	}
}

func TestResumeArgs(t *testing.T) {
	got := ResumeArgs("sm-claude-s1", "/x/alpha", "claude", []string{"--resume", "s1"})
	want := []string{"new-session", "-A", "-s", "sm-claude-s1", "-c", "/x/alpha", "claude", "--resume", "s1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResumeArgs = %v", got)
	}
}

func TestNewArgs(t *testing.T) {
	got := NewArgs("sm-codex-pending-42", "/x/beta", "codex", nil)
	want := []string{"new-session", "-s", "sm-codex-pending-42", "-c", "/x/beta", "codex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewArgs = %v", got)
	}
}

func TestParseList(t *testing.T) {
	out := "sm-claude-s1\nother-session\nsm-codex-pending-9\n\n"
	got := parseList(out)
	if !got["sm-claude-s1"] || !got["sm-codex-pending-9"] {
		t.Errorf("parseList missing sm- names: %v", got)
	}
	if got["other-session"] {
		t.Error("parseList should drop non-sm names")
	}
	if len(got) != 2 {
		t.Errorf("parseList size = %d, want 2", len(got))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/tmux/`
Expected: FAIL (package/functions undefined).

- [ ] **Step 3: Implement the tmux package**

Create `internal/tmux/tmux.go`:
```go
// Package tmux owns sm's tmux session naming, argv builders, and the
// injectable Runner boundary. sm keeps no persisted tmux state: the live set
// is discovered by listing sessions whose names carry the "sm-" prefix.
package tmux

import (
	"os/exec"
	"strconv"
	"strings"
)

// Prefix marks every sm-managed tmux session name.
const Prefix = "sm-"

const pendingInfix = "-pending-"

// Short is the first 8 lowercased characters of a session id (fewer if the
// id is shorter). tmux mangles '.', so the full UUID is never embedded.
func Short(id string) string {
	s := strings.ToLower(id)
	if len(s) > 8 {
		s = s[:8]
	}
	return s
}

// Name is the tmux session name for an agent and short id: sm-<agent>-<id8>.
func Name(agent, id8 string) string {
	return Prefix + agent + "-" + id8
}

// PendingName is a provisional name for a new session whose id is not known
// yet: sm-<agent>-pending-<nonce>.
func PendingName(agent string, nonce int64) string {
	return Prefix + agent + pendingInfix[1:] + strconv.FormatInt(nonce, 10)
}

// IsPending reports whether name is a provisional new-session tmux.
func IsPending(name string) bool {
	return strings.HasPrefix(name, Prefix) && strings.Contains(name, pendingInfix)
}

// PendingAgent extracts the agent segment of a provisional name, or "".
func PendingAgent(name string) string {
	if !IsPending(name) {
		return ""
	}
	rest := strings.TrimPrefix(name, Prefix)
	i := strings.Index(rest, pendingInfix)
	if i < 0 {
		return ""
	}
	return rest[:i]
}

// ResumeArgs builds the tmux argv (after the "tmux" binary) that attaches to
// session name if it exists, else creates it in cwd running the agent command.
func ResumeArgs(name, cwd, agentName string, agentArgs []string) []string {
	args := []string{"new-session", "-A", "-s", name, "-c", cwd, agentName}
	return append(args, agentArgs...)
}

// NewArgs builds the tmux argv for a fresh (non-attach) session in cwd.
func NewArgs(name, cwd, agentName string, agentArgs []string) []string {
	args := []string{"new-session", "-s", name, "-c", cwd, agentName}
	return append(args, agentArgs...)
}

// Runner is the injectable tmux boundary. The real implementation is Exec;
// tests inject a fake.
type Runner interface {
	// List returns the set of live sm-prefixed session names. A missing tmux
	// server yields an empty set, not an error.
	List() (map[string]bool, error)
	// Path returns a session's pane_current_path (used to place provisional
	// new-session tmux during adoption).
	Path(name string) (string, error)
	Kill(name string) error
	Rename(from, to string) error
}

// Exec is the real Runner; it shells out to tmux.
type Exec struct{}

func (Exec) List() (map[string]bool, error) {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		// No server running (or no sessions) is an empty set, not an error.
		return map[string]bool{}, nil
	}
	return parseList(string(out)), nil
}

func (Exec) Path(name string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", name, "#{pane_current_path}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (Exec) Kill(name string) error {
	return exec.Command("tmux", "kill-session", "-t", name).Run()
}

func (Exec) Rename(from, to string) error {
	return exec.Command("tmux", "rename-session", "-t", from, to).Run()
}

// parseList keeps only sm-prefixed names from tmux list-sessions output.
func parseList(out string) map[string]bool {
	set := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if strings.HasPrefix(name, Prefix) {
			set[name] = true
		}
	}
	return set
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/tmux/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tmux/tmux.go internal/tmux/tmux_test.go
git commit -m "feat(tmux): session naming, argv builders, and Runner boundary

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Discovery set + panel markers

**Files:**
- Modify: `internal/ui/model.go` (tmux fields, tick/list messages + commands, Init/handlers)
- Modify: `internal/ui/listpane.go` (`SetTmuxLive`, marker rendering, `projectHasLiveTmux`)
- Test: `internal/ui/listpane_test.go` (marker rendering), `internal/ui/model_test.go` (tmuxListMsg wiring)

**Interfaces:**
- Consumes: `tmux.Runner`, `tmux.Name`, `tmux.Short`; `Model.tmuxEnabled`.
- Produces:
  - `Model` fields `tmux tmux.Runner`, `tmuxLive map[string]bool`.
  - messages `tmuxTickMsg struct{}`, `tmuxListMsg struct{ set map[string]bool }`.
  - `func (m Model) tmuxTickCmd() tea.Cmd`, `func (m Model) refreshTmuxCmd() tea.Cmd`.
  - `func (l *listPane) SetTmuxLive(set map[string]bool)`.
  - `func (l *listPane) projectHasLiveTmux(project string) bool`.
  - `func tmuxNameFor(s store.Session) string` (in `internal/ui`).

- [ ] **Step 1: Write the failing tests**

Add to `internal/ui/listpane_test.go`:
```go
func TestSessionMarkerRendersWhenLive(t *testing.T) {
	l := newTestPane() // s1 claude /x/alpha, s2 claude /x/beta
	l.SetTmuxLive(map[string]bool{tmuxNameFor(l.sessions[0]): true})
	v := l.View()
	if !strings.Contains(v, "●") {
		t.Errorf("expected a ● marker for the live session:\n%s", v)
	}
}

func TestNoMarkerWhenNoneLive(t *testing.T) {
	l := newTestPane()
	l.SetTmuxLive(map[string]bool{})
	if strings.Contains(l.View(), "●") {
		t.Error("no marker should render when nothing is live")
	}
}

func TestProjectHasLiveTmux(t *testing.T) {
	l := newTestPane()
	l.SetTmuxLive(map[string]bool{tmuxNameFor(l.sessions[1]): true}) // s2 -> beta
	if !l.projectHasLiveTmux("beta") {
		t.Error("beta should report a live tmux")
	}
	if l.projectHasLiveTmux("alpha") {
		t.Error("alpha should not report a live tmux")
	}
}
```

Add to `internal/ui/model_test.go`:
```go
func TestTmuxListMsgUpdatesMarkers(t *testing.T) {
	m := newTestModel()
	name := tmuxNameFor(m.list.sessions[0])
	m2, _ := m.Update(tmuxListMsg{set: map[string]bool{name: true}})
	m = m2.(Model)
	if !m.tmuxLive[name] {
		t.Error("model should store the live set")
	}
	if !m.list.projectHasLiveTmux(m.list.sessions[0].Project()) {
		t.Error("list pane should see the live tmux after tmuxListMsg")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'Marker|ProjectHasLiveTmux|TmuxListMsg'`
Expected: FAIL (`SetTmuxLive`, `tmuxNameFor`, `tmuxListMsg`, `projectHasLiveTmux` undefined).

- [ ] **Step 3: Add the listPane marker plumbing**

In `internal/ui/listpane.go`, add the import `"github.com/dukechain2333/ai-sessions-manager/internal/tmux"`. Add a field to the `listPane` struct (after `search`):
```go
	tmuxLive map[string]bool // live sm-<agent>-<id8> names; nil = none
```
Add methods (near `projectMajorityAgent`):
```go
// SetTmuxLive records the current live-tmux set for marker rendering.
func (l *listPane) SetTmuxLive(set map[string]bool) { l.tmuxLive = set }

// projectHasLiveTmux reports whether any session in project has a live tmux.
func (l *listPane) projectHasLiveTmux(project string) bool {
	for _, s := range l.sessions {
		if s.Project() == project && l.tmuxLive[tmuxNameFor(s)] {
			return true
		}
	}
	return false
}
```
In `View()`, render the header marker: replace the header `name := fmt.Sprintf("%s %s", indicator, r.project)` line's block so the marker is appended to the rendered header. After the existing `lines = append(lines, rendered)` for headers, change it to:
```go
			if l.projectHasLiveTmux(r.project) {
				rendered += " " + lipgloss.NewStyle().Foreground(l.styles.AgentAccent(l.projectMajorityAgent(r.project))).Render("●")
			}
			lines = append(lines, rendered)
			continue
```
For the session row, after computing `titleStyle`/`prefix` and before appending lines, build the title line with an optional marker. Replace the final `lines = append(lines, titleStyle.Render(store.Truncate(prefix+title, l.width)), ...)` block with:
```go
			titleWidth := l.width
			marker := ""
			if l.tmuxLive[tmuxNameFor(s)] {
				titleWidth -= 2 // reserve space for " ●"
				marker = " " + lipgloss.NewStyle().Foreground(l.styles.AgentAccent(s.Agent)).Render("●")
			}
			metaText := store.Truncate("  "+meta, l.width-len(tag)-3)
			lines = append(lines,
				titleStyle.Render(store.Truncate(prefix+title, titleWidth))+marker,
				metaStyle.Render(metaText)+" "+tagStyle.Render(tag),
				"")
```
(Remove the old `metaText := ...` and `lines = append(...)` lines this replaces.) Add the import `"github.com/charmbracelet/lipgloss"` to `listpane.go`.

- [ ] **Step 4: Add `tmuxNameFor` and the model discovery plumbing**

In `internal/ui/model.go`, add the import `"github.com/dukechain2333/ai-sessions-manager/internal/tmux"` and `"time"` is already present. Add the helper (near `focusedBorderColor`):
```go
// tmuxNameFor is the tmux session name sm uses for a session.
func tmuxNameFor(s store.Session) string {
	return tmux.Name(string(s.Agent), tmux.Short(s.ID))
}
```
Add fields to the `Model` struct (near `tmuxEnabled`):
```go
	tmux     tmux.Runner
	tmuxLive map[string]bool
```
In `New`, set the runner in the `Model{...}` literal: `tmux: tmux.Exec{},`. Add the messages (near `agentExitMsg`):
```go
	tmuxTickMsg struct{}
	tmuxListMsg struct{ set map[string]bool }
```
Add the commands (near `scanCmd`):
```go
// tmuxTickCmd schedules the next discovery poll.
func (m Model) tmuxTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tmuxTickMsg{} })
}

// refreshTmuxCmd lists live sm tmux sessions once.
func (m Model) refreshTmuxCmd() tea.Cmd {
	r := m.tmux
	return func() tea.Msg {
		set, _ := r.List()
		if set == nil {
			set = map[string]bool{}
		}
		return tmuxListMsg{set: set}
	}
}
```
Change `Init`:
```go
func (m Model) Init() tea.Cmd {
	if m.tmuxEnabled {
		return tea.Batch(m.scanCmd(), m.refreshTmuxCmd(), m.tmuxTickCmd())
	}
	return m.scanCmd()
}
```
Add message handlers in `Update`'s `switch` (e.g. after `agentExitMsg`):
```go
	case tmuxTickMsg:
		if !m.tmuxEnabled {
			return m, nil
		}
		return m, tea.Batch(m.refreshTmuxCmd(), m.tmuxTickCmd())

	case tmuxListMsg:
		m.tmuxLive = msg.set
		m.list.SetTmuxLive(msg.set)
		return m, nil
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'Marker|ProjectHasLiveTmux|TmuxListMsg' -v`
Expected: PASS.

- [ ] **Step 6: Full verification**

Run:
```bash
export PATH=$HOME/.local/go/bin:$PATH
gofmt -w internal/ui/model.go internal/ui/listpane.go internal/ui/listpane_test.go internal/ui/model_test.go
gofmt -l internal cmd            # expect empty
go vet ./... && go test ./internal/ui/
```
Expected: gofmt clean, vet clean, tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/model.go internal/ui/listpane.go internal/ui/listpane_test.go internal/ui/model_test.go
git commit -m "feat(ui): discover live tmux and render panel markers

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Launch resume/new through tmux + startup PATH gate

**Files:**
- Modify: `internal/ui/model.go` (`runAgentCmd` helper; use it in the four launch sites; PATH gate in `New`)
- Test: `internal/ui/model_test.go` (resume/new argv when enabled; PATH-gate fallback)

**Interfaces:**
- Consumes: `tmux.ResumeArgs`, `tmux.NewArgs`, `tmux.PendingName`, `tmux.Name`, `tmux.Short`; `m.now`.
- Produces: `func (m Model) runAgentCmd(p store.Provider, cwd string, resume *store.Session) tea.Cmd`; package var `var tmuxLookPath = func() bool { … }` (overridable in tests).

- [ ] **Step 1: Write the failing tests**

Add to `internal/ui/model_test.go`:
```go
func newTmuxModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	m := newTestModel()
	m.tmuxEnabled = true
	captured := &[]string{}
	m.runCmd = func(name, dir string, args ...string) tea.Cmd {
		*captured = append([]string{name, dir}, args...)
		return nil
	}
	m.now = func() time.Time { return time.Unix(0, 1234) }
	return m, captured
}

func TestResumeWrapsInTmuxWhenEnabled(t *testing.T) {
	m, cap := newTmuxModel(t)
	dir := t.TempDir() // startResume stats CWD; it must exist or it opens the picker
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0) // s1, claude
	m.startResume()
	got := *cap
	if len(got) < 3 || got[0] != "tmux" {
		t.Fatalf("resume should run tmux, got %v", got)
	}
	// tmux new-session -A -s sm-claude-s1 -c <dir> claude --resume s1
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "new-session -A -s sm-claude-s1 -c "+dir+" claude --resume s1") {
		t.Errorf("resume argv = %v", got)
	}
}

func TestNewWrapsInTmuxWhenEnabled(t *testing.T) {
	m, cap := newTmuxModel(t)
	m.launchNewSession("/x/alpha") // single provider (claude-only test model)
	got := *cap
	if len(got) < 3 || got[0] != "tmux" {
		t.Fatalf("new should run tmux, got %v", got)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "new-session -s sm-claude-pending-1234 -c /x/alpha claude") {
		t.Errorf("new argv = %v", got)
	}
}

func TestResumeStaysInlineWhenDisabled(t *testing.T) {
	m := newTestModel() // tmux disabled
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir // else startResume opens the dir picker
	captured := &[]string{}
	m.runCmd = func(name, dir string, args ...string) tea.Cmd {
		*captured = append([]string{name, dir}, args...)
		return nil
	}
	m.list.selectSession(0)
	m.startResume()
	if len(*captured) == 0 || (*captured)[0] != "claude" {
		t.Errorf("disabled resume should run claude directly, got %v", *captured)
	}
}

func TestTmuxMissingAtStartupDisablesAndWarns(t *testing.T) {
	orig := tmuxLookPath
	tmuxLookPath = func() bool { return false }
	defer func() { tmuxLookPath = orig }()
	cfg := config.Default()
	cfg.TmuxEnabled = true
	m := New("/nope", "/nope", cfg)
	if m.tmuxEnabled {
		t.Error("missing tmux should disable integration")
	}
	if m.dialog != dialogError {
		t.Error("missing tmux should raise a startup error dialog")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'WrapsInTmux|StaysInline|TmuxMissing'`
Expected: FAIL (`runAgentCmd`/`tmuxLookPath` behavior undefined; resume/new not wrapped).

- [ ] **Step 3: Add the PATH gate and `runAgentCmd`**

In `internal/ui/model.go`, add the package var (near `execCmd`):
```go
// tmuxLookPath reports whether tmux is on PATH; overridable in tests.
var tmuxLookPath = func() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}
```
In `New`, right before `return ret`, add the gate:
```go
	if ret.tmuxEnabled && !tmuxLookPath() {
		ret.tmuxEnabled = false
		ret.dialog = dialogError
		ret.errText = "tmux integration is enabled but tmux was not found on PATH"
	}
```
Add the helper (near `startResume`):
```go
// runAgentCmd launches an agent, wrapping it in tmux when integration is on.
// resume != nil resumes that session; resume == nil starts a new session with
// p in cwd.
func (m Model) runAgentCmd(p store.Provider, cwd string, resume *store.Session) tea.Cmd {
	if resume != nil {
		name, args := p.ResumeCommand(*resume)
		if m.tmuxEnabled {
			sess := tmux.Name(string(resume.Agent), tmux.Short(resume.ID))
			return m.runCmd("tmux", cwd, tmux.ResumeArgs(sess, cwd, name, args)...)
		}
		return m.runCmd(name, cwd, args...)
	}
	name, args := p.NewCommand()
	if m.tmuxEnabled {
		pend := tmux.PendingName(string(p.Agent()), m.now().UnixNano())
		return m.runCmd("tmux", cwd, tmux.NewArgs(pend, cwd, name, args)...)
	}
	return m.runCmd(name, cwd, args...)
}
```

- [ ] **Step 4: Route the four launch sites through `runAgentCmd`**

In `internal/ui/model.go`:

`startResume`: replace the final two lines
```go
	name, args := p.ResumeCommand(s)
	return m, m.runCmd(name, s.CWD, args...)
```
with
```go
	return m, m.runAgentCmd(p, s.CWD, &s)
```

`launchNewSession` (single-provider branch): replace
```go
		name, args := p.NewCommand()
		return m, m.runCmd(name, dir, args...)
```
with
```go
		return m, m.runAgentCmd(p, dir, nil)
```

`handleDialogKey` `dialogPickDir` resume branch: replace
```go
				name, args := p.ResumeCommand(*pending)
				return m, m.runCmd(name, dir, args...)
```
with
```go
				return m, m.runAgentCmd(p, dir, pending)
```

`handleDialogKey` `dialogPickAgent` branch: replace
```go
		name, args := p.NewCommand()
		return m, m.runCmd(name, dir, args...)
```
with
```go
		return m, m.runAgentCmd(p, dir, nil)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'WrapsInTmux|StaysInline|TmuxMissing' -v`
Expected: PASS.

- [ ] **Step 6: Full verification**

Run:
```bash
export PATH=$HOME/.local/go/bin:$PATH
gofmt -w internal/ui/model.go internal/ui/model_test.go
gofmt -l internal cmd            # expect empty
go vet ./... && go test ./internal/ui/
```
Expected: gofmt clean, vet clean, tests pass. (Existing resume/new tests still pass because the default model has `tmuxEnabled=false`.)

- [ ] **Step 7: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): launch resume/new inside tmux when enabled; PATH gate

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Kill key `x` + project kill-all + help item

**Files:**
- Modify: `internal/ui/model.go` (`x` handling, `dialogKillProject`, kill commands, `CursorProject` use)
- Modify: `internal/ui/listpane.go` (`CursorProject`)
- Modify: `internal/ui/mouse.go` (`helpItems`, `helpLineFor`, `clickHelp`)
- Modify: `internal/ui/mouse_test.go` (help-line assertion + helpBar iteration → helpItems)
- Test: `internal/ui/model_test.go` (single kill; header confirm + kill-all)

**Interfaces:**
- Consumes: `tmux.Name`, `tmux.Short`, `tmux.IsPending`, `m.tmux`.
- Produces:
  - `dialogKillProject dialogKind`
  - `Model.pendingKillProject string`
  - `func (m Model) killOneCmd(name string) tea.Cmd`
  - `func (m Model) killProjectCmd(project string) tea.Cmd`
  - `func (l *listPane) CursorProject() (string, bool)`
  - `func (m Model) helpItems() []helpItem`
  - `func helpLineFor(items []helpItem) string` (replaces `helpLine()`)

- [ ] **Step 1: Write the failing tests**

Add to `internal/ui/model_test.go`:
```go
type fakeTmux struct {
	live    map[string]bool
	paths   map[string]string
	killed  []string
	renamed [][2]string
}

func (f *fakeTmux) List() (map[string]bool, error) {
	cp := map[string]bool{}
	for k, v := range f.live {
		cp[k] = v
	}
	return cp, nil
}
func (f *fakeTmux) Path(name string) (string, error) { return f.paths[name], nil }
func (f *fakeTmux) Kill(name string) error {
	delete(f.live, name)
	f.killed = append(f.killed, name)
	return nil
}
func (f *fakeTmux) Rename(from, to string) error {
	delete(f.live, from)
	f.live[to] = true
	f.renamed = append(f.renamed, [2]string{from, to})
	return nil
}

func TestKillOneSession(t *testing.T) {
	m := newTestModel()
	m.tmuxEnabled = true
	name := tmuxNameFor(m.list.sessions[0])
	f := &fakeTmux{live: map[string]bool{name: true}}
	m.tmux = f
	m.tmuxLive = map[string]bool{name: true}
	m.list.SetTmuxLive(m.tmuxLive)
	m.list.selectSession(0)
	m2, cmd := m.Update(key("x"))
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("x on a live session should return a refresh cmd")
	}
	cmd() // runs killOneCmd's goroutine body
	if len(f.killed) != 1 || f.killed[0] != name {
		t.Errorf("expected kill of %s, got %v", name, f.killed)
	}
}

func TestKillProjectHeaderConfirms(t *testing.T) {
	m := newTestModel()
	m.tmuxEnabled = true
	n0 := tmuxNameFor(m.list.sessions[0]) // alpha
	f := &fakeTmux{live: map[string]bool{n0: true}}
	m.tmux = f
	m.tmuxLive = map[string]bool{n0: true}
	m.list.SetTmuxLive(m.tmuxLive)
	// Put the cursor on the alpha header.
	m.list.selectSession(0)
	for !m.list.OnHeader() {
		m.list.MoveCursor(-1)
	}
	m2, _ := m.Update(key("x"))
	m = m2.(Model)
	if m.dialog != dialogKillProject {
		t.Fatalf("x on a header should open the kill-project confirm, got %v", m.dialog)
	}
	m2, cmd := m.Update(key("y"))
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("confirming should return a kill cmd")
	}
	cmd()
	if len(f.killed) != 1 || f.killed[0] != n0 {
		t.Errorf("kill-all should have killed %s, got %v", n0, f.killed)
	}
}
```

Add to `internal/ui/model_test.go` an assertion that the help item is gated (uses `helpItems`):
```go
func TestKillHelpItemGatedByTmux(t *testing.T) {
	m := newTestModel() // disabled
	for _, it := range m.helpItems() {
		if it.label == "x kill" {
			t.Fatal("x kill must not show when tmux is disabled")
		}
	}
	m.tmuxEnabled = true
	found := false
	for _, it := range m.helpItems() {
		if it.label == "x kill" {
			found = true
		}
	}
	if !found {
		t.Error("x kill should show when tmux is enabled")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'KillOne|KillProject|KillHelpItem'`
Expected: FAIL (`dialogKillProject`, `helpItems`, `x` handling undefined).

- [ ] **Step 3: Add `CursorProject` to listPane**

In `internal/ui/listpane.go`, add:
```go
// CursorProject returns the project label of the row under the cursor
// (header or session). ok is false when the list is empty or on a subheader.
func (l *listPane) CursorProject() (string, bool) {
	if l.cursor < 0 || l.cursor >= len(l.rows) || l.rows[l.cursor].subheader {
		return "", false
	}
	return l.rows[l.cursor].project, true
}
```

- [ ] **Step 4: Add the dialog kind, kill commands, and `x` handling**

In `internal/ui/model.go`, add `dialogKillProject` to the `dialogKind` const block (after `dialogPickAgent`). Add the field `pendingKillProject string` to `Model` (near `pendingNewDir`). Add the kill commands (near `refreshTmuxCmd`):
```go
// killOneCmd kills one tmux session and re-lists.
func (m Model) killOneCmd(name string) tea.Cmd {
	r := m.tmux
	return func() tea.Msg {
		_ = r.Kill(name)
		set, _ := r.List()
		if set == nil {
			set = map[string]bool{}
		}
		return tmuxListMsg{set: set}
	}
}

// killProjectCmd kills every live tmux belonging to project's sessions
// (named children), plus any provisional tmux whose path base is project.
func (m Model) killProjectCmd(project string) tea.Cmd {
	r := m.tmux
	sessions := append([]store.Session(nil), m.list.Sessions()...)
	return func() tea.Msg {
		set, _ := r.List()
		if set == nil {
			set = map[string]bool{}
		}
		for _, s := range sessions {
			if s.Project() != project {
				continue
			}
			name := tmuxNameFor(s)
			if set[name] {
				_ = r.Kill(name)
				delete(set, name)
			}
		}
		for name := range set {
			if !tmux.IsPending(name) {
				continue
			}
			if p, err := r.Path(name); err == nil && filepath.Base(p) == project {
				_ = r.Kill(name)
				delete(set, name)
			}
		}
		return tmuxListMsg{set: set}
	}
}
```
In `handleKey`'s `default: // focusList` switch, add an `x` case (after the `case "d":` block):
```go
		case "x":
			if !m.tmuxEnabled {
				return m, nil
			}
			if m.list.OnHeader() {
				if proj, ok := m.list.CursorProject(); ok && m.projectHasLiveTmux(proj) {
					m.pendingKillProject = proj
					m.dialog = dialogKillProject
				}
				return m, nil
			}
			if s, _, ok := m.list.Selected(); ok {
				name := tmuxNameFor(s)
				if m.tmuxLive[name] {
					return m, m.killOneCmd(name)
				}
			}
			return m, nil
```
Add the model-level helper (near `projectLabelColor`):
```go
// projectHasLiveTmux reports whether the selected project has any live tmux.
func (m Model) projectHasLiveTmux(project string) bool {
	return m.list.projectHasLiveTmux(project)
}
```
Add the dialog handler branch in `handleDialogKey`'s switch (after `dialogPickAgent`):
```go
	case dialogKillProject:
		proj := m.pendingKillProject
		m.pendingKillProject = ""
		m.dialog = dialogNone
		switch msg.String() {
		case "y", "enter":
			return m, m.killProjectCmd(proj)
		}
		return m, nil
```
Add the dialog view branch in `dialogView`'s switch (after `dialogPickAgent`):
```go
	case dialogKillProject:
		n := 0
		for _, s := range m.list.Sessions() {
			if s.Project() == m.pendingKillProject && m.tmuxLive[tmuxNameFor(s)] {
				n++
			}
		}
		return m.st.DialogBox.Render(fmt.Sprintf(
			"Kill %d tmux in %s?\n\n%s", n, m.pendingKillProject,
			m.st.Help.Render("y confirm · n cancel")))
```
Ensure `internal/ui/model.go` imports `"path/filepath"` (already imported) and `fmt` (already imported).

- [ ] **Step 5: Make the help bar tmux-aware**

In `internal/ui/mouse.go`, replace the `helpLine()` function with a per-items version and add `helpItems`:
```go
// helpItems is the help bar for the current model: the base bar plus the
// tmux "x kill" item when integration is on.
func (m Model) helpItems() []helpItem {
	if !m.tmuxEnabled {
		return helpBar
	}
	items := make([]helpItem, 0, len(helpBar)+1)
	for _, it := range helpBar {
		items = append(items, it)
		if it.label == "d delete" {
			items = append(items, helpItem{"x kill", runeKey("x")})
		}
	}
	return items
}

// helpLineFor renders a help bar's text (unstyled).
func helpLineFor(items []helpItem) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = it.label
	}
	return " " + strings.Join(parts, "  ")
}
```
In `clickHelp`, iterate the model's items:
```go
func (m Model) clickHelp(x int) (tea.Model, tea.Cmd) {
	pos := lipgloss.Width(m.projectLabelText()) + 1 // label prefix + helpLine's leading space
	for _, it := range m.helpItems() {
		w := lipgloss.Width(it.label)
		if x >= pos && x < pos+w {
			m.focus = focusList
			return m.handleKey(it.key)
		}
		pos += w + 2
	}
	return m, nil
}
```
In `internal/ui/model.go` `View()`, change the help render:
```go
	styledHelp := m.st.Help.MaxWidth(helpBudget).Render(helpLineFor(m.helpItems()))
```

- [ ] **Step 6: Update the two mouse_test references**

In `internal/ui/mouse_test.go`:
- The exact-text assertion (~line 342): replace `helpLine()` with `helpLineFor(helpBar)` in both the call and the format args (the default test model has tmux disabled, so `helpBar` is the right expectation).
- The helpBar iteration (~line 709): change `for _, it := range helpBar {` to `for _, it := range m.helpItems() {` so the click-position walk matches what `clickHelp` now uses.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'KillOne|KillProject|KillHelpItem|Click|Help' -v`
Expected: PASS.

- [ ] **Step 8: Full verification**

Run:
```bash
export PATH=$HOME/.local/go/bin:$PATH
gofmt -w internal/ui/model.go internal/ui/listpane.go internal/ui/mouse.go internal/ui/mouse_test.go internal/ui/model_test.go
gofmt -l internal cmd            # expect empty
go vet ./... && go test -race ./...
```
Expected: gofmt clean, vet clean, all tests pass (incl. `-race`).

- [ ] **Step 9: Commit**

```bash
git add internal/ui/model.go internal/ui/listpane.go internal/ui/mouse.go internal/ui/mouse_test.go internal/ui/model_test.go
git commit -m "feat(ui): x kills tmux (session immediate, project confirmed)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: Adopt provisional new-session tmux on rescan

**Files:**
- Modify: `internal/ui/model.go` (`adoptPending`, `adoptCmd`, call from `scanDoneMsg`; refresh tmux after `agentExitMsg`)
- Test: `internal/ui/model_test.go` (adoption renames newest match; skips backed; no-op)

**Interfaces:**
- Consumes: `tmux.IsPending`, `tmux.PendingAgent`, `tmux.Name`, `tmux.Short`, `tmux.Runner`.
- Produces: `func adoptPending(r tmux.Runner, sessions []store.Session, set map[string]bool)`; `func (m Model) adoptCmd(sessions []store.Session) tea.Cmd`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/ui/model_test.go`:
```go
func TestAdoptRenamesNewestMatch(t *testing.T) {
	sessions := []store.Session{
		{ID: "old11111", CWD: "/x/alpha", Agent: store.AgentClaude, LastActivity: time.Unix(100, 0)},
		{ID: "new22222", CWD: "/x/alpha", Agent: store.AgentClaude, LastActivity: time.Unix(200, 0)},
	}
	pend := tmux.PendingName("claude", 5)
	f := &fakeTmux{
		live:  map[string]bool{pend: true},
		paths: map[string]string{pend: "/x/alpha"},
	}
	set := map[string]bool{pend: true}
	adoptPending(f, sessions, set)
	want := tmux.Name("claude", tmux.Short("new22222"))
	if len(f.renamed) != 1 || f.renamed[0][1] != want {
		t.Errorf("expected rename to %s, got %v", want, f.renamed)
	}
	if !set[want] || set[pend] {
		t.Errorf("set should swap pending for adopted: %v", set)
	}
}

func TestAdoptSkipsBacked(t *testing.T) {
	backed := tmux.Name("claude", tmux.Short("new22222"))
	sessions := []store.Session{
		{ID: "new22222", CWD: "/x/alpha", Agent: store.AgentClaude, LastActivity: time.Unix(200, 0)},
	}
	pend := tmux.PendingName("claude", 5)
	f := &fakeTmux{
		live:  map[string]bool{pend: true, backed: true},
		paths: map[string]string{pend: "/x/alpha"},
	}
	set := map[string]bool{pend: true, backed: true}
	adoptPending(f, sessions, set)
	if len(f.renamed) != 0 {
		t.Errorf("should not adopt an already-backed session, got %v", f.renamed)
	}
}

func TestAdoptNoMatchIsNoop(t *testing.T) {
	sessions := []store.Session{
		{ID: "z", CWD: "/x/beta", Agent: store.AgentClaude, LastActivity: time.Unix(200, 0)},
	}
	pend := tmux.PendingName("claude", 5)
	f := &fakeTmux{live: map[string]bool{pend: true}, paths: map[string]string{pend: "/x/other"}}
	set := map[string]bool{pend: true}
	adoptPending(f, sessions, set)
	if len(f.renamed) != 0 {
		t.Errorf("no cwd match should be a no-op, got %v", f.renamed)
	}
}
```
Add the import `"github.com/dukechain2333/ai-sessions-manager/internal/tmux"` to `internal/ui/model_test.go` if not already present.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestAdopt`
Expected: FAIL (`adoptPending` undefined).

- [ ] **Step 3: Implement adoption**

In `internal/ui/model.go`, add:
```go
// adoptPending links each live provisional new-session tmux to the newest
// matching (same cwd + agent) session that isn't already backed by a real
// sm-<agent>-<id8> tmux, renaming the provisional session to that name. It
// mutates set to reflect the renames.
func adoptPending(r tmux.Runner, sessions []store.Session, set map[string]bool) {
	backed := map[string]bool{}
	for name := range set {
		if !tmux.IsPending(name) {
			backed[name] = true
		}
	}
	for name := range set {
		if !tmux.IsPending(name) {
			continue
		}
		cwd, err := r.Path(name)
		if err != nil || cwd == "" {
			continue
		}
		agent := tmux.PendingAgent(name)
		best := -1
		for i, s := range sessions {
			if string(s.Agent) != agent || s.CWD != cwd {
				continue
			}
			target := tmux.Name(agent, tmux.Short(s.ID))
			if backed[target] || set[target] {
				continue
			}
			if best < 0 || s.LastActivity.After(sessions[best].LastActivity) {
				best = i
			}
		}
		if best < 0 {
			continue
		}
		target := tmux.Name(agent, tmux.Short(sessions[best].ID))
		if r.Rename(name, target) == nil {
			delete(set, name)
			set[target] = true
			backed[target] = true
		}
	}
}

// adoptCmd re-lists tmux, adopts provisional sessions against the given
// scanned sessions, and returns the resulting live set.
func (m Model) adoptCmd(sessions []store.Session) tea.Cmd {
	r := m.tmux
	snap := append([]store.Session(nil), sessions...)
	return func() tea.Msg {
		set, _ := r.List()
		if set == nil {
			set = map[string]bool{}
		}
		adoptPending(r, snap, set)
		return tmuxListMsg{set: set}
	}
}
```

- [ ] **Step 4: Trigger adoption on rescan and refresh after resume**

In `internal/ui/model.go` `scanDoneMsg` handler, after `store.Enrich(msg.sessions, m.providers, 8, ch)` and before the `if m.searchAll …` block, add adoption to the returned batch. Change the two return statements at the end of the success branch to include `m.adoptCmd(msg.sessions)` when enabled. Concretely, replace:
```go
		if m.searchAll && m.activeQuery != "" {
			m.list.SetSearchResults(nil) // never render old indices over the new ordering
			return m, tea.Batch(waitEnrich(ch), m.loadTranscriptCmd(), m.dispatchSearch())
		}
		return m, tea.Batch(waitEnrich(ch), m.loadTranscriptCmd())
```
with:
```go
		cmds := []tea.Cmd{waitEnrich(ch), m.loadTranscriptCmd()}
		if m.tmuxEnabled {
			cmds = append(cmds, m.adoptCmd(msg.sessions))
		}
		if m.searchAll && m.activeQuery != "" {
			m.list.SetSearchResults(nil) // never render old indices over the new ordering
			cmds = append(cmds, m.dispatchSearch())
		}
		return m, tea.Batch(cmds...)
```
In the `agentExitMsg` handler, refresh tmux markers when enabled. Replace:
```go
		return m, m.scanCmd()
```
with:
```go
		if m.tmuxEnabled {
			return m, tea.Batch(m.scanCmd(), m.refreshTmuxCmd())
		}
		return m, m.scanCmd()
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestAdopt -v`
Expected: PASS.

- [ ] **Step 6: Full verification**

Run:
```bash
export PATH=$HOME/.local/go/bin:$PATH
gofmt -w internal/ui/model.go internal/ui/model_test.go
gofmt -l internal cmd            # expect empty
go vet ./... && go test -race ./...
```
Expected: gofmt clean, vet clean, all tests pass (incl. `-race`).

- [ ] **Step 7: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go
git commit -m "feat(ui): adopt provisional new-session tmux on rescan

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 8: README documentation + manual e2e

**Files:**
- Modify: `README.md` (config.json section)

**Interfaces:** none (docs).

- [ ] **Step 1: Document config.json in the README**

In `README.md`, add a `## Configuration` section (place it after the usage/keys section; match the file's existing heading style and tone). It MUST include, verbatim where shown:

- Path resolution: `$XDG_CONFIG_HOME/sm/config.json`, else `~/.config/sm/config.json`; `--config <path>` override.
- The full example config:
  ```json
  {
    "tmux": { "enabled": false },
    "colors": {
      "claude": { "light": "#C15F3C", "dark": "#D97757" },
      "codex":  { "light": "#0A7C66", "dark": "#10A37F" }
    }
  }
  ```
- Key reference:
  - `tmux.enabled` (default `false`) — when `true`, resume and new sessions run inside a tmux session (`sm-<agent>-<id8>`) so work survives detaching (Ctrl-b d). Requires `tmux` on `PATH`; if missing, sm shows a startup notice and runs without it.
  - `colors.claude` / `colors.codex` — each optional `light`/`dark` `#RRGGBB` accent; omitted or invalid values keep the defaults.
- tmux behavior notes:
  - A session with a live tmux shows a `●` marker; a project header shows `●` when any of its sessions has one.
  - `x` kills the selected session's tmux; on a project header it kills all of that project's tmux (after a confirm).
  - Killing a tmux outside sm (e.g. `tmux kill-session`) is detected automatically — the marker clears on the next refresh.
  - Known edge: a **new** session's tmux is linked to its panel row on the next rescan by matching the newest session in that directory; starting two new sessions in the same directory before returning can label them in either order (both are still killable via the project header).

- [ ] **Step 2: Verify the README renders and mentions the essentials**

Run:
```bash
grep -n "config.json\|tmux.enabled\|XDG_CONFIG_HOME\|x kills\|●" README.md
```
Expected: matches for the config path, the tmux toggle, and the marker/kill notes.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document config.json (tmux integration, colors)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [ ] **Step 4: Manual e2e with real tmux (controller runs after review)**

Build, write a temp config enabling tmux, and drive a real terminal. This is a live check, not an automated test.
```bash
export PATH=$HOME/.local/go/bin:$PATH && make build
mkdir -p "$CLAUDE_JOB_DIR/tmp/smcfg/sm"
printf '{"tmux":{"enabled":true}}' > "$CLAUDE_JOB_DIR/tmp/smcfg/sm/config.json"
tmux kill-session -t smtest 2>/dev/null
tmux new-session -d -s smtest -x 150 -y 45 "XDG_CONFIG_HOME=$CLAUDE_JOB_DIR/tmp/smcfg ./sm"; sleep 3
tmux capture-pane -t smtest -p | tail -3   # confirm help row shows 'x kill'
tmux send-keys -t smtest 'q'; sleep 0.5; tmux kill-session -t smtest 2>/dev/null
```
Expected: the help row includes `x kill`. Full interactive verification (resume → attach → detach → `●` appears → `x` clears it → external `tmux kill-session` self-corrects on the next tick) is done by hand in a real terminal, matching how prior features were verified. Do NOT `make install` or publish here — the controller installs after review.

---

## Notes for the executor

- The default test model (`newTestModel`) has `tmuxEnabled=false`, so every pre-existing test keeps its current behavior; tmux paths are exercised only by the new tests that flip the flag and inject `fakeTmux`.
- `fakeTmux` is defined once in Task 6 and reused in Task 7 — if Task 7 runs first in isolation, move the type up, but under the normal task order it already exists.
- Do not bump `version` in `cmd/sm/main.go`; publishing is deferred.
