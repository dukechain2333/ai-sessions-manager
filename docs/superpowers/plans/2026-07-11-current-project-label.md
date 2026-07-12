# Current-Project Label Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show the selected session's project at the far left of the bottom instruction row, always visible, updating as the cursor moves.

**Architecture:** A single `Model.projectLabelText()` helper is the one source of truth for the label text and its width. `View()` renders it (accent-styled) to the left of the existing help line; the mouse help-bar hit-tester (`clickHelp`) offsets its button x-positions by the label's width so clicks stay consistent. No list-pane, layout-height, or scroll change.

**Tech Stack:** Go ≥1.24, Bubble Tea v1, Lipgloss v1. No new dependencies.

## Global Constraints

- Module `github.com/dukechain2333/ai-sessions-manager`; binary `sm`. Go at `~/.local/go` (prefix shell steps with `export PATH=$HOME/.local/go/bin:$PATH`).
- The bottom row stays ONE line, clamped to `m.width` (key hints truncate on the right first, as today).
- `View()` and `clickHelp` MUST derive the label from the same `projectLabelText()` so their geometry cannot drift.
- Label is `" ▸ <project>  "` (leading space, `▸`, project truncated to 40 runes via `store.Truncate`, two trailing spaces); empty string when no session is selected.
- gofmt-clean; `go vet ./...`, `go test -race ./...` green; commit message ends with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- After the task: `make install` to `~/.local/bin/sm`. DO NOT publish/tag/release until the user asks.

---

### Task 1: Current-project label on the instruction line

**Files:**
- Modify: `internal/ui/model.go` (add `projectLabelText`; change the bottom-line composition in `View()`)
- Modify: `internal/ui/mouse.go` (`clickHelp` x offset)
- Test: `internal/ui/model_test.go` (display test), `internal/ui/mouse_test.go` (mouse-consistency test + update existing help-click tests)

**Interfaces:**
- Consumes: `m.list.Selected() (store.Session, int, bool)`; `store.Session.Project() string`; `store.Truncate(s string, n int) string`; `m.st.Accent lipgloss.AdaptiveColor`; `m.st.Help lipgloss.Style`; `helpLine() string`; `helpBar []helpItem`.
- Produces: `func (m Model) projectLabelText() string` (used by both `View()` and `clickHelp`).

- [ ] **Step 1: Write the failing display test**

