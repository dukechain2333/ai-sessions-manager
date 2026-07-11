# Claude Code Visual Theme Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reskin `sm` in Claude Code's visual language (coral accent, layered grays, `>`/`⏺`/`⎿` glyphs, `> ` prompt, `✻` title mark) with zero functional change.

**Architecture:** Pure presentation swap: `styles.go` palette rewrite, three prefix strings in `preview.go`, prompt/title strings in `model.go`, a one-column prompt-zone tweak in `mouse.go`, a split-styled group-header count in `listpane.go`. All hard invariants (help-bar text, dialog corner glyphs, pane geometry) are already pinned by tests.

**Tech Stack:** lipgloss AdaptiveColor (truecolor hex), existing test suite as the regression net.

**Spec:** `docs/superpowers/specs/2026-07-11-claude-code-theme-design.md`

## Global Constraints

- Zero functional change except the deliberate prompt-zone narrowing `x <= 2` → `x <= 1` (the `> ` prompt occupies columns 0-1).
- Help-bar rendered text stays exactly `" ↵ resume  tab focus  n new  d delete  / filter  g group  space fold  e empty  r rescan  q quit"`; placeholders `filter…`/`search…` unchanged; dialog borders stay `RoundedBorder`.
- Palette (AdaptiveColor, Light/Dark): accent `#C15F3C`/`#D97757`; text `#333333`/`#DEDEDE`; dim `#8A8A8A`/`#767676`; faint (blurred borders) `#D0D0D0`/`#3A3A3A`; warn unchanged (`160`/`203`).
- Prefixes: user `> ` (bold text color), assistant `⏺ `, tool `⎿ ` (dim). Filter prompt `> ` (accent). Title mark `✻ ` (accent) before bold `sm · AI Sessions`.
- Group headers lose the underline; the `(n)` count renders in accent via a new `GroupCount` style (selected or not).
- All work on branch `feat/search` in `~/Desktop/ai-sessions-manager`. Commit per task with trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`. Gate per task: `gofmt -l .` empty, `go vet ./...`, `go test -race ./...`.

---

### Task 1: Chrome — palette, prompt, title, zone, header count

**Files:**
- Modify: `internal/ui/styles.go` (defaultStyles rewrite + `GroupCount` field)
- Modify: `internal/ui/model.go:131` (prompt), `~:823` (title)
- Modify: `internal/ui/mouse.go` (zoneFilter branch: `x <= 2` → `x <= 1`, comment `🔍 icon` → `> prompt glyph`)
- Modify: `internal/ui/listpane.go` (header render: count segment via `GroupCount`)
- Test: `internal/ui/search_test.go` (boundary assertion), `internal/ui/model_test.go` or `search_test.go` (title/prompt pins)

**Interfaces:**
- Produces: `styles.GroupCount lipgloss.Style` (accent); everything else is value changes inside existing fields.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/search_test.go`:

```go
func TestPromptZoneNarrowedToPromptGlyph(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(click(2, 1)) // column 2 is now input area, not the prompt
	m = m2.(Model)
	if m.searchAll {
		t.Fatal("clicking column 2 must not toggle the layer (prompt is columns 0-1)")
	}
	if m.focus != focusFilter {
		t.Error("clicking the bar must still focus the filter")
	}
	m2, _ = m.Update(click(1, 1))
	m = m2.(Model)
	if !m.searchAll {
		t.Error("clicking the prompt glyph must still toggle")
	}
}

func TestClaudeThemeChrome(t *testing.T) {
	m := newTestModel()
	view := m.View()
	if !strings.Contains(view, "✻ sm · AI Sessions") {
		t.Errorf("title must carry the ✻ mark; head: %.80s", view)
	}
	if !strings.Contains(view, "> ") || strings.Contains(view, "🔍") {
		t.Error("filter prompt must be '> ', not 🔍")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/Desktop/ai-sessions-manager && go test ./internal/ui/ -run 'TestPromptZone|TestClaudeTheme' -v`
Expected: FAIL — column-2 click toggles today; title lacks `✻`; prompt is 🔍.

- [ ] **Step 3: Implement**

`internal/ui/styles.go` — add the field and rewrite `defaultStyles`:

```go
type styles struct {
	// … existing fields unchanged …
	GroupCount lipgloss.Style
}

func defaultStyles() styles {
	accent := lipgloss.AdaptiveColor{Light: "#C15F3C", Dark: "#D97757"}
	text := lipgloss.AdaptiveColor{Light: "#333333", Dark: "#DEDEDE"}
	dim := lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#767676"}
	faint := lipgloss.AdaptiveColor{Light: "#D0D0D0", Dark: "#3A3A3A"}
	warn := lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	return styles{
		AppTitle:       lipgloss.NewStyle().Bold(true).Foreground(text),
		Count:          lipgloss.NewStyle().Foreground(dim),
		ListTitle:      lipgloss.NewStyle().Foreground(text),
		ListTitleSel:   lipgloss.NewStyle().Bold(true).Foreground(accent),
		ListMeta:       lipgloss.NewStyle().Foreground(dim),
		ListMetaSel:    lipgloss.NewStyle().Foreground(accent),
		GroupHeader:    lipgloss.NewStyle().Bold(true).Foreground(text),
		GroupHeaderSel: lipgloss.NewStyle().Bold(true).Foreground(accent),
		GroupCount:     lipgloss.NewStyle().Foreground(accent),
		UserMsg:        lipgloss.NewStyle().Bold(true).Foreground(text),
		AssistantMsg:   lipgloss.NewStyle().Foreground(text),
		ToolMsg:        lipgloss.NewStyle().Foreground(dim),
		PaneFocused:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent),
		PaneBlurred:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(faint),
		Help:           lipgloss.NewStyle().Foreground(dim),
		ErrorText:      lipgloss.NewStyle().Bold(true).Foreground(warn),
		DialogBox:      lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
	}
}
```

