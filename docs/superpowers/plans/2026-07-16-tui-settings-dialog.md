# TUI Settings Dialog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `,`-triggered modal settings dialog to sm's TUI that edits every existing config.json key and writes the file back (apply-on-restart).

**Architecture:** A new `config.Save` serializes a resolved `Config` back to the canonical file shape. The UI gains a `dialogSettings` modal (new file `internal/ui/settings.go`) driven by a row table (`settingsRows()`) with enum/bool/text/hex row kinds; the form edits a working copy of the startup `config.Config`, and `s` writes it via an injected `saveConfig` func. No hot-reload: the running Model's state is untouched by a save.

**Tech Stack:** Go, Bubble Tea (`tea.Model` value semantics), lipgloss, `bubbles/textinput`. Spec: `docs/superpowers/specs/2026-07-16-tui-settings-dialog-design.md`.

## Global Constraints

- Module path: `github.com/dukechain2333/ai-sessions-manager`.
- Run tests with `go test ./...` from the repo root; run `gofmt -w` on every file you touch before committing.
- Do NOT modify `config.DefaultFileJSON` or `config.Load` semantics — existing tests pin them.
- Bubble Tea handlers use VALUE receivers (`func (m Model) handleKey(...) (tea.Model, tea.Cmd)`); mutate the local copy and return it. Pointer-receiver helpers (`func (m *Model) openSettings()`) may be called on that local copy before returning it — this matches the existing `openDirPicker` pattern.
- Settings row order and labels are pinned by the spec: `view`, `open_in mode`, `iterm2 ssh`, `tmux enabled`, `claude light`, `claude dark`, `codex light`, `codex dark`.
- Commit message style from repo history: `feat: …`, `test: …`, `docs: …`, with trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: `config.Save` and `config.ValidHex`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go` (append)

**Interfaces:**
- Consumes: existing `Config`, `Default()`, `Load(path)`, `hexRE`.
- Produces: `func Save(path string, cfg Config) error` and `func ValidHex(s string) bool` — Task 2 injects `config.Save` into the Model; Task 5 calls `config.ValidHex`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestSaveRoundTrip(t *testing.T) {
	cfg := Config{
		TmuxEnabled: true,
		Claude:      AgentColors{Light: "#111111", Dark: "#222222"},
		Codex:       AgentColors{Light: "#333333", Dark: "#444444"},
		View:        "tabs",
		OpenIn:      OpenInWindow,
		ITerm2SSH:   "generalserver",
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got != cfg {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, cfg)
	}
}

func TestSaveDefaultRoundTripsToDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got != Default() {
		t.Errorf("Save(Default()) did not round-trip:\n got %+v\nwant %+v", got, Default())
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "nested", "config.json")
	if err := Save(path, Default()); err != nil {
		t.Fatalf("Save into missing dirs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not written: %v", err)
	}
}

func TestValidHex(t *testing.T) {
	for s, want := range map[string]bool{
		"#C15F3C": true, "#abcdef": true,
		"C15F3C": false, "#C15F3": false, "#C15F3CZ": false, "": false, "#GGGGGG": false,
	} {
		if got := ValidHex(s); got != want {
			t.Errorf("ValidHex(%q) = %v, want %v", s, got, want)
		}
	}
}
```

