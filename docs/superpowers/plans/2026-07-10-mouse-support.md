# Mouse Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every interactive element of `sm` mouse-operable (click to select, double-click to resume, clickable headers/help bar/dialog buttons, wheel scrolling) with zero new dependencies and zero keyboard-behavior changes.

**Architecture:** Enable Bubble Tea's `WithMouseCellMotion`, route `tea.MouseMsg` to a new `internal/ui/mouse.go` that resolves screen coordinates to zones using the same arithmetic `layout()` uses (extracted into shared helpers so they can't drift). Clicks on the help bar and dialogs synthesize the equivalent `tea.KeyMsg` and feed it through the existing `handleKey`/`handleDialogKey`, so a button can never behave differently from its key.

**Tech Stack:** Go 1.24, bubbletea v1.3.10 (`MouseMsg` Action/Button API), bubbles v1.0.0 (viewport handles wheel natively), lipgloss v1.1.0.

**Spec:** `docs/superpowers/specs/2026-07-10-mouse-support-design.md`

## Global Constraints

- No new dependencies — `go.mod` must not change.
- Keyboard behavior byte-for-byte unchanged; the help bar's rendered text must stay exactly `" ↵ resume  tab focus  n new  d delete  / filter  g group  space fold  e empty  r rescan  q quit"`.
- Double-click window: 400 ms, via injected clock (`now func() time.Time` on `Model`), never `time.Now()` inline in mouse logic.
- Only `MouseActionPress` events act; `Left` for clicks, `WheelUp`/`WheelDown` for scrolling. Motion/release/right/middle are ignored.
- All work on branch `feat/mouse-support` in `~/Desktop/ai-sessions-manager`. Commit after every task. CI gate: `gofmt -l .` empty, `go vet ./...`, `go test -race ./...`.
- Geometry ground truth (from `layout()`/`View()`): row 0 title, row 1 filter bar, row 2 body top border, pane content rows `y ∈ [3, 2+bodyH]` where `bodyH = max(height-5, 3)`, help bar at `y = height-1`. List content `x ∈ [1, listW-2]`; preview content `x ∈ [listW+1, listW+previewW]`; `listW = max(width*2/5, 20)` (narrow `<80` cols: `listW = width-2`, no preview); `previewW = max(width-listW-2, 10)`.
- Test fixture facts (`newTestModel()`: 100×30, grouped): `listW=40`, `bodyH=25`, content rows `y∈[3,27]`, list `x∈[1,38]`, preview `x∈[41,98]`, help `y=29`. Rows: header *alpha* y=3, session `s1` y=4–6, header *beta* y=7, session `s2` y=8–10.

---

### Task 1: `listPane.RowAtLine` + `SetCursor`

**Files:**
- Modify: `internal/ui/listpane.go` (append after `MoveCursor`, ~line 146)
- Test: `internal/ui/listpane_test.go`

**Interfaces:**
- Consumes: existing `listPane.layout() (start []int, total int)`, `lineOffset`, `ensureVisible()`.
- Produces: `func (l *listPane) RowAtLine(visible int) (int, bool)` — maps a 0-based *visible* content line (0 = first line currently on screen) to a row index, accounting for `lineOffset` and mixed 1-line-header/3-line-session heights; `ok=false` past the last row. `func (l *listPane) SetCursor(i int)` — clamped cursor set + `ensureVisible()`. Task 3 depends on both.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/listpane_test.go`:

```go
func TestRowAtLineGrouped(t *testing.T) {
	l := newTestPane()
	l.ToggleGroup() // fixture pane starts flat; model default is grouped
	// rows: 0 hdr alpha, 1 s1 (3 lines), 2 hdr beta, 3 s2 (3 lines)
	cases := []struct {
		line int
		row  int
		ok   bool
	}{
		{0, 0, true}, {1, 1, true}, {3, 1, true}, {4, 2, true},
		{5, 3, true}, {7, 3, true}, {8, 0, false}, {-1, 0, false},
	}
	for _, c := range cases {
		row, ok := l.RowAtLine(c.line)
		if ok != c.ok || (ok && row != c.row) {
			t.Errorf("RowAtLine(%d) = (%d,%v), want (%d,%v)", c.line, row, ok, c.row, c.ok)
		}
	}
}

func TestRowAtLineFlat(t *testing.T) {
	l := newTestPane() // flat: rows 0 s1 (lines 0-2), 1 s2 (lines 3-5)
	for line, want := range map[int]int{0: 0, 2: 0, 3: 1, 5: 1} {
		if row, ok := l.RowAtLine(line); !ok || row != want {
			t.Errorf("RowAtLine(%d) = (%d,%v), want (%d,true)", line, row, ok, want)
		}
	}
	if _, ok := l.RowAtLine(6); ok {
		t.Error("RowAtLine(6) should be out of range")
	}
}

func TestRowAtLineFolded(t *testing.T) {
	l := newTestPane()
	l.ToggleGroup()
	l.ToggleFold() // cursor starts on s1 → folds alpha; rows: hdr alpha, hdr beta, s2
	if row, ok := l.RowAtLine(1); !ok || row != 1 {
		t.Errorf("folded: RowAtLine(1) = %d, want 1 (beta header)", row)
	}
	if row, ok := l.RowAtLine(3); !ok || row != 2 {
		t.Errorf("folded: RowAtLine(3) = %d, want 2 (s2 middle line)", row)
	}
}

func TestRowAtLineScrolledSmallPane(t *testing.T) {
	l := listPane{styles: defaultStyles()}
	l.SetSize(50, 3)
	l.SetSessions(testSessions())
	l.ToggleGroup()
	l.SetCursor(3) // s2 occupies lines 5-7
	// ensureVisible keeps title+meta visible (the blank separator may hang
	// off-screen): cursor on s2 (lines 5-7, height 3) → lineOffset 4.
	if l.lineOffset == 0 {
		t.Fatal("setup: expected a scrolled pane")
	}
	if row, ok := l.RowAtLine(0); !ok || row != 2 {
		t.Errorf("scrolled: RowAtLine(0) = %d, want 2 (beta header at line 4)", row)
	}
	if row, ok := l.RowAtLine(1); !ok || row != 3 {
		t.Errorf("scrolled: RowAtLine(1) = %d, want 3 (s2 title line)", row)
	}
}

