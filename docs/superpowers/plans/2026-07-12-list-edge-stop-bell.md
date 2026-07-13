# List edge-stop with a bell — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `k`/`↑` at the top row and `j`/`↓` at the bottom row keep the cursor put and ring the terminal bell, instead of `↑`-at-top jumping into the filter bar.

**Architecture:** `listPane.MoveCursor` returns whether it moved; the list key handlers ring an injectable `bell` command (BEL to stderr) when it didn't. The `↑`-at-top → filter branch is removed, so the filter/search bar is reached only via `/`, `s`, or a mouse click.

**Tech Stack:** Go ≥1.24, Bubble Tea v1. No new dependencies.

## Global Constraints

- Module `github.com/dukechain2333/ai-sessions-manager`; binary `sm`. Go at `~/.local/go` (prefix shell steps with `export PATH=$HOME/.local/go/bin:$PATH`).
- Edge = top row on up / bottom row on down (empty list also counts as an edge). Bell rings at edges; cursor and focus stay unchanged (`focusList`).
- Bell writes the BEL byte `"\a"` to **stderr** (never stdout — that is Bubble Tea's alt-screen frame). No app-side bell config.
- Keyboard list navigation only (`j`, `k`, `↑`, `↓`). Mouse-wheel edge scrolls stay silent (unchanged). Filter-bar exit (down/enter/esc/click) is unchanged.
- `/` and `s` still focus the filter/search bar. No new `f` key.
- gofmt-clean; `go vet ./...`, `go test -race ./...` green; commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- After the task: `make install`. DO NOT publish/tag/release; do NOT bump the `version` var.

---

### Task 1: Edge-stop navigation with a bell

**Files:**
- Modify: `internal/ui/listpane.go` (`MoveCursor` returns `bool`)
- Modify: `internal/ui/model.go` (`bell` field + `ringBell`; rewrite the `j`/`k` handlers; remove the `cursor == 0` → filter branch)
- Modify: `internal/ui/search_test.go` (rewrite `TestUpAtTopEntersBarDownReturns`)
- Test: `internal/ui/listpane_test.go` (MoveCursor return value), `internal/ui/model_test.go` (edge bell)
- Modify: `README.md` (key-table row)

**Interfaces:**
- Consumes: `listPane.cursor`, `listPane.rows`, `Model.list`, `Model.focus`, `focusList`, `m.loadTranscriptCmd()`.
- Produces: `func (l *listPane) MoveCursor(delta int) (moved bool)`; `Model.bell tea.Cmd`; `func ringBell() tea.Msg`.

- [ ] **Step 1: Write the failing MoveCursor test**

Add to `internal/ui/listpane_test.go`:
```go
func TestMoveCursorReportsEdges(t *testing.T) {
	l := newTestPane() // flat/grouped list with sessions; cursor starts on the first session
	// Walk to the very top; the last upward move that changes nothing returns false.
	for l.MoveCursor(-1) {
	}
	topBefore := l.cursor
	if moved := l.MoveCursor(-1); moved {
		t.Error("MoveCursor(-1) at the top must return false")
	}
	if l.cursor != topBefore {
		t.Errorf("cursor moved at the top edge: %d → %d", topBefore, l.cursor)
	}
	// Walk to the very bottom; one more down returns false and does not move.
	for l.MoveCursor(1) {
	}
	botBefore := l.cursor
	if moved := l.MoveCursor(1); moved {
		t.Error("MoveCursor(1) at the bottom must return false")
	}
	if l.cursor != botBefore {
		t.Errorf("cursor moved at the bottom edge: %d → %d", botBefore, l.cursor)
	}
	// An interior move returns true.
	if !l.MoveCursor(-1) {
		t.Error("MoveCursor(-1) from the bottom must move and return true")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestMoveCursorReportsEdges`
Expected: FAIL — `MoveCursor` returns no value, so `for l.MoveCursor(-1)` and `moved := l.MoveCursor(-1)` do not compile.

- [ ] **Step 3: Make `MoveCursor` return whether it moved**

In `internal/ui/listpane.go`, change the signature and return the moved flag. Replace the whole function:
```go
func (l *listPane) MoveCursor(delta int) (moved bool) {
	if len(l.rows) == 0 {
		return false
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	n := delta
	if n < 0 {
		n = -n
	}
	start := l.cursor
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
	return l.cursor != start
}
```

- [ ] **Step 4: Run the MoveCursor test**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run TestMoveCursorReportsEdges -v`
Expected: PASS.

- [ ] **Step 5: Write the failing edge-bell model tests**

Add to `internal/ui/model_test.go`:
```go
func TestUpAtTopRingsBellStaysInList(t *testing.T) {
	m := newTestModel()
	rang := false
	m.bell = func() tea.Msg { rang = true; return nil }
	// Walk up to the very top row.
	for {
		before := m.list.cursor
		m2, _ := m.Update(key("k"))
		m = m2.(Model)
		if m.list.cursor == before {
			break // at the top edge now
		}
	}
	top := m.list.cursor
	m2, cmd := m.Update(key("k")) // one more up, at the edge
	m = m2.(Model)
	if m.focus != focusList {
		t.Fatalf("up at the top must stay in the list, focus=%v", m.focus)
	}
	if m.list.cursor != top {
		t.Errorf("cursor moved at the top edge: %d → %d", top, m.list.cursor)
	}
	if cmd == nil {
		t.Fatal("up at the top must return the bell command")
	}
	cmd()
	if !rang {
		t.Error("up at the top must ring the bell")
	}
}

func TestDownAtBottomRingsBellStaysInList(t *testing.T) {
	m := newTestModel()
	rang := false
	m.bell = func() tea.Msg { rang = true; return nil }
	for {
		before := m.list.cursor
		m2, _ := m.Update(key("j"))
		m = m2.(Model)
		if m.list.cursor == before {
			break // at the bottom edge
		}
	}
	bottom := m.list.cursor
	m2, cmd := m.Update(key("j"))
	m = m2.(Model)
	if m.focus != focusList {
		t.Fatalf("down at the bottom must stay in the list, focus=%v", m.focus)
	}
	if m.list.cursor != bottom {
		t.Errorf("cursor moved at the bottom edge: %d → %d", bottom, m.list.cursor)
	}
	if cmd == nil {
		t.Fatal("down at the bottom must return the bell command")
	}
	cmd()
	if !rang {
		t.Error("down at the bottom must ring the bell")
	}
}

func TestInteriorMoveDoesNotRing(t *testing.T) {
	m := newTestModel()
	rang := false
	m.bell = func() tea.Msg { rang = true; return nil }
	// Ensure we start with room to move down (cursor on the first session/header).
	before := m.list.cursor
	m2, _ := m.Update(key("j"))
	m = m2.(Model)
	if m.list.cursor == before {
		t.Skip("test model has no interior move available")
	}
	if rang {
		t.Error("an interior move must not ring the bell")
	}
}
```

- [ ] **Step 6: Run them to verify they fail**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'TestUpAtTopRingsBell|TestDownAtBottomRingsBell|TestInteriorMoveDoesNotRing'`
Expected: FAIL — `m.bell` field does not exist; `k` at the top still switches focus to the filter.

- [ ] **Step 7: Add the `bell` field and `ringBell`**

In `internal/ui/model.go`, add a field to the `Model` struct next to the other injected-for-tests fields (after `runCmd`):
```go
	// injected for tests
	trashFn func(store.Session) (string, error)
	runCmd  func(name, dir string, args ...string) tea.Cmd
	bell    tea.Cmd
```
Add the package-level command (near `execCmd`):
```go
// ringBell writes the terminal BEL to stderr — not stdout, which is Bubble
// Tea's alt-screen frame — so it never corrupts the render. The terminal
// decides how BEL manifests (audible, visual, or silent) per its own setting.
func ringBell() tea.Msg {
	fmt.Fprint(os.Stderr, "\a")
	return nil
}
```
In `New`, set the field in the `Model{...}` literal (next to `runCmd: execCmd,`):
```go
		runCmd:       execCmd,
		bell:         ringBell,
		lastClickRow: -1,
		now:          time.Now,
```

- [ ] **Step 8: Rewrite the `j`/`k` handlers**

In `internal/ui/model.go`, replace the `case "j", "down":` and `case "k", "up":` blocks (the ones inside the `default: // focusList` switch) with:
```go
		case "j", "down":
			if m.list.MoveCursor(1) {
				return m, m.loadTranscriptCmd()
			}
			return m, m.bell // bottom edge: stay put, ring
		case "k", "up":
			if m.list.MoveCursor(-1) {
				return m, m.loadTranscriptCmd()
			}
			return m, m.bell // top edge: stay put, ring (no longer enters the filter)
```
(This deletes the `if m.list.cursor == 0 { … focus = focusFilter … }` branch entirely.)

- [ ] **Step 9: Run the edge-bell tests**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'TestUpAtTopRingsBell|TestDownAtBottomRingsBell|TestInteriorMoveDoesNotRing' -v`
Expected: PASS.

- [ ] **Step 10: Update the stale search test**

In `internal/ui/search_test.go`, the existing `TestUpAtTopEntersBarDownReturns` asserts the removed behavior (k at row 0 enters the bar). Replace that entire function with two tests — the new edge behavior, plus the still-valid "down in the bar returns to the list" path reached via `/`:
```go
func TestUpAtTopStaysInListAndBells(t *testing.T) {
	m := searchModel(t)         // grouped: row 0 = alpha header, row 1 = s1 (initial cursor)
	rang := false
	m.bell = func() tea.Msg { rang = true; return nil }
	m2, _ := m.Update(key("k")) // row 1 → row 0 (header): a normal move
	m = m2.(Model)
	if m.focus != focusList || m.list.cursor != 0 {
		t.Fatalf("k above the first session must move to the header and stay in the list (focus=%v cursor=%d)", m.focus, m.list.cursor)
	}
	m2, cmd := m.Update(key("k")) // at row 0: edge — stay put, ring, do NOT enter the bar
	m = m2.(Model)
	if m.focus != focusList {
		t.Fatal("k at the top row must stay in the list, not focus the bar")
	}
	if m.list.cursor != 0 {
		t.Errorf("cursor moved off the top edge: 0 → %d", m.list.cursor)
	}
	if cmd == nil {
		t.Fatal("k at the top row must return the bell command")
	}
	cmd()
	if !rang {
		t.Error("k at the top row must ring the bell")
	}
}

func TestDownInBarReturnsToList(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/")) // enter the filter bar deliberately
	m = m2.(Model)
	if m.focus != focusFilter {
		t.Fatal("/ must focus the filter bar")
	}
	m = typeInto(t, m, "abc")
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m2.(Model)
	if m.focus != focusList || m.filterInput.Focused() {
		t.Fatal("down in the bar must return focus to the list")
	}
	if m.filterInput.Value() != "abc" {
		t.Error("down must keep the query, like enter")
	}
}
```
(If `typeInto` is defined in `search_test.go` — it is used by the original test — keep using it as-is.)

- [ ] **Step 11: Update the README key table**

In `README.md`, replace the `↑/↓` `j/k` row:
```
| `↑/↓` `j/k` | move selection (over project headers and sessions); ↑ at the top enters the search bar, ↓ in the bar returns |
```
with:
```
| `↑/↓` `j/k` | move the selection over project headers and sessions; at the top or bottom edge it stays put and rings the terminal bell. Reach the filter/search bar with `/`, `s`, or a mouse click. |
```

- [ ] **Step 12: Full verification**

Run:
```bash
export PATH=$HOME/.local/go/bin:$PATH
gofmt -w internal/ui/listpane.go internal/ui/model.go internal/ui/listpane_test.go internal/ui/model_test.go internal/ui/search_test.go
gofmt -l internal cmd            # expect empty
go vet ./... && go test -race ./...
```
Expected: gofmt clean, vet clean, all tests pass (incl. `-race`). Pay attention that no other test relied on `↑`-at-top entering the bar (grep `go test ./internal/ui/ 2>&1` output for failures and fix any that assumed the old behavior the same way Step 10 does).

- [ ] **Step 13: Commit**

```bash
git add internal/ui/listpane.go internal/ui/model.go internal/ui/listpane_test.go internal/ui/model_test.go internal/ui/search_test.go README.md
git commit -m "feat(ui): stop at list edges with a bell instead of entering the filter

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [ ] **Step 14: Live check (controller installs after review)**

```bash
export PATH=$HOME/.local/go/bin:$PATH && make build
tmux kill-session -t bell 2>/dev/null
tmux new-session -d -s bell -x 130 -y 40 "./sm"; sleep 3
# Press up repeatedly at the top: cursor should stay on the first row (no jump to the filter bar).
tmux send-keys -t bell 'k k k k k'; sleep 0.5
tmux capture-pane -t bell -p | sed -n '2,6p'   # filter bar row 2 must NOT be focused; selection still on the top session
tmux send-keys -t bell 'q'; sleep 0.5; tmux kill-session -t bell 2>/dev/null
```
Expected: after mashing `k` at the top, the `▶` selection stays on the top session and focus never moves to the `> filter…` bar. (The bell itself is audible/visual per the terminal; verify by ear/eye in a real terminal.) Do NOT `make install` or publish here — the controller installs after review.

---

## Notes for the executor

- Adding a `bool` return to `MoveCursor` does not break the wheel caller at `internal/ui/mouse.go:128` (it ignores the return) — leave that call as-is; wheel edge scrolls stay silent by design.
- Do not bump `version` in `cmd/sm/main.go`.