Single source of truth for the accent pair: add a field `Accent lipgloss.AdaptiveColor` to the `styles` struct, set it first in `defaultStyles` (`Accent: accent,`), and add:

```go
// TitleMark is the Claude-style ✻ rendered in the accent color.
func (s styles) TitleMark() string {
	return lipgloss.NewStyle().Foreground(s.Accent).Render("✻")
}
```

`internal/ui/model.go`:
- Line ~131: replace `fi.Prompt = "🔍 "` with:

```go
	fi.Prompt = "> "
	fi.PromptStyle = lipgloss.NewStyle().Foreground(st.Accent)
```

(`st` is already in scope in `New()`; add the lipgloss import to model.go only if it is not already there — it is.)
- Title (~line 823): replace

```go
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		m.st.AppTitle.Render(" sm · AI Sessions  "),
		m.st.Count.Render(count),
	)
```

with

```go
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		m.st.TitleMark(), // ✻ in accent
		m.st.AppTitle.Render(" sm · AI Sessions  "),
		m.st.Count.Render(count),
	)
```

(The leading space inside AppTitle keeps `✻ sm` spaced; the rendered text becomes `✻ sm · AI Sessions  <count>`.)

`internal/ui/mouse.go` — zoneFilter branch:

```go
	case zoneFilter:
		m.focus = focusFilter
		m.filterInput.Focus()
		if msg.X <= 1 { // the "> " prompt glyph toggles the search layer
			return m, m.toggleSearchLayer()
		}
		return m, nil
```

`internal/ui/listpane.go` — in the header-row render branch of `View()`, split the count out of the single styled string: render the `▾ name` part with the existing header style and the `(n)` with `l.styles.GroupCount`, joined by a space, preserving the exact same visible text (`▾ project (n)`). Adapt mechanically to the local string-building code; the visible characters must not change (only their styling).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v` — new tests PASS; ALL pre-existing tests must pass untouched (help-bar text, dialog corner scan, zone tests, search pipeline).

- [ ] **Step 5: Full gate + commit**

```bash
gofmt -l . && go vet ./... && go test -race ./...
git add internal/ui/styles.go internal/ui/model.go internal/ui/mouse.go internal/ui/listpane.go internal/ui/search_test.go
git commit -m "feat(ui): Claude Code chrome — coral palette, > prompt, ✻ title"
```

---

### Task 2: Preview glyphs

**Files:**
- Modify: `internal/ui/preview.go:27-32` (three prefix strings)
- Modify: `internal/ui/preview_test.go:17` (old-glyph assertions)
- Modify: `internal/ui/search_test.go:621-624` (comment wording only: `🔍 icon` → `prompt glyph`)

- [ ] **Step 1: Update the assertion FIRST (this is the RED)**

`internal/ui/preview_test.go:17` becomes:

```go
	for _, want := range []string{"> make slides", "⏺ I'll read", "⎿ Bash: Run tests"} {
```

Run: `go test ./internal/ui/ -run TestRenderTranscript -v`
Expected: FAIL — renderer still emits `› `/`● `/`⚒ `.

- [ ] **Step 2: Implement**

`internal/ui/preview.go` — in the prefix switch:

```go
		var style = st.AssistantMsg
		prefix := "⏺ "
		switch m.Kind {
		case store.KindUser:
			style, prefix = st.UserMsg, "> "
		case store.KindTool:
			style, prefix = st.ToolMsg, "⎿ "
		}
```

Also update the function's doc comment (`Prefixes: > user, ⏺ assistant, ⎿ tool call.`) and the two comment-only 🔍 mentions in `search_test.go`.

- [ ] **Step 3: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v` — all PASS (highlight/jump tests are prefix-agnostic; offsets unchanged since all prefixes remain 2 columns).

- [ ] **Step 4: Full gate + commit**

```bash
gofmt -l . && go vet ./... && go test -race ./...
git add internal/ui/preview.go internal/ui/preview_test.go internal/ui/search_test.go
git commit -m "feat(ui): Claude Code message glyphs in the preview"
```

---

### Task 3: Docs, gate, install

**Files:**
- Modify: `README.md` (Search section: "click the 🔍 icon" → "click the `>` prompt")
- Modify: `docs/superpowers/specs/2026-07-10-full-text-search-design.md` (interaction-table row: `Click the 🔍 icon (bar columns 0–1)` → `Click the `>` prompt (bar columns 0–1)` — historical doc kept consistent)

- [ ] **Step 1: Edit the two docs** (exact replacements above; nothing else reflowed).

- [ ] **Step 2: Full gate**

```bash
cd ~/Desktop/ai-sessions-manager
gofmt -l . && go vet ./... && go test -race ./...
```

- [ ] **Step 3: Install**

```bash
make install && sm --version
```

Expected: builds and installs; the human eyeballs the theme in Ghostty (dark + light) afterward.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/superpowers/specs/2026-07-10-full-text-search-design.md
git commit -m "docs: prompt-glyph wording for the Claude Code theme"
```