Add to `internal/ui/model_test.go`:
```go
func TestBottomLabelShowsSelectedProject(t *testing.T) {
	m := newTestModel() // grouped; cursor starts on the first session (s1, project "alpha")
	if got := m.projectLabelText(); got != " ▸ alpha  " {
		t.Fatalf("projectLabelText = %q, want %q", got, " ▸ alpha  ")
	}
	if v := m.View(); !strings.Contains(v, "▸ alpha") {
		t.Errorf("View bottom line missing project label:\n%s", v)
	}
	// Move the cursor onto the project header (no session selected) → empty label.
	m2, _ := m.Update(key("k"))
	m = m2.(Model)
	if !m.list.OnHeader() {
		t.Fatal("setup: k from first session should land on the header")
	}
	if got := m.projectLabelText(); got != "" {
		t.Errorf("projectLabelText on a header = %q, want empty", got)
	}
}
```
(`newTestModel`'s `testSessions()` give session `s1` CWD `/x/alpha` → `Project()=="alpha"`; the first cursor position is that session. `strings` is already imported in model_test.go.)

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestBottomLabelShowsSelectedProject`
Expected: FAIL (`projectLabelText` undefined).

- [ ] **Step 3: Add `projectLabelText` and render it in `View()`**

In `internal/ui/model.go`, add the helper (place it near `bodyHeight`/`paneWidths`):
```go
// projectLabelText is the current-project label shown at the far left of the
// bottom instruction row: " ▸ <project>  " for the selected session, or "" when
// no session is selected. It is the single source of truth for both the
// rendered label and the mouse help-bar x offset, so the two cannot drift.
func (m Model) projectLabelText() string {
	s, _, ok := m.list.Selected()
	if !ok {
		return ""
	}
	return " ▸ " + store.Truncate(s.Project(), 40) + "  "
}
```
In `View()`, replace the bottom-line construction:
```go
	help := m.st.Help.MaxWidth(m.width).Render(helpLine())
	return header + "\n" + filterBar + "\n" + body + "\n" + help
```
with:
```go
	label := m.projectLabelText()
	labelW := lipgloss.Width(label)
	helpBudget := m.width - labelW
	if helpBudget < 0 {
		helpBudget = 0
	}
	styledLabel := lipgloss.NewStyle().Bold(true).Foreground(m.st.Accent).
		MaxWidth(m.width).Render(label)
	styledHelp := m.st.Help.MaxWidth(helpBudget).Render(helpLine())
	return header + "\n" + filterBar + "\n" + body + "\n" + styledLabel + styledHelp
```

- [ ] **Step 4: Run the display test**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestBottomLabelShowsSelectedProject -v`
Expected: PASS.

- [ ] **Step 5: Write the failing mouse-consistency test**

Add to `internal/ui/mouse_test.go` (models here have a session selected, so the label is non-empty and shifts the help buttons):
```go
func TestClickHelpWithProjectLabelStillWorks(t *testing.T) {
	m := newTestModel() // a session is selected → non-empty project label
	// Compute the "q quit" button's screen x AFTER the label offset.
	base := lipgloss.Width(m.projectLabelText()) + 1 // +1 = helpLine leading space
	pos := base
	var qx int = -1
	for _, it := range helpBar {
		w := lipgloss.Width(it.label)
		if it.label == "q quit" {
			qx = pos + 1 // a cell inside the button
			break
		}
		pos += w + 2
	}
	if qx < 0 {
		t.Fatal("no 'q quit' item in helpBar")
	}
	m2, cmd := m.Update(click(qx, m.height-1))
	_ = m2
	if cmd == nil {
		t.Fatalf("click on 'q quit' at x=%d produced no cmd", qx)
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("clicking 'q quit' (label-shifted position) should quit")
	}
}
```
(`click(x, y)`, `newTestModel`, `helpBar`, `lipgloss`, and `tea` are already available in the ui test files.)

- [ ] **Step 6: Run it to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestClickHelpWithProjectLabelStillWorks`
Expected: FAIL — `clickHelp` still starts at `pos := 1`, so the label-shifted x maps to the wrong button (or none), no `QuitMsg`.

- [ ] **Step 7: Offset `clickHelp` by the label width**

In `internal/ui/mouse.go`, change `clickHelp`'s starting position:
```go
func (m Model) clickHelp(x int) (tea.Model, tea.Cmd) {
	pos := lipgloss.Width(m.projectLabelText()) + 1 // label prefix + helpLine's leading space
	for _, it := range helpBar {
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

- [ ] **Step 8: Fix any existing help-click tests broken by the shift**

Existing help-bar click tests in `internal/ui/mouse_test.go` (e.g. tests that `click(x, height-1)` on a help hint with a hardcoded `x`) assumed `pos` started at 1. With a selected session the label now shifts buttons right. Update each such test to compute the x as `lipgloss.Width(m.projectLabelText()) + <the old label-relative offset>` — i.e. mirror the `base`/`pos` walk from Step 5 to find the intended button's cell, instead of a magic number. Run `go test ./internal/ui/ -run 'Click|Help' -v` to find and fix each failure.

- [ ] **Step 9: Full verification**

Run:
```bash
export PATH=$HOME/.local/go/bin:$PATH
gofmt -w internal/ui/model.go internal/ui/mouse.go internal/ui/model_test.go internal/ui/mouse_test.go
gofmt -l internal cmd            # expect empty
go vet ./... && go test -race ./...
```
Expected: gofmt clean, vet clean, all tests pass (incl. `-race`).

- [ ] **Step 10: Commit**

```bash
git add internal/ui/model.go internal/ui/mouse.go internal/ui/model_test.go internal/ui/mouse_test.go
git commit -m "feat(ui): show current project at the left of the instruction line

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [ ] **Step 11: Live check (controller does the install)**

Build and eyeball the bottom row shows `▸ <project>` for the selection and updates as the cursor moves across projects:
```bash
export PATH=$HOME/.local/go/bin:$PATH && make build
tmux kill-session -t lbl 2>/dev/null
tmux new-session -d -s lbl -x 130 -y 40 "./sm"; sleep 3
tmux capture-pane -t lbl -p | tail -2
tmux send-keys -t lbl 'j j j j j'; sleep 1; tmux capture-pane -t lbl -p | tail -2
tmux send-keys -t lbl 'q'; sleep 0.5; tmux kill-session -t lbl 2>/dev/null
```
Expected: the last row starts with `▸ <project>` and the project changes after moving down across a project boundary. (Do NOT `make install` or publish here — the controller handles install after review.)