(`filepath` and `os` are likely already imported in the test file; add them if not.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestSave|TestValidHex' -v`
Expected: FAIL — `undefined: Save`, `undefined: ValidHex`.

- [ ] **Step 3: Write the implementation**

Append to `internal/config/config.go`:

```go
// ValidHex reports whether s is a #RRGGBB hex color — the same rule Load
// applies to color keys.
func ValidHex(s string) bool {
	return hexRE.MatchString(s)
}

// saveFile mirrors DefaultFileJSON's canonical shape. Save always writes
// the object form of open_in and every known key; the parser knows no
// other keys, so a full rewrite loses nothing.
type saveFile struct {
	View   string     `json:"view"`
	OpenIn saveOpenIn `json:"open_in"`
	Tmux   saveTmux   `json:"tmux"`
	Colors saveColors `json:"colors"`
}

type saveOpenIn struct {
	Mode   string     `json:"mode"`
	ITerm2 saveITerm2 `json:"iterm2"`
}

type saveITerm2 struct {
	SSH string `json:"ssh"`
}

type saveTmux struct {
	Enabled bool `json:"enabled"`
}

type saveColors struct {
	Claude saveAgentColors `json:"claude"`
	Codex  saveAgentColors `json:"codex"`
}

type saveAgentColors struct {
	Light string `json:"light"`
	Dark  string `json:"dark"`
}

// Save writes cfg to path in the canonical config.json shape, creating
// parent directories. It rewrites the whole file; user formatting and
// shorthand forms are normalized away.
func Save(path string, cfg Config) error {
	f := saveFile{
		View:   cfg.View,
		OpenIn: saveOpenIn{Mode: cfg.OpenIn, ITerm2: saveITerm2{SSH: cfg.ITerm2SSH}},
		Tmux:   saveTmux{Enabled: cfg.TmuxEnabled},
		Colors: saveColors{
			Claude: saveAgentColors{Light: cfg.Claude.Light, Dark: cfg.Claude.Dark},
			Codex:  saveAgentColors{Light: cfg.Codex.Light, Dark: cfg.Codex.Dark},
		},
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: all PASS (including the pre-existing suite).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/config/config.go internal/config/config_test.go
git add internal/config/
git commit -m "feat(config): add Save (canonical full rewrite) and ValidHex"
```

---

### Task 2: Model plumbing — retain config, path, and save hook

**Files:**
- Modify: `internal/ui/model.go` (Model struct, `New` signature and body)
- Modify: `cmd/sm/main.go:90` (the `ui.New` call)
- Modify: `internal/ui/model_test.go` (all 12 `New(...)` call sites)
- Test: `internal/ui/model_test.go` (append one test)

**Interfaces:**
- Consumes: `config.Save` from Task 1.
- Produces: `New(projectsDir, codexDir, configPath string, cfg config.Config) Model`; Model fields `cfg config.Config`, `configPath string`, `saveConfig func(string, config.Config) error`, `setInput textinput.Model` (plus `setForm config.Config`, `setCursor int`, `setEditing bool`, `setErr string` used from Task 3 on). Tasks 3–6 rely on these exact names.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/model_test.go`:

```go
func TestNewRetainsConfigAndPath(t *testing.T) {
	cfg := config.Default()
	cfg.ITerm2SSH = "generalserver"
	m := New("/nope", "/nope", "/tmp/x/config.json", cfg)
	if m.configPath != "/tmp/x/config.json" {
		t.Errorf("configPath = %q, want /tmp/x/config.json", m.configPath)
	}
	if m.cfg.ITerm2SSH != "generalserver" {
		t.Errorf("cfg not retained: %+v", m.cfg)
	}
	if m.saveConfig == nil {
		t.Error("saveConfig hook must default to config.Save")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestNewRetainsConfigAndPath -v`
Expected: FAIL to compile — `too many arguments` / `undefined field configPath`.

- [ ] **Step 3: Implement**

In `internal/ui/model.go`:

(a) Add fields to `Model` (after the existing `bridgePath` field, around line 96):

```go
	// settings dialog state; cfg is the startup file config (pre-downgrade —
	// the tmux/open_in fallbacks below adjust the runtime fields only), so
	// the form always seeds from what the file said, not degraded state.
	cfg        config.Config
	configPath string
	saveConfig func(string, config.Config) error // injected for tests
	setForm    config.Config                     // working copy while the dialog is open
	setCursor  int
	setEditing bool
	setErr     string // inline row/save error
	setInput   textinput.Model
```

(b) Change the `New` signature (line 162) to:

```go
func New(projectsDir, codexDir, configPath string, cfg config.Config) Model {
```

(c) Inside `New`, next to the existing `di := textinput.New()` block, add:

```go
	si := textinput.New()
	si.Prompt = ""
```

and add to the `ret := Model{...}` literal:

```go
		cfg:        cfg,
		configPath: configPath,
		saveConfig: config.Save,
		setInput:   si,
```

(d) In `cmd/sm/main.go` change the `ui.New` call to:

```go
	p := tea.NewProgram(ui.New(*projectsDir, *codexDir, path, cfg), tea.WithAltScreen(), tea.WithMouseCellMotion())
```

(e) Update the 12 test call sites in `internal/ui/model_test.go` — insert `""` as the third argument:

```bash
sed -i -E 's/\bNew\((("[^"]*"|t\.TempDir\(\)), ("[^"]*"|t\.TempDir\(\))), (cfg|config\.Default\(\))\)/New(\1, "", \4)/' internal/ui/model_test.go
```

Then verify nothing was missed: `grep -n 'New("' internal/ui/model_test.go` — every `New(` call must now have 4 arguments.

- [ ] **Step 4: Run the full suite**

Run: `go build ./... && go test ./...`
Expected: everything compiles; all tests PASS including the new one.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/model.go internal/ui/model_test.go cmd/sm/main.go
git add internal/ui/ cmd/sm/main.go
git commit -m "feat(ui): thread config path and startup config into the Model"
```

---

### Task 3: Settings dialog — open, navigate, render, close

**Files:**
- Create: `internal/ui/settings.go`
- Create: `internal/ui/settings_test.go`
- Modify: `internal/ui/model.go` (dialogKind consts ~line 38; `handleKey` focusList switch ~line 1207; `handleDialogKey` ~line 1456; `dialogView` ~line 1595)

**Interfaces:**
- Consumes: Model fields from Task 2.
- Produces: `dialogSettings`, `dialogInfo` dialogKind values; `settingsRows() []settingRow` with `settingRow{label string, kind settingKind, options []string, get func(*config.Config) string, set func(*config.Config, string)}` and kinds `settingEnum, settingBool, settingText, settingHex`; `(m *Model) openSettings()`; `(m Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd)`; `(m Model) settingsView() string`. Tasks 4–6 extend `handleSettingsKey`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/settings_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	"github.com/dukechain2333/ai-sessions-manager/internal/config"
)

// openSettingsDialog drives the ',' key from the list and asserts the
// dialog opened. (Named to avoid shadowing confusion with the Model method
// openSettings.)
func openSettingsDialog(t *testing.T, m Model) Model {
	t.Helper()
	m2, _ := m.handleKey(runeKey(","))
	sm := m2.(Model)
	if sm.dialog != dialogSettings {
		t.Fatalf("',' should open the settings dialog, got dialog=%v", sm.dialog)
	}
	return sm
}

func TestSettingsOpenSeedsFormFromStartupConfig(t *testing.T) {
	cfg := config.Default()
	cfg.ITerm2SSH = "generalserver"
	cfg.View = "tabs"
	m := New("/nope", "/nope", "/tmp/config.json", cfg)
	m2, _ := m.Update(scanDoneMsg{sessions: testSessions()})
	sm := openSettingsDialog(t, m2.(Model))
	if sm.setForm != cfg {
		t.Errorf("form must seed from the startup config:\n got %+v\nwant %+v", sm.setForm, cfg)
	}
	if sm.setCursor != 0 || sm.setEditing || sm.setErr != "" {
		t.Errorf("form state not reset: cursor=%d editing=%v err=%q", sm.setCursor, sm.setEditing, sm.setErr)
	}
}

func TestSettingsCursorMovesAndClamps(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	last := len(settingsRows()) - 1
	for i := 0; i < last+3; i++ { // overshoot: must clamp at the last row
		m2, _ := sm.handleDialogKey(runeKey("j"))
		sm = m2.(Model)
	}
	if sm.setCursor != last {
		t.Errorf("cursor = %d after overshooting down, want %d", sm.setCursor, last)
	}
	for i := 0; i < last+3; i++ {
		m2, _ := sm.handleDialogKey(runeKey("k"))
		sm = m2.(Model)
	}
	if sm.setCursor != 0 {
		t.Errorf("cursor = %d after overshooting up, want 0", sm.setCursor)
	}
}

func TestSettingsEscClosesWithoutSaving(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	saved := false
	sm.saveConfig = func(string, config.Config) error { saved = true; return nil }
	sm.setForm.View = "tabs" // dirty the form, then abandon it
	m2, _ := sm.handleDialogKey(escKey())
	sm = m2.(Model)
	if sm.dialog != dialogNone {
		t.Errorf("esc should close the dialog, got %v", sm.dialog)
	}
	if saved {
		t.Error("esc must not write the config")
	}
	if sm.cfg.View == "tabs" {
		t.Error("abandoned edits must not leak into m.cfg")
	}
}

func TestSettingsViewRendersEveryRow(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	out := sm.settingsView()
	for _, want := range []string{
		"view", "open_in mode", "iterm2 ssh", "tmux enabled",
		"claude light", "claude dark", "codex light", "codex dark",
		"#C15F3C", "#D97757", "#0A7C66", "#10A37F",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings view missing %q:\n%s", want, out)
		}
	}
}
```

Add the `escKey` helper if `settings_test.go` needs it (check first — if no helper exists in the package, add to `settings_test.go`):

```go
func escKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEsc} }
```

(import `tea "github.com/charmbracelet/bubbletea"` in that case).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run TestSettings -v`
Expected: FAIL to compile — `undefined: dialogSettings`, `undefined: settingsRows`.

- [ ] **Step 3: Implement**

(a) In `internal/ui/model.go`, extend the dialogKind consts (append after `dialogError` — do not reorder existing values):

```go
const (
	dialogNone dialogKind = iota
	dialogDelete
	dialogPickDir
	dialogPickAgent
	dialogKillProject
	dialogError
	dialogInfo     // neutral notice (e.g. "settings saved"); any key closes
	dialogSettings // the , settings form (settings.go)
)
```

(b) In `handleKey`'s `focusList` switch (next to `case "e":`), add:

```go
		case ",":
			m.openSettings()
			return m, nil
```

(c) In `handleDialogKey`'s switch, add two cases (after `case dialogError:`):

```go
	case dialogInfo:
		m.dialog = dialogNone
		m.errText = ""
		return m, nil

	case dialogSettings:
		return m.handleSettingsKey(msg)
```

(d) In `dialogView`'s switch, add:

```go
	case dialogInfo:
		return m.st.DialogBox.Render(
			m.errText + "\n\n" + m.st.Help.Render("press any key"))

	case dialogSettings:
		return m.settingsView()
```

(e) Create `internal/ui/settings.go`:

```go
package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dukechain2333/ai-sessions-manager/internal/config"
)

// The settings dialog edits a working copy of the startup config (m.setForm)
// and writes it back with config.Save on "s". Apply-on-restart by design:
// a save never touches the running Model's state.

type settingKind int

const (
	settingEnum settingKind = iota
	settingBool
	settingText
	settingHex
)

// settingRow is one line of the form. get/set go through the working copy
// as strings (bools as "true"/"false") so one table drives rendering,
// cycling, and editing for every kind.
type settingRow struct {
	label   string
	kind    settingKind
	options []string // settingEnum only
	get     func(*config.Config) string
	set     func(*config.Config, string)
}

// settingsRows is the full form, in the file's key order.
func settingsRows() []settingRow {
	return []settingRow{
		{label: "view", kind: settingEnum, options: []string{"list", "tabs"},
			get: func(c *config.Config) string { return c.View },
			set: func(c *config.Config, v string) { c.View = v }},
		{label: "open_in mode", kind: settingEnum, options: []string{config.OpenInCurrent, config.OpenInWindow},
			get: func(c *config.Config) string { return c.OpenIn },
			set: func(c *config.Config, v string) { c.OpenIn = v }},
		{label: "iterm2 ssh", kind: settingText,
			get: func(c *config.Config) string { return c.ITerm2SSH },
			set: func(c *config.Config, v string) { c.ITerm2SSH = v }},
		{label: "tmux enabled", kind: settingBool,
			get: func(c *config.Config) string { return strconv.FormatBool(c.TmuxEnabled) },
			set: func(c *config.Config, v string) { c.TmuxEnabled = v == "true" }},
		{label: "claude light", kind: settingHex,
			get: func(c *config.Config) string { return c.Claude.Light },
			set: func(c *config.Config, v string) { c.Claude.Light = v }},
		{label: "claude dark", kind: settingHex,
			get: func(c *config.Config) string { return c.Claude.Dark },
			set: func(c *config.Config, v string) { c.Claude.Dark = v }},
		{label: "codex light", kind: settingHex,
			get: func(c *config.Config) string { return c.Codex.Light },
			set: func(c *config.Config, v string) { c.Codex.Light = v }},
		{label: "codex dark", kind: settingHex,
			get: func(c *config.Config) string { return c.Codex.Dark },
			set: func(c *config.Config, v string) { c.Codex.Dark = v }},
	}
}

// openSettings seeds the form from the startup file config and shows the
// dialog. m.cfg, not the runtime fields — those may have been downgraded
// (tmux missing, open_in fallback) and colors only live in baked styles.
func (m *Model) openSettings() {
	m.setForm = m.cfg
	m.setCursor = 0
	m.setEditing = false
	m.setErr = ""
	m.dialog = dialogSettings
}

// handleSettingsKey is dialogSettings' key handler (Tasks 4–6 extend it).
func (m Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := settingsRows()
	switch msg.String() {
	case "j", "down":
		if m.setCursor < len(rows)-1 {
			m.setCursor++
		}
		return m, nil
	case "k", "up":
		if m.setCursor > 0 {
			m.setCursor--
		}
		return m, nil
	case "esc", "q":
		m.dialog = dialogNone
		m.setErr = ""
		return m, nil
	}
	return m, nil
}

// settingsView renders the form.
func (m Model) settingsView() string {
	rows := settingsRows()
	var b strings.Builder
	b.WriteString(m.st.AppTitle.Render("Settings") + "\n\n")
	for i, r := range rows {
		marker := "  "
		if i == m.setCursor {
			marker = m.st.ListTitleSel.Render("▶") + " "
		}
		b.WriteString(marker + fmt.Sprintf("%-13s", r.label) + m.renderSettingValue(r, i) + "\n")
	}
	b.WriteString("\n")
	if m.setErr != "" {
		b.WriteString(m.st.ErrorText.Render(m.setErr) + "\n")
	}
	help := "j/k move · ↵/←/→ change · s save · esc close"
	if m.setEditing {
		help = "↵ apply · esc cancel edit"
	}
	b.WriteString(m.st.Help.Render(help))
	return m.st.DialogBox.Render(b.String())
}

// renderSettingValue renders one row's value cell; the row being edited
// shows the live text input instead.
func (m Model) renderSettingValue(r settingRow, i int) string {
	if m.setEditing && i == m.setCursor {
		return m.setInput.View()
	}
	v := r.get(&m.setForm)
	switch r.kind {
	case settingEnum:
		return "◂ " + v + " ▸"
	case settingBool:
		if v == "true" {
			return "[x]"
		}
		return "[ ]"
	case settingHex:
		return v + " " + lipgloss.NewStyle().Foreground(lipgloss.Color(v)).Render("██")
	default:
		if v == "" {
			return m.st.ListMeta.Render("(unset)")
		}
		return v
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run TestSettings -v && go test ./...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/settings.go internal/ui/settings_test.go internal/ui/model.go
git add internal/ui/
git commit -m "feat(ui): settings dialog skeleton — open with ',', navigate, render, close"
```

---

### Task 4: Enum cycling and bool toggling

**Files:**
- Modify: `internal/ui/settings.go` (extend `handleSettingsKey`, add `activateSettingRow`)
- Test: `internal/ui/settings_test.go` (append)

**Interfaces:**
- Consumes: `settingsRows`, `handleSettingsKey` from Task 3.
- Produces: `(m Model) activateSettingRow(row settingRow, key string) (tea.Model, tea.Cmd)`; enum rows cycle on `enter`/`l`/`→` (forward) and `h`/`←` (backward); bool rows toggle on `enter`/`space`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/settings_test.go`:

```go
func TestSettingsEnumCycles(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.setCursor = 0 // view: list ⇄ tabs
	press := func(k string) {
		m2, _ := sm.handleDialogKey(runeKey(k))
		sm = m2.(Model)
	}
	press("l")
	if sm.setForm.View != "tabs" {
		t.Errorf("l should cycle view to tabs, got %q", sm.setForm.View)
	}
	press("l") // wraps
	if sm.setForm.View != "list" {
		t.Errorf("cycling past the end should wrap to list, got %q", sm.setForm.View)
	}
	press("h") // backward wraps too
	if sm.setForm.View != "tabs" {
		t.Errorf("h should cycle backward (wrapping), got %q", sm.setForm.View)
	}
	m2, _ := sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	if sm.setForm.View != "list" {
		t.Errorf("enter should cycle forward, got %q", sm.setForm.View)
	}
}

func TestSettingsBoolToggles(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.setCursor = 3 // tmux enabled
	m2, _ := sm.handleDialogKey(runeKey(" "))
	sm = m2.(Model)
	if !sm.setForm.TmuxEnabled {
		t.Error("space should toggle tmux on")
	}
	m2, _ = sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	if sm.setForm.TmuxEnabled {
		t.Error("enter should toggle tmux back off")
	}
}
```

(Add `tea "github.com/charmbracelet/bubbletea"` to the test file's imports if Task 3 didn't already.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestSettingsEnum|TestSettingsBool' -v`
Expected: FAIL — values unchanged (keys fall through the Task 3 switch).

- [ ] **Step 3: Implement**

In `internal/ui/settings.go`, extend `handleSettingsKey`'s switch (before the final `return m, nil`):

```go
	case "enter", " ", "left", "right", "h", "l":
		return m.activateSettingRow(rows[m.setCursor], msg.String())
```

and add:

```go
// activateSettingRow applies the "change" keys to the row under the cursor:
// enums cycle (backward on h/←), bools toggle, text rows open the editor
// (Task 5).
func (m Model) activateSettingRow(row settingRow, key string) (tea.Model, tea.Cmd) {
	switch row.kind {
	case settingEnum:
		delta := 1
		if key == "h" || key == "left" {
			delta = -1
		}
		cur := row.get(&m.setForm)
		idx := 0
		for i, o := range row.options {
			if o == cur {
				idx = i
			}
		}
		row.set(&m.setForm, row.options[(idx+delta+len(row.options))%len(row.options)])
	case settingBool:
		if key == "enter" || key == " " {
			row.set(&m.setForm, strconv.FormatBool(row.get(&m.setForm) != "true"))
		}
	}
	return m, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run TestSettings -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/settings.go internal/ui/settings_test.go
git add internal/ui/
git commit -m "feat(ui): settings enum cycling and bool toggling"
```

---

### Task 5: Text and hex editing with validation

**Files:**
- Modify: `internal/ui/settings.go` (editing branch in `handleSettingsKey`, text case in `activateSettingRow`)
- Test: `internal/ui/settings_test.go` (append)

**Interfaces:**
- Consumes: `config.ValidHex` (Task 1), `m.setInput`/`m.setEditing`/`m.setErr` (Task 2).
- Produces: `enter` on a text/hex row starts inline editing; while editing, `enter` commits (hex rows validate, invalid input sets `setErr` and stays in edit mode), `esc` abandons the row edit, all other keys feed `m.setInput`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/settings_test.go`:

```go
func TestSettingsTextEditCommits(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.setCursor = 2 // iterm2 ssh
	m2, _ := sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	if !sm.setEditing {
		t.Fatal("enter on a text row should start editing")
	}
	sm.setInput.SetValue("myhost")
	m2, _ = sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	if sm.setEditing {
		t.Error("enter should commit and leave edit mode")
	}
	if sm.setForm.ITerm2SSH != "myhost" {
		t.Errorf("committed value = %q, want myhost", sm.setForm.ITerm2SSH)
	}
	if sm.dialog != dialogSettings {
		t.Errorf("dialog must stay open after a commit, got %v", sm.dialog)
	}
}

func TestSettingsInvalidHexRejected(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.setCursor = 4 // claude light
	m2, _ := sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	sm.setInput.SetValue("nothex")
	m2, _ = sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	if !sm.setEditing {
		t.Error("invalid hex must keep the row in edit mode")
	}
	if sm.setErr == "" {
		t.Error("invalid hex must set an inline error")
	}
	if sm.setForm.Claude.Light != "#C15F3C" {
		t.Errorf("invalid hex must not be committed, got %q", sm.setForm.Claude.Light)
	}
}

func TestSettingsEscCancelsRowEditNotDialog(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.setCursor = 2
	m2, _ := sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	sm.setInput.SetValue("abandoned")
	m2, _ = sm.handleDialogKey(escKey())
	sm = m2.(Model)
	if sm.setEditing {
		t.Error("esc should leave edit mode")
	}
	if sm.dialog != dialogSettings {
		t.Errorf("esc while editing must not close the dialog, got %v", sm.dialog)
	}
	if sm.setForm.ITerm2SSH != "" {
		t.Errorf("abandoned edit must not be committed, got %q", sm.setForm.ITerm2SSH)
	}
}

func TestSettingsEditSeedsCurrentValue(t *testing.T) {
	cfg := config.Default()
	cfg.ITerm2SSH = "generalserver"
	m := New("/nope", "/nope", "", cfg)
	m2, _ := m.Update(scanDoneMsg{sessions: testSessions()})
	sm := openSettingsDialog(t, m2.(Model))
	sm.setCursor = 2
	m3, _ := sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m3.(Model)
	if got := sm.setInput.Value(); got != "generalserver" {
		t.Errorf("editor must seed with the current value, got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestSettingsText|TestSettingsInvalid|TestSettingsEscCancels|TestSettingsEditSeeds' -v`
Expected: FAIL — `enter` on a text row does nothing yet (`setEditing` stays false).

- [ ] **Step 3: Implement**

In `internal/ui/settings.go`:

(a) At the TOP of `handleSettingsKey` (before the navigation switch), add the editing branch:

```go
	if m.setEditing {
		switch msg.Type {
		case tea.KeyEsc:
			m.setEditing = false
			m.setErr = ""
			m.setInput.Blur()
			return m, nil
		case tea.KeyEnter:
			row := rows[m.setCursor]
			v := strings.TrimSpace(m.setInput.Value())
			if row.kind == settingHex && !config.ValidHex(v) {
				m.setErr = "colors must be #RRGGBB"
				return m, nil
			}
			row.set(&m.setForm, v)
			m.setEditing = false
			m.setErr = ""
			m.setInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.setInput, cmd = m.setInput.Update(msg)
		return m, cmd
	}
```

(b) In `activateSettingRow`, add the text/hex case:

```go
	case settingText, settingHex:
		if key != "enter" {
			return m, nil
		}
		m.setInput.SetValue(row.get(&m.setForm))
		m.setInput.CursorEnd()
		m.setInput.Focus()
		m.setEditing = true
		m.setErr = ""
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run TestSettings -v && go test ./...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/settings.go internal/ui/settings_test.go
git add internal/ui/
git commit -m "feat(ui): settings inline text/hex editing with validation"
```

---

### Task 6: Save flow

**Files:**
- Modify: `internal/ui/settings.go` (`s` key in `handleSettingsKey`)
- Test: `internal/ui/settings_test.go` (append)

**Interfaces:**
- Consumes: `m.saveConfig`, `m.configPath` (Task 2), `dialogInfo` (Task 3).
- Produces: `s` (not editing) calls `saveConfig(configPath, setForm)`; success → `m.cfg = m.setForm`, `dialogInfo` with "Saved — restart sm to apply"; failure → stay in `dialogSettings` with `setErr` so the form survives for a retry.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/settings_test.go`:

```go
func TestSettingsSaveWritesAndConfirms(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.configPath = "/cfg/config.json"
	var gotPath string
	var gotCfg config.Config
	sm.saveConfig = func(p string, c config.Config) error { gotPath, gotCfg = p, c; return nil }
	sm.setForm.View = "tabs"
	m2, _ := sm.handleDialogKey(runeKey("s"))
	sm = m2.(Model)
	if gotPath != "/cfg/config.json" {
		t.Errorf("saved to %q, want /cfg/config.json", gotPath)
	}
	if gotCfg.View != "tabs" {
		t.Errorf("saved config = %+v, want the edited form", gotCfg)
	}
	if sm.dialog != dialogInfo {
		t.Errorf("save should show the info dialog, got %v", sm.dialog)
	}
	if !strings.Contains(sm.errText, "restart sm") {
		t.Errorf("confirmation should mention restarting, got %q", sm.errText)
	}
	if sm.cfg.View != "tabs" {
		t.Error("m.cfg must track the saved form so reopening shows saved values")
	}
}

func TestSettingsSaveFailureKeepsForm(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.saveConfig = func(string, config.Config) error { return errors.New("disk full") }
	sm.setForm.View = "tabs"
	m2, _ := sm.handleDialogKey(runeKey("s"))
	sm = m2.(Model)
	if sm.dialog != dialogSettings {
		t.Errorf("failed save must keep the dialog open, got %v", sm.dialog)
	}
	if !strings.Contains(sm.setErr, "disk full") {
		t.Errorf("save error must surface inline, got %q", sm.setErr)
	}
	if sm.setForm.View != "tabs" {
		t.Error("failed save must preserve the form for a retry")
	}
}
```

(Add `"errors"` to the test file's imports.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run TestSettingsSave -v`
Expected: FAIL — `s` falls through the switch, nothing saved.

- [ ] **Step 3: Implement**

In `handleSettingsKey`'s (non-editing) switch, add:

```go
	case "s":
		if err := m.saveConfig(m.configPath, m.setForm); err != nil {
			m.setErr = "save failed: " + err.Error()
			return m, nil
		}
		m.cfg = m.setForm
		m.dialog = dialogInfo
		m.errText = "Saved — restart sm to apply"
		m.setErr = ""
		return m, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run TestSettings -v && go test ./...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/settings.go internal/ui/settings_test.go
git add internal/ui/
git commit -m "feat(ui): settings save — write config.json, confirm, inline retry on failure"
```

---

### Task 7: Help bar entry and docs

**Files:**
- Modify: `internal/ui/mouse.go:39` (helpBar table)
- Modify: `README.md` (keys table ~line 120; Configuration section ~line 173)
- Test: `internal/ui/settings_test.go` (append)

**Interfaces:**
- Consumes: `helpBar`/`helpItem` in mouse.go — the table drives both the rendered help line and mouse hit-testing, so one entry makes `,` clickable for free.
- Produces: user-visible `, settings` in the help bar and README docs.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/settings_test.go`:

```go
func TestHelpBarHasSettingsEntry(t *testing.T) {
	m := newTestModel()
	if !strings.Contains(helpLineFor(m.helpItems()), ", settings") {
		t.Error("help bar must advertise the settings key")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestHelpBarHasSettingsEntry -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

In `internal/ui/mouse.go`, add to the `helpBar` table between `{"r rescan", runeKey("r")}` and `{"q quit", runeKey("q")}`:

```go
	{", settings", runeKey(",")},
```

In `README.md`:

(a) Keys table — add a row before the `q` row:

```markdown
| `,` | settings (edit `config.json` in-app; saved changes apply on restart) |
```

(b) Configuration section — after the sentence ending "falls back to defaults with a notice." (~line 177), add:

```markdown
You can also press `,` inside `sm` to edit every setting in a dialog —
saving rewrites `config.json` in the canonical shape; changes apply the
next time `sm` starts.
```

- [ ] **Step 4: Run the full suite**

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/mouse.go
git add internal/ui/ README.md
git commit -m "feat(ui): advertise settings in the help bar; document ',' in README"
```

---

## Final verification (after all tasks)

- [ ] `go build ./... && go test ./...` — everything green.
- [ ] Manual smoke test: `go run ./cmd/sm --config /tmp/sm-test-config.json`, press `,`, change a few values, `s`, quit, `cat /tmp/sm-test-config.json` — the file must contain the edited values in the canonical shape.
- [ ] Check off the spec's Testing section items against the implemented tests.