func TestSetCursorClamps(t *testing.T) {
	l := newTestPane()
	l.SetCursor(1)
	if s, _, ok := l.Selected(); !ok || s.ID != "s2" {
		t.Errorf("SetCursor(1) selected %v, want s2", s.ID)
	}
	l.SetCursor(99) // out of range: no-op
	if s, _, ok := l.Selected(); !ok || s.ID != "s2" {
		t.Errorf("SetCursor(99) moved cursor to %v", s.ID)
	}
	l.SetCursor(-1)
	if s, _, ok := l.Selected(); !ok || s.ID != "s2" {
		t.Errorf("SetCursor(-1) moved cursor to %v", s.ID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd ~/Desktop/ai-sessions-manager && go test ./internal/ui/ -run 'TestRowAtLine|TestSetCursor' -v
```

Expected: compile error — `l.RowAtLine undefined` and `l.SetCursor undefined`.

- [ ] **Step 3: Implement**

Append to `internal/ui/listpane.go` after `MoveCursor`:

```go
// RowAtLine maps a visible content line (0 = the first line currently on
// screen) to the row under it, accounting for scroll offset and the mixed
// one-line-header / three-line-session heights. ok is false below the last
// row or above the top.
func (l *listPane) RowAtLine(visible int) (int, bool) {
	if visible < 0 {
		return 0, false
	}
	line := visible + l.lineOffset
	start, total := l.layout()
	if line >= total {
		return 0, false
	}
	for i := len(start) - 1; i >= 0; i-- {
		if line >= start[i] {
			return i, true
		}
	}
	return 0, false
}

// SetCursor moves the cursor to row i, ignoring out-of-range values.
func (l *listPane) SetCursor(i int) {
	if i < 0 || i >= len(l.rows) {
		return
	}
	l.cursor = i
	l.ensureVisible()
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/ui/ -run 'TestRowAtLine|TestSetCursor' -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/listpane.go internal/ui/listpane_test.go
git commit -m "feat(ui): listPane.RowAtLine and SetCursor for mouse hit-testing"
```

---

### Task 2: Shared geometry helpers + zone resolver + enable mouse reporting

**Files:**
- Modify: `internal/ui/model.go` (`layout()` ~line 166; `Update()` ~line 192)
- Create: `internal/ui/mouse.go`
- Modify: `cmd/sm/main.go:29`
- Test: `internal/ui/mouse_test.go` (new)

**Interfaces:**
- Consumes: `Model.width/height`, `Model.narrow()`.
- Produces (later tasks rely on these exact names):
  - `func (m *Model) bodyHeight() int` — `max(m.height-5, 3)`.
  - `func (m *Model) paneWidths() (listW, previewW int)` — outer pane widths, same math as today's `layout()`.
  - `type zone int` with constants `zoneNone, zoneFilter, zoneList, zonePreview, zoneHelp, zoneDialog` (`zoneDialog` is only used as a double-click-tracking tag from Task 7 on, never returned by `zoneAt`).
  - `func (m *Model) zoneAt(x, y int) (zone, int)` — zone plus 0-based pane content line (meaningful for `zoneList`/`zonePreview`).
  - `func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd)` — stub for now: `return m, nil`.
  - Mouse reporting enabled in `main.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/mouse_test.go`:

```go
package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func wheel(x, y int, up bool) tea.MouseMsg {
	b := tea.MouseButtonWheelDown
	if up {
		b = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b}
}

func TestZoneAt(t *testing.T) {
	m := newTestModel() // 100x30: listW=40, bodyH=25
	cases := []struct {
		name string
		x, y int
		z    zone
		line int
	}{
		{"filter row", 5, 1, zoneFilter, 0},
		{"help row", 50, 29, zoneHelp, 0},
		{"title row", 5, 0, zoneNone, 0},
		{"body top border", 5, 2, zoneNone, 0},
		{"body bottom border", 5, 28, zoneNone, 0},
		{"list first line", 5, 3, zoneList, 0},
		{"list last line", 38, 27, zoneList, 24},
		{"list left border", 0, 5, zoneNone, 0},
		{"list right border", 39, 5, zoneNone, 0},
		{"preview left border", 40, 5, zoneNone, 0},
		{"preview first col", 41, 3, zonePreview, 0},
		{"preview last col", 98, 10, zonePreview, 7},
		{"preview right border", 99, 10, zoneNone, 0},
	}
	for _, c := range cases {
		z, line := m.zoneAt(c.x, c.y)
		if z != c.z || line != c.line {
			t.Errorf("%s: zoneAt(%d,%d) = (%v,%d), want (%v,%d)", c.name, c.x, c.y, z, line, c.z, c.line)
		}
	}
}

func TestZoneAtNarrow(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 30}) // narrow: listW=68
	m = m2.(Model)
	if z, _ := m.zoneAt(50, 5); z != zoneList {
		t.Errorf("narrow: (50,5) = %v, want zoneList", z)
	}
	if z, _ := m.zoneAt(66, 5); z != zoneList {
		t.Errorf("narrow: (66,5) = %v, want zoneList (last content col)", z)
	}
	if z, _ := m.zoneAt(67, 5); z != zoneNone {
		t.Errorf("narrow: (67,5) = %v, want zoneNone (border)", z)
	}
	if z, _ := m.zoneAt(69, 5); z != zoneNone {
		t.Errorf("narrow: (69,5) = %v, want zoneNone (no preview pane)", z)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ui/ -run TestZoneAt -v
```

Expected: compile error — `zone`/`zoneAt` undefined.

- [ ] **Step 3: Implement**

Create `internal/ui/mouse.go`:

```go
package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// zone is an interactive region of the screen. zoneDialog is never returned
// by zoneAt; it tags dialog rows in the double-click tracker.
type zone int

const (
	zoneNone zone = iota
	zoneFilter
	zoneList
	zonePreview
	zoneHelp
	zoneDialog
)

// zoneAt resolves screen coordinates to the region under them. Geometry
// mirrors layout()/View(): row 0 title, row 1 filter bar, row 2 body top
// border, pane content rows [3, 2+bodyH], help bar on the last row. The
// second return is the 0-based content line inside the pane (zoneList /
// zonePreview only).
func (m *Model) zoneAt(x, y int) (zone, int) {
	switch {
	case y == 1:
		return zoneFilter, 0
	case y == m.height-1:
		return zoneHelp, 0
	}
	bodyH := m.bodyHeight()
	if y < 3 || y > 2+bodyH {
		return zoneNone, 0
	}
	line := y - 3
	listW, previewW := m.paneWidths()
	if x >= 1 && x <= listW-2 {
		return zoneList, line
	}
	if !m.narrow() && x >= listW+1 && x <= listW+previewW {
		return zonePreview, line
	}
	return zoneNone, 0
}

// handleMouse dispatches mouse events. Built out task by task; keyboard
// paths are never touched.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	return m, nil
}
```

In `internal/ui/model.go`, replace the body-height and width math inside `layout()` with the shared helpers, adding the two methods right above `layout()`:

```go
// bodyHeight is the pane content height: total height minus title, filter,
// help, and the panes' top/bottom border rows.
func (m *Model) bodyHeight() int {
	h := m.height - 5
	if h < 3 {
		return 3
	}
	return h
}

// paneWidths returns the outer widths of the list and preview panes.
// layout() and mouse hit-testing must agree on these, so they live here.
func (m *Model) paneWidths() (listW, previewW int) {
	listW = m.width * 2 / 5
	if listW < 20 {
		listW = 20
	}
	if m.narrow() {
		listW = m.width - 2
	}
	previewW = m.width - listW - 2
	if previewW < 10 {
		previewW = 10
	}
	return listW, previewW
}
```

and `layout()` becomes:

```go
func (m *Model) layout() {
	bodyH := m.bodyHeight()
	listW, previewW := m.paneWidths()
	m.list.SetSize(listW-2, bodyH)
	if !m.ready {
		m.preview = viewport.New(previewW, bodyH)
		m.ready = true
	} else {
		m.preview.Width = previewW
		m.preview.Height = bodyH
	}
}
```

In `Model.Update`, add after the `case tea.KeyMsg:` clause:

```go
	case tea.MouseMsg:
		return m.handleMouse(msg)
```

In `cmd/sm/main.go:29`:

```go
	p := tea.NewProgram(ui.New(*projectsDir), tea.WithAltScreen(), tea.WithMouseCellMotion())
```

- [ ] **Step 4: Run tests + build**

```bash
gofmt -l . && go vet ./... && go test ./internal/ui/ -run TestZoneAt -v && go build ./...
```

Expected: `gofmt -l` prints nothing, both tests PASS, build clean. Run the full suite once to prove no regressions: `go test ./...` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/sm/main.go internal/ui/model.go internal/ui/mouse.go internal/ui/mouse_test.go
git commit -m "feat(ui): mouse reporting, shared pane geometry, zone resolver"
```

---

### Task 3: List clicks (select, fold) + wheel + focus-follows-click

**Files:**
- Modify: `internal/ui/mouse.go`
- Test: `internal/ui/mouse_test.go`

**Interfaces:**
- Consumes: `zoneAt`, `RowAtLine`, `SetCursor`, `OnHeader`, `ToggleFold`, `MoveCursor`, `loadTranscriptCmd`, `startResume` (existing), `m.focus`/`focusList`/`focusPreview`/`focusFilter`, `m.filterInput`.
- Produces: working `handleMouse` for the non-dialog screen; `func (m Model) clickList(line int) (tea.Model, tea.Cmd)`. The `m.dialog != dialogNone` branch stays `return m, nil` until Task 7. Double-click comes in Task 4 — for now every session click just selects.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/mouse_test.go`:

```go
func TestClickSelectsSession(t *testing.T) {
	m := newTestModel()
	m2, cmd := m.Update(click(5, 8)) // s2's title line
	m = m2.(Model)
	if s, _, ok := m.list.Selected(); !ok || s.ID != "s2" {
		t.Fatalf("selected %v, want s2", s.ID)
	}
	if cmd == nil {
		t.Error("selecting must reload the preview")
	}
}

func TestClickHeaderFolds(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(5, 3)) // alpha header
	m = m2.(Model)
	if !m.list.folded["alpha"] {
		t.Fatal("clicking a header must fold its project")
	}
	if !m.list.OnHeader() {
		t.Error("cursor should park on the folded header")
	}
	m2, _ = m.Update(click(5, 3)) // click again: unfold
	m = m2.(Model)
	if m.list.folded["alpha"] {
		t.Error("second click must unfold")
	}
}

func TestClickBlankBelowListIsNoop(t *testing.T) {
	m := newTestModel()
	before, _, _ := m.list.Selected()
	m2, _ := m.Update(click(5, 20)) // inside the pane, below the last row
	m = m2.(Model)
	after, _, ok := m.list.Selected()
	if !ok || after.ID != before.ID {
		t.Errorf("selection changed from %v to %v on a blank-area click", before.ID, after.ID)
	}
}

func TestWheelOverListMovesSelection(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(wheel(5, 5, false)) // down: s1 → beta header
	m = m2.(Model)
	if !m.list.OnHeader() {
		t.Fatal("wheel down should move cursor to the beta header row")
	}
	m2, _ = m.Update(wheel(5, 5, false)) // down again: → s2
	m = m2.(Model)
	if s, _, ok := m.list.Selected(); !ok || s.ID != "s2" {
		t.Fatalf("second wheel down landed on %v, want s2", s.ID)
	}
	m2, _ = m.Update(wheel(5, 5, true)) // up: → header
	m = m2.(Model)
	m2, cmd := m.Update(wheel(5, 5, true)) // up again: → s1
	m = m2.(Model)
	if s, _, ok := m.list.Selected(); !ok || s.ID != "s1" {
		t.Errorf("wheel up landed on %v, want s1", s.ID)
	}
	// landing on a session (not a header) must reload the preview
	if cmd == nil {
		t.Error("wheeling onto a session must reload the preview")
	}
}

func TestClickListReturnsFocus(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus preview
	m = m2.(Model)
	m2, _ = m.Update(click(5, 8))
	m = m2.(Model)
	if m.focus != focusList {
		t.Errorf("focus = %v, want focusList after clicking the list", m.focus)
	}

	m2, _ = m.Update(key("/")) // focus filter
	m = m2.(Model)
	m2, _ = m.Update(click(5, 8))
	m = m2.(Model)
	if m.focus != focusList || m.filterInput.Focused() {
		t.Error("clicking the list while filtering must blur the filter and refocus the list")
	}
}
```

(`m.list.folded["alpha"]` — project labels come from `store.Session.Project()`, the last path component of the CWD: `/x/alpha` → `alpha`.)

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ui/ -run 'TestClick|TestWheel' -v
```

Expected: FAIL — `handleMouse` is a stub, selection/fold/focus assertions all fail.

- [ ] **Step 3: Implement**

Replace the `handleMouse` stub in `internal/ui/mouse.go` and add `clickList`:

```go
// handleMouse dispatches mouse events. Only left presses and wheel ticks
// act; motion, release, and other buttons are ignored. Keyboard paths are
// never touched.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.dialog != dialogNone {
		return m, nil // dialog mouse support lands in a later task
	}
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	z, line := m.zoneAt(msg.X, msg.Y)

	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		switch z {
		case zoneList:
			delta := 1
			if msg.Button == tea.MouseButtonWheelUp {
				delta = -1
			}
			m.list.MoveCursor(delta)
			return m, m.loadTranscriptCmd()
		case zonePreview:
			var cmd tea.Cmd
			m.preview, cmd = m.preview.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	// A click outside the filter bar while typing in it puts the keyboard
	// back on the list; the filter text stays (same as pressing enter).
	if m.focus == focusFilter && z != zoneFilter {
		m.filterInput.Blur()
		m.focus = focusList
	}

	switch z {
	case zoneList:
		m.focus = focusList
		return m.clickList(line)
	case zonePreview:
		m.focus = focusPreview
		return m, nil
	case zoneFilter:
		m.focus = focusFilter
		m.filterInput.Focus()
		return m, nil
	case zoneHelp:
		return m, nil // clickable help bar lands in a later task
	}
	return m, nil
}

// clickList selects the row under a click; header rows fold instead.
func (m Model) clickList(line int) (tea.Model, tea.Cmd) {
	row, ok := m.list.RowAtLine(line)
	if !ok {
		return m, nil
	}
	m.list.SetCursor(row)
	if m.list.OnHeader() {
		m.list.ToggleFold()
		return m, m.loadTranscriptCmd()
	}
	return m, m.loadTranscriptCmd()
}
```

Note: the preview-wheel branch is exercised in Task 5's test; it rides along here because splitting the wheel `switch` across tasks would churn the same lines twice.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/ui/ -v
```

Expected: all PASS (new and pre-existing).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mouse.go internal/ui/mouse_test.go
git commit -m "feat(ui): click to select/fold, wheel scrolling, focus follows click"
```

---

### Task 4: Double-click resumes (injected clock)

**Files:**
- Modify: `internal/ui/model.go` (Model struct ~line 55, `New` ~line 85)
- Modify: `internal/ui/mouse.go`
- Test: `internal/ui/mouse_test.go`

**Interfaces:**
- Consumes: `startResume` (existing), `resumeRecorder`/`modelWithRealCWD` test helpers (existing in `actions_test.go`).
- Produces (Tasks 7–8 rely on these):
  - Model fields `lastClickZone zone`, `lastClickRow int`, `lastClickAt time.Time`, `now func() time.Time`; `New()` sets `now: time.Now, lastClickRow: -1`.
  - `const doubleClickWindow = 400 * time.Millisecond`
  - `func (m *Model) isDoubleClick(z zone, row int) bool`
  - `func (m *Model) recordClick(z zone, row int)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/mouse_test.go` (add `"time"` to its imports):

```go
func TestDoubleClickResumes(t *testing.T) {
	m, dir := modelWithRealCWD(t)
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
	t0 := time.Now()
	m.now = func() time.Time { return t0 }
	m2, _ := m.Update(click(5, 4)) // s1 title line
	m = m2.(Model)
	m.now = func() time.Time { return t0.Add(200 * time.Millisecond) }
	m2, cmd := m.Update(click(5, 4))
	m = m2.(Model)
	if cmd == nil || rec.dir != dir {
		t.Fatalf("double-click: resume dir = %q, want %q (cmd nil: %v)", rec.dir, dir, cmd == nil)
	}
	if len(rec.args) != 2 || rec.args[0] != "--resume" || rec.args[1] != "s1" {
		t.Errorf("args = %v, want [--resume s1]", rec.args)
	}
}

func TestSlowSecondClickDoesNotResume(t *testing.T) {
	m, _ := modelWithRealCWD(t)
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
	t0 := time.Now()
	m.now = func() time.Time { return t0 }
	m2, _ := m.Update(click(5, 4))
	m = m2.(Model)
	m.now = func() time.Time { return t0.Add(time.Second) }
	m2, _ = m.Update(click(5, 4))
	m = m2.(Model)
	if rec.dir != "" {
		t.Error("a slow second click must not resume")
	}
}

func TestDoubleClickDifferentRowsDoesNotResume(t *testing.T) {
	m, _ := modelWithRealCWD(t)
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
	t0 := time.Now()
	m.now = func() time.Time { return t0 }
	m2, _ := m.Update(click(5, 4)) // s1
	m = m2.(Model)
	m.now = func() time.Time { return t0.Add(100 * time.Millisecond) }
	m2, _ = m.Update(click(5, 8)) // s2 — fast, but a different row
	m = m2.(Model)
	if rec.dir != "" {
		t.Error("fast clicks on different rows must not resume")
	}
	if s, _, _ := m.list.Selected(); s.ID != "s2" {
		t.Errorf("second click should still select s2, got %v", s.ID)
	}
}

func TestHeaderClickResetsDoubleClick(t *testing.T) {
	m, _ := modelWithRealCWD(t)
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
	t0 := time.Now()
	m.now = func() time.Time { return t0 }
	m2, _ := m.Update(click(5, 4)) // s1
	m = m2.(Model)
	m2, _ = m.Update(click(5, 3)) // alpha header (folds; s1 row index shifts)
	m = m2.(Model)
	m2, _ = m.Update(click(5, 3)) // unfold
	m = m2.(Model)
	m.now = func() time.Time { return t0.Add(300 * time.Millisecond) }
	m2, _ = m.Update(click(5, 4)) // s1 again, still inside the window
	m = m2.(Model)
	if rec.dir != "" {
		t.Error("a header click in between must reset double-click tracking")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ui/ -run 'TestDoubleClick|TestSlowSecond|TestHeaderClickResets' -v
```

Expected: compile error — `m.now` undefined.

- [ ] **Step 3: Implement**

`internal/ui/model.go` — add to the `Model` struct after `runClaude`:

```go
	// mouse double-click tracking; now is injected for tests
	lastClickZone zone
	lastClickRow  int
	lastClickAt   time.Time
	now           func() time.Time
```

(add `"time"` to model.go's imports), and in `New()` add to the returned literal:

```go
		lastClickRow:  -1,
		now:           time.Now,
```

`internal/ui/mouse.go` — add near the top (add `"time"` import):

```go
// doubleClickWindow is how close two presses on the same row must be to
// count as a double-click.
const doubleClickWindow = 400 * time.Millisecond

func (m *Model) isDoubleClick(z zone, row int) bool {
	return z == m.lastClickZone && row == m.lastClickRow &&
		m.now().Sub(m.lastClickAt) <= doubleClickWindow
}

func (m *Model) recordClick(z zone, row int) {
	m.lastClickZone, m.lastClickRow, m.lastClickAt = z, row, m.now()
}
```

and rewrite `clickList`:

```go
// clickList selects the row under a click; header rows fold instead, and a
// second click on the same session within doubleClickWindow resumes it.
func (m Model) clickList(line int) (tea.Model, tea.Cmd) {
	row, ok := m.list.RowAtLine(line)
	if !ok {
		return m, nil
	}
	m.list.SetCursor(row)
	if m.list.OnHeader() {
		m.lastClickRow = -1 // folding renumbers rows; stale indexes must not pair
		m.list.ToggleFold()
		return m, m.loadTranscriptCmd()
	}
	if m.isDoubleClick(zoneList, row) {
		m.lastClickRow = -1
		return m.startResume()
	}
	m.recordClick(zoneList, row)
	return m, m.loadTranscriptCmd()
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/ui/ -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/mouse.go internal/ui/mouse_test.go
git commit -m "feat(ui): double-click resumes a session (400ms window, injected clock)"
```

---

### Task 5: Preview focus + wheel-over-preview + filter-bar click

**Files:**
- Test: `internal/ui/mouse_test.go` (the implementation already landed in Task 3's `handleMouse`; this task proves it and pins it)

**Interfaces:**
- Consumes: `handleMouse` zonePreview/zoneFilter branches (Task 3), bubbles viewport wheel handling (`MouseWheelEnabled` defaults true, `MouseWheelDelta` = 3).
- Produces: regression tests only; no new symbols.

- [ ] **Step 1: Write the tests**

Append to `internal/ui/mouse_test.go` (add `"strings"` to its imports):

```go
func TestClickPreviewFocusesIt(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(50, 10))
	m = m2.(Model)
	if m.focus != focusPreview {
		t.Errorf("focus = %v, want focusPreview", m.focus)
	}
}

func TestWheelOverPreviewScrollsWithoutFocus(t *testing.T) {
	m := newTestModel()
	m.preview.SetContent(strings.Repeat("x\n", 100))
	m.preview.GotoTop()
	m2, _ := m.Update(wheel(50, 10, false))
	m = m2.(Model)
	if m.preview.YOffset != 3 {
		t.Errorf("YOffset = %d, want 3 (one wheel tick)", m.preview.YOffset)
	}
	if m.focus != focusList {
		t.Error("wheel over the preview must not steal focus")
	}
	m2, _ = m.Update(wheel(50, 10, true))
	m = m2.(Model)
	if m.preview.YOffset != 0 {
		t.Errorf("YOffset = %d, want 0 after wheeling back up", m.preview.YOffset)
	}
}

func TestClickFilterBarFocusesFilter(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(5, 1))
	m = m2.(Model)
	if m.focus != focusFilter || !m.filterInput.Focused() {
		t.Fatalf("focus = %v focused=%v, want filter focused", m.focus, m.filterInput.Focused())
	}
	// typed keys must now go to the filter
	for _, r := range "backup" {
		m2, _ = m.Update(key(string(r)))
		m = m2.(Model)
	}
	if s, _, ok := m.list.Selected(); !ok || s.ID != "s2" {
		t.Errorf("typing after a filter-bar click selected %v, want s2", s.ID)
	}
}

func TestNonLeftPressesIgnored(t *testing.T) {
	m := newTestModel()
	before, _, _ := m.list.Selected()
	for _, msg := range []tea.MouseMsg{
		{X: 5, Y: 8, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft},
		{X: 5, Y: 8, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone},
		{X: 5, Y: 8, Action: tea.MouseActionPress, Button: tea.MouseButtonRight},
	} {
		m2, _ := m.Update(msg)
		m = m2.(Model)
	}
	if s, _, ok := m.list.Selected(); !ok || s.ID != before.ID {
		t.Error("release/motion/right-click must not change the selection")
	}
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./internal/ui/ -run 'TestClickPreview|TestWheelOverPreview|TestClickFilterBar|TestNonLeftPresses' -v
```

Expected: all PASS immediately (behavior landed in Task 3). If any fail, the Task 3 wiring is wrong — fix `handleMouse`, not the tests.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/mouse_test.go
git commit -m "test(ui): pin preview focus/wheel and filter-bar click behavior"
```

---

### Task 6: Clickable help bar

**Files:**
- Modify: `internal/ui/model.go` (`View()` ~line 558)
- Modify: `internal/ui/mouse.go`
- Test: `internal/ui/mouse_test.go`

**Interfaces:**
- Consumes: `handleKey` (existing), `zoneHelp` branch of `handleMouse`.
- Produces:
  - `type helpItem struct { label string; key tea.KeyMsg }`
  - `var helpBar = []helpItem{...}` (package-level, in mouse.go)
  - `func runeKey(s string) tea.KeyMsg` (production twin of the test-only `key()` helper)
  - `func (m Model) clickHelp(x int) (tea.Model, tea.Cmd)`
  - `View()` renders the help bar from `helpBar` — byte-identical output.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/mouse_test.go`. Help-bar x-ranges at any width (leading space, labels joined by two spaces): `↵ resume`→[1,8], `tab focus`→[11,19], `n new`→[22,26], `d delete`→[29,36], `/ filter`→[39,46], `g group`→[49,55], `space fold`→[58,67], `e empty`→[70,76], `r rescan`→[79,86], `q quit`→[89,94].

```go
func TestHelpBarTextUnchanged(t *testing.T) {
	m := newTestModel()
	want := " ↵ resume  tab focus  n new  d delete  / filter  g group  space fold  e empty  r rescan  q quit"
	if !strings.Contains(m.View(), want) {
		t.Fatal("help bar text changed — it must stay byte-identical")
	}
}

func TestClickHelpBarTriggersAction(t *testing.T) {
	m := newTestModel()
	if !m.list.groupByProject {
		t.Fatal("setup: expected grouped mode")
	}
	m2, _ := m.Update(click(50, 29)) // inside "g group" [49,55]
	m = m2.(Model)
	if m.list.groupByProject {
		t.Error("clicking 'g group' must toggle grouping")
	}
	m2, _ = m.Update(click(60, 29)) // inside "space fold" [58,67]
	m = m2.(Model)
	// flat mode: fold is a no-op; regroup and fold via clicks
	m2, _ = m.Update(click(50, 29))
	m = m2.(Model)
	m2, _ = m.Update(click(60, 29))
	m = m2.(Model)
	if len(m.list.folded) == 0 {
		t.Error("clicking 'space fold' must fold the current project")
	}
}

func TestClickHelpBarGapIsNoop(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(47, 29)) // gap between "/ filter" and "g group"
	m = m2.(Model)
	if !m.list.groupByProject {
		t.Error("a click on a help-bar gap must do nothing")
	}
}

func TestClickHelpQuitReturnsQuit(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(click(90, 29)) // inside "q quit" [89,94]
	if cmd == nil {
		t.Fatal("clicking 'q quit' must return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("the command must be tea.Quit")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ui/ -run TestClickHelp -v
```

Expected: FAIL — help clicks are no-ops (`TestHelpBarTextUnchanged` passes already; the click tests fail).

- [ ] **Step 3: Implement**

`internal/ui/mouse.go` — add (plus `"strings"` and `"github.com/charmbracelet/lipgloss"` imports):

```go
// runeKey builds the KeyMsg for a printable key, so help-bar buttons reuse
// the exact key-handling paths.
func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// helpItem is one clickable segment of the help bar.
type helpItem struct {
	label string
	key   tea.KeyMsg
}

// helpBar drives both the rendered help line and its hit-testing; the two
// can't drift because they read the same table.
var helpBar = []helpItem{
	{"↵ resume", tea.KeyMsg{Type: tea.KeyEnter}},
	{"tab focus", tea.KeyMsg{Type: tea.KeyTab}},
	{"n new", runeKey("n")},
	{"d delete", runeKey("d")},
	{"/ filter", runeKey("/")},
	{"g group", runeKey("g")},
	{"space fold", runeKey(" ")},
	{"e empty", runeKey("e")},
	{"r rescan", runeKey("r")},
	{"q quit", runeKey("q")},
}

// helpLine renders the help bar's text (unstyled).
func helpLine() string {
	parts := make([]string, len(helpBar))
	for i, it := range helpBar {
		parts[i] = it.label
	}
	return " " + strings.Join(parts, "  ")
}

// clickHelp maps an x coordinate on the help bar to its segment and feeds
// that segment's key through the normal key path.
func (m Model) clickHelp(x int) (tea.Model, tea.Cmd) {
	pos := 1 // leading space
	for _, it := range helpBar {
		w := lipgloss.Width(it.label)
		if x >= pos && x < pos+w {
			return m.handleKey(it.key)
		}
		pos += w + 2
	}
	return m, nil
}
```

In `handleMouse`, replace the `case zoneHelp:` no-op with:

```go
	case zoneHelp:
		// Buttons act from list focus: without this, a synthesized key would
		// be eaten by whichever pane holds focus (e.g. "d" scrolls a focused
		// preview half a page instead of opening the delete dialog).
		m.focus = focusList
		return m.clickHelp(msg.X)
```

and add a regression test:

```go
func TestClickHelpWhilePreviewFocused(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(50, 10)) // focus the preview
	m = m2.(Model)
	if m.focus != focusPreview {
		t.Fatal("setup: preview should be focused")
	}
	m2, _ = m.Update(click(30, 29)) // "d delete" [29,36]
	m = m2.(Model)
	if m.dialog != dialogDelete {
		t.Errorf("dialog = %v, want dialogDelete (button must act on the list, not scroll the preview)", m.dialog)
	}
}
```

In `model.go`'s `View()`, replace the hardcoded help line:

```go
	help := m.st.Help.Render(helpLine())
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/ui/ -v
```

Expected: all PASS, including `TestHelpBarTextUnchanged` (proves the refactor kept the bytes).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/mouse.go internal/ui/mouse_test.go
git commit -m "feat(ui): clickable help bar driven by a shared segment table"
```

---

### Task 7: Dialog mouse — geometry, delete buttons, error dismiss, click-outside

**Files:**
- Modify: `internal/ui/mouse.go`
- Test: `internal/ui/mouse_test.go`

**Interfaces:**
- Consumes: `dialogView()` (existing), `handleDialogKey` (existing), `m.st.DialogBox` (RoundedBorder + `Padding(1,2)` → content offset: left `GetBorderLeftSize()+GetPaddingLeft()` = 3, top `GetBorderTopSize()+GetPaddingTop()` = 2), `isDoubleClick`/`recordClick` (Task 4), lipgloss `Place(Center)` gap rounding: leading gap = `gap - round(gap*0.5)` = `floor(gap/2)` (verified against lipgloss v1.1.0 `position.go`).
- Produces:
  - `func (m Model) dialogOrigin(box string) (x0, y0 int)` — top-left screen cell of the centered dialog: `x0 = (m.width - lipgloss.Width(box)) / 2`, `y0 = 2 + (m.height - 3 - lipgloss.Height(box)) / 2` (Go integer division = floor for these non-negative gaps). Task 8 reuses it.
  - `func (m Model) handleDialogMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd)` — full error/delete handling; pickDir branch completed in Task 8.
  - Delete-dialog content layout (inside padding): line 0 `Move session to trash?`, 1 blank, 2 title, 3 blank, 4 `y confirm · n cancel` with `y confirm` at columns [0,8] and `n cancel` at [12,19].

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/mouse_test.go`:

```go
// dialogContentOrigin returns the screen cell of the dialog's content (0,0),
// i.e. the box origin shifted past border and padding.
func dialogContentOrigin(m Model) (int, int) {
	x0, y0 := m.dialogOrigin(m.dialogView())
	return x0 + m.st.DialogBox.GetBorderLeftSize() + m.st.DialogBox.GetPaddingLeft(),
		y0 + m.st.DialogBox.GetBorderTopSize() + m.st.DialogBox.GetPaddingTop()
}

func TestDialogOriginMatchesRender(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(key("d")) // delete dialog for s1
	m = m2.(Model)
	x0, y0 := m.dialogOrigin(m.dialogView())
	lines := strings.Split(m.View(), "\n")
	for y, ln := range lines {
		if x := strings.Index(ln, "╭"); x >= 0 {
			// bytes == cells here: everything left of the corner is spaces
			if x != x0 || y != y0 {
				t.Fatalf("corner rendered at (%d,%d), dialogOrigin says (%d,%d)", x, y, x0, y0)
			}
			return
		}
	}
	t.Fatal("no dialog border corner found in the rendered view")
}

func TestDeleteDialogButtons(t *testing.T) {
	m := newTestModel()
	trashed := ""
	m.trashFn = func(_ string, s store.Session) (string, error) {
		trashed = s.ID
		return "/trash/" + s.ID, nil
	}
	m2, _ := m.Update(key("d"))
	m = m2.(Model)
	cx, cy := dialogContentOrigin(m)
	m2, _ = m.Update(click(cx+4, cy+4)) // inside "y confirm" [0,8]
	m = m2.(Model)
	if trashed != "s1" || m.dialog != dialogNone {
		t.Fatalf("y-button: trashed=%q dialog=%v, want s1 trashed and dialog closed", trashed, m.dialog)
	}

	// n cancel
	m2, _ = m.Update(key("d"))
	m = m2.(Model)
	trashed = ""
	cx, cy = dialogContentOrigin(m)
	m2, _ = m.Update(click(cx+14, cy+4)) // inside "n cancel" [12,19]
	m = m2.(Model)
	if trashed != "" || m.dialog != dialogNone {
		t.Errorf("n-button: trashed=%q dialog=%v, want nothing trashed and dialog closed", trashed, m.dialog)
	}
}

func TestDeleteDialogClickOutsideCancels(t *testing.T) {
	m := newTestModel()
	m.trashFn = func(string, store.Session) (string, error) {
		t.Error("trashFn must not run on an outside click")
		return "", nil
	}
	m2, _ := m.Update(key("d"))
	m = m2.(Model)
	x0, y0 := m.dialogOrigin(m.dialogView())
	m2, _ = m.Update(click(x0-2, y0))
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Error("clicking outside the delete dialog must cancel it")
	}
}

func TestDeleteDialogDeadZoneClickKeepsDialog(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(key("d"))
	m = m2.(Model)
	cx, cy := dialogContentOrigin(m)
	m2, _ = m.Update(click(cx+10, cy+4)) // the " · " separator between buttons
	m = m2.(Model)
	if m.dialog != dialogDelete {
		t.Error("clicking between the buttons must keep the dialog open")
	}
}

func TestErrorDialogClickDismisses(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(claudeMissingMsg{})
	m = m2.(Model)
	if m.dialog != dialogError {
		t.Fatal("setup: expected the error dialog")
	}
	m2, _ = m.Update(click(3, 3)) // anywhere
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Error("any click must dismiss the error dialog")
	}
}
```

(`store` is already imported by other files in the package; add `"github.com/dukechain2333/ai-sessions-manager/internal/store"` to mouse_test.go's imports.)

Byte-index-equals-column in `TestDialogOriginMatchesRender` holds because under plain `go test` there is no TTY, so lipgloss renders with the Ascii color profile — no escape codes. If that test ever fails with offset positions, a color profile leaked in; add a `TestMain` calling `lipgloss.SetColorProfile(termenv.Ascii)` (import `github.com/muesli/termenv`, already an indirect dependency) rather than weakening the assertion.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ui/ -run 'TestDialog|TestDeleteDialog|TestErrorDialog' -v
```

Expected: compile error — `dialogOrigin` undefined.

- [ ] **Step 3: Implement**

`internal/ui/mouse.go`:

```go
// dialogOrigin returns the top-left screen cell of the centered dialog box.
// It must match lipgloss.Place(Center, Center): the leading gap is
// gap - round(gap/2), i.e. floor(gap/2); the box area starts at row 2
// (title + filter rows above it). Pinned by TestDialogOriginMatchesRender.
func (m Model) dialogOrigin(box string) (x0, y0 int) {
	x0 = (m.width - lipgloss.Width(box)) / 2
	y0 = 2 + (m.height-3-lipgloss.Height(box))/2
	return x0, y0
}

// handleDialogMouse gives the active dialog the same mouse affordances as
// the main screen: buttons are clickable, wheel moves the dir cursor, and
// clicking outside is a non-destructive cancel.
func (m Model) handleDialogMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		if m.dialog == dialogPickDir {
			k := tea.KeyMsg{Type: tea.KeyDown}
			if msg.Button == tea.MouseButtonWheelUp {
				k = tea.KeyMsg{Type: tea.KeyUp}
			}
			return m.handleDialogKey(k)
		}
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	box := m.dialogView()
	x0, y0 := m.dialogOrigin(box)
	inside := msg.X >= x0 && msg.X < x0+lipgloss.Width(box) &&
		msg.Y >= y0 && msg.Y < y0+lipgloss.Height(box)
	cx := msg.X - x0 - m.st.DialogBox.GetBorderLeftSize() - m.st.DialogBox.GetPaddingLeft()
	cy := msg.Y - y0 - m.st.DialogBox.GetBorderTopSize() - m.st.DialogBox.GetPaddingTop()

	switch m.dialog {
	case dialogError:
		// "press any key" — any click counts
		return m.handleDialogKey(tea.KeyMsg{Type: tea.KeyEsc})

	case dialogDelete:
		if !inside {
			return m.handleDialogKey(runeKey("n"))
		}
		// content: 0 question, 1 blank, 2 title, 3 blank, 4 "y confirm · n cancel"
		if cy == 4 {
			switch {
			case cx >= 0 && cx <= 8: // "y confirm"
				return m.handleDialogKey(runeKey("y"))
			case cx >= 12 && cx <= 19: // "n cancel"
				return m.handleDialogKey(runeKey("n"))
			}
		}
		return m, nil

	case dialogPickDir:
		if !inside {
			return m.handleDialogKey(tea.KeyMsg{Type: tea.KeyEsc})
		}
		return m.clickPickDir(cy)
	}
	return m, nil
}

// clickPickDir lands in Task 8; the stub keeps this task compiling.
func (m Model) clickPickDir(cy int) (tea.Model, tea.Cmd) {
	return m, nil
}
```

In `handleMouse`, replace the dialog guard from Task 3:

```go
	if m.dialog != dialogNone {
		return m.handleDialogMouse(msg)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/ui/ -v
```

Expected: all PASS. If `TestDialogOriginMatchesRender` fails with an off-by-one, the lipgloss rounding assumption broke — fix `dialogOrigin`'s formula to match the corner the test found, never the test.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mouse.go internal/ui/mouse_test.go
git commit -m "feat(ui): clickable dialogs — delete buttons, error dismiss, outside-click cancel"
```

---

### Task 8: Dir-picker rows — click, double-click, wheel

**Files:**
- Modify: `internal/ui/mouse.go` (fill in `clickPickDir`)
- Test: `internal/ui/mouse_test.go`

**Interfaces:**
- Consumes: `dialogOrigin`, `dialogContentOrigin` (test helper, Task 7), `isDoubleClick`/`recordClick` (Task 4), `handleDialogKey` (existing), `m.dirs`/`m.dirCursor`.
- Produces: final `func (m Model) clickPickDir(cy int) (tea.Model, tea.Cmd)`. PickDir content layout: line 0 header, 1 blank, rows `2..2+len(dirs)-1`, then blank/input/blank/help — so dir index `i = cy - 2`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/mouse_test.go`:

```go
func pickerModel(t *testing.T) (Model, string, string, *resumeRecorder) {
	t.Helper()
	dirA, dirB := t.TempDir(), t.TempDir()
	m := newTestModel()
	m.list.sessions[0].CWD = dirA // s1, most recent → dirs[0]
	m.list.sessions[1].CWD = dirB // s2 → dirs[1]
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
	m2, _ := m.Update(key("n"))
	m = m2.(Model)
	if m.dialog != dialogPickDir || len(m.dirs) != 2 || m.dirs[1] != dirB {
		t.Fatalf("setup: dialog=%v dirs=%v", m.dialog, m.dirs)
	}
	return m, dirA, dirB, rec
}

func TestPickDirClickSelectsRow(t *testing.T) {
	m, _, _, _ := pickerModel(t)
	cx, cy := dialogContentOrigin(m)
	m2, _ := m.Update(click(cx+1, cy+3)) // content line 3 = dir row 1
	m = m2.(Model)
	if m.dirCursor != 1 {
		t.Errorf("dirCursor = %d, want 1", m.dirCursor)
	}
	if m.dialog != dialogPickDir {
		t.Error("a single click must not confirm")
	}
}

func TestPickDirDoubleClickConfirms(t *testing.T) {
	m, _, dirB, rec := pickerModel(t)
	t0 := time.Now()
	m.now = func() time.Time { return t0 }
	cx, cy := dialogContentOrigin(m)
	m2, _ := m.Update(click(cx+1, cy+3))
	m = m2.(Model)
	m.now = func() time.Time { return t0.Add(150 * time.Millisecond) }
	m2, _ = m.Update(click(cx+1, cy+3))
	m = m2.(Model)
	if rec.dir != dirB || len(rec.args) != 0 {
		t.Errorf("double-click confirm: dir=%q args=%v, want %q []", rec.dir, rec.args, dirB)
	}
	if m.dialog != dialogNone {
		t.Error("confirming must close the dialog")
	}
}

func TestPickDirWheelMovesCursor(t *testing.T) {
	m, _, _, _ := pickerModel(t)
	x0, y0 := m.dialogOrigin(m.dialogView())
	m2, _ := m.Update(wheel(x0+2, y0+2, false))
	m = m2.(Model)
	if m.dirCursor != 1 {
		t.Errorf("wheel down: dirCursor = %d, want 1", m.dirCursor)
	}
	m2, _ = m.Update(wheel(x0+2, y0+2, true))
	m = m2.(Model)
	if m.dirCursor != 0 {
		t.Errorf("wheel up: dirCursor = %d, want 0", m.dirCursor)
	}
}

func TestPickDirClickOutsideCancels(t *testing.T) {
	m, _, _, rec := pickerModel(t)
	m2, _ := m.Update(click(1, 3)) // far outside the centered box
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Error("clicking outside the picker must cancel it")
	}
	if rec.dir != "" {
		t.Error("cancel must not launch claude")
	}
}

func TestPickDirClickNonRowIsNoop(t *testing.T) {
	m, _, _, _ := pickerModel(t)
	cx, cy := dialogContentOrigin(m)
	m2, _ := m.Update(click(cx+1, cy)) // header line
	m = m2.(Model)
	if m.dialog != dialogPickDir || m.dirCursor != 0 {
		t.Errorf("header click: dialog=%v dirCursor=%d, want open dialog, cursor 0", m.dialog, m.dirCursor)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ui/ -run TestPickDir -v
```

Expected: `TestPickDirClickSelectsRow`, `TestPickDirDoubleClickConfirms` FAIL (stub `clickPickDir` does nothing). Wheel and outside-click tests PASS already (Task 7 wired them).

- [ ] **Step 3: Implement**

Replace the `clickPickDir` stub in `internal/ui/mouse.go`:

```go
// clickPickDir selects the directory row under a click; a second click on
// the same row within doubleClickWindow confirms it (same as enter).
func (m Model) clickPickDir(cy int) (tea.Model, tea.Cmd) {
	// content: 0 header, 1 blank, 2..2+len(dirs)-1 dir rows, then input/help
	i := cy - 2
	if i < 0 || i >= len(m.dirs) {
		return m, nil
	}
	m.dirCursor = i
	if m.isDoubleClick(zoneDialog, i) {
		m.lastClickRow = -1
		return m.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	m.recordClick(zoneDialog, i)
	return m, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/ui/ -v && go test -race ./...
```

Expected: all PASS, race-clean.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mouse.go internal/ui/mouse_test.go
git commit -m "feat(ui): dir picker rows respond to click, double-click, and wheel"
```

---

### Task 9: README, full gate, install, manual smoke

**Files:**
- Modify: `README.md` (after the key table in **Usage**)

**Interfaces:** none — docs and verification.

- [ ] **Step 1: Document the mouse**

Insert into `README.md` directly after the key table (before the `### Resuming` heading):

```markdown
### Mouse

The whole UI is clickable — `sm` enables mouse reporting:

| Gesture | Action |
|---|---|
| click a session | select it (the preview follows) |
| double-click a session | resume it |
| click a project header | fold / unfold that project |
| scroll wheel | move the selection; over the preview, scroll the transcript |
| click the preview pane | focus it (like `tab`) |
| click the filter bar | start filtering (like `/`) |
| click a help-bar action or dialog button | same as pressing its key |
| click outside a dialog | cancel it |

With mouse reporting on, select text with **Shift+drag** (standard for
mouse-enabled TUIs).
```

- [ ] **Step 2: Full gate**

```bash
cd ~/Desktop/ai-sessions-manager
gofmt -l . && go vet ./... && go test -race ./...
```

Expected: `gofmt -l` prints nothing; vet clean; all tests PASS.

- [ ] **Step 3: Install and smoke-test**

```bash
make install && sm --version
```

Expected: `sm 0.1.1` (version var is stamped at release; dev builds print the default). Then a human check in a real terminal — run `sm` and verify: click selects, double-click resumes, header click folds, wheel scrolls list and preview, help-bar and dialog buttons click, Shift+drag still selects text.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: mouse support usage"
```

---

### Task 10: Push and open the upstream PR

**Files:** none.

- [ ] **Step 1: Push the branch to the fork**

```bash
git push -u origin feat/mouse-support
```

- [ ] **Step 2: Open the PR against upstream**

```bash
gh pr create --repo dukechain2333/ai-sessions-manager \
  --head xuanji86:feat/mouse-support \
  --title "feat: mouse support — click to select, double-click to resume, clickable help bar and dialogs" \
  --body "$(cat <<'EOF'
Makes the whole UI mouse-operable, with zero new dependencies and zero keyboard-behavior changes.

| Gesture | Action |
|---|---|
| click a session | select it (preview follows) |
| double-click a session | resume it |
| click a project header | fold / unfold |
| scroll wheel | move selection; over the preview, scroll the transcript |
| click preview / filter bar | focus it (`tab` / `/`) |
| click a help-bar action or dialog button | same as its key |
| click outside a dialog | cancel |

Design notes:

- `WithMouseCellMotion` + a `tea.MouseMsg` handler; hit-testing shares the exact pane arithmetic with `layout()` via extracted `bodyHeight()`/`paneWidths()`, so the two can't drift.
- Help-bar and dialog buttons synthesize the equivalent `tea.KeyMsg` through the existing `handleKey`/`handleDialogKey` — a button can never behave differently from its key. The rendered help-bar text is byte-identical (pinned by a test).
- Double-click = 400 ms window with an injected clock, following the existing `trashFn`/`runClaude` test-injection pattern.
- Dialog hit-testing recomputes `lipgloss.Place` centering; a test locates the rendered border corner and asserts the math, so a lipgloss rounding change fails loudly.
- Docs: spec and plan under `docs/superpowers/`, matching the repo's convention; README gains a Mouse section (including the Shift+drag-to-select-text note that comes with mouse reporting).

Tested with `go test -race ./...` and manually in Ghostty on macOS.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Expected: PR URL printed.

- [ ] **Step 3: Report back**

Give the user: the PR URL, confirmation that `~/.local/bin/sm` is the patched build, and the note that a future official-installer upgrade would overwrite the patched binary until the PR is merged and released.
