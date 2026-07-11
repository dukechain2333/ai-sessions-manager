# Search-Bar Keyboard Access Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `s` = focus the bar on the full-text layer; `↑`/`k` at the list top enters the bar; `↓` in the bar returns to the list.

**Architecture:** Three small additions to `internal/ui/model.go`'s key dispatch; no new state, no listpane/mouse changes.

**Tech Stack:** existing test helpers (`searchModel`, `key`, `typeInto`).

**Spec:** `docs/superpowers/specs/2026-07-11-search-bar-keyboard-access-design.md`

## Global Constraints

- Help-bar text and all mouse geometry untouched. `/` semantics unchanged. Wheel behavior unchanged.
- `s` uses the existing `toggleSearchLayer()` when (and only when) the layer is off — never a manual `searchAll` write; respects its `indexErr` dialog guard by construction.
- Branch `feat/search`; commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`; gate `gofmt -l .` empty, `go vet ./...`, `go test -race ./...`.

---

### Task 1: Key bindings + tests

**Files:**
- Modify: `internal/ui/model.go` (focusList branch: `case "s"`, `case "k", "up"`; focusFilter branch: `case tea.KeyDown`)
- Test: `internal/ui/search_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/search_test.go`:

```go
func TestSKeyFocusesFullTextSearch(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("s"))
	m = m2.(Model)
	if m.focus != focusFilter || !m.filterInput.Focused() {
		t.Fatal("s must focus the search bar")
	}
	if !m.searchAll || m.filterInput.Placeholder != "search…" {
		t.Fatal("s must land on the full-text layer")
	}
	// leave the bar with the layer still on, press s again from the list
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	m2, _ = m.Update(key("s"))
	m = m2.(Model)
	if !m.searchAll {
		t.Error("s with the layer already on must not flip it back off")
	}
	if m.focus != focusFilter {
		t.Error("s must still focus the bar")
	}
}

func TestUpAtTopEntersBarDownReturns(t *testing.T) {
	m := searchModel(t) // grouped: row 0 = alpha header, row 1 = s1 (initial cursor)
	m2, _ := m.Update(key("k")) // row 1 → row 0 (normal move, no focus change)
	m = m2.(Model)
	if m.focus != focusList {
		t.Fatal("k above the first session must stay in the list (moved to the header)")
	}
	m2, _ = m.Update(key("k")) // at row 0: enter the bar
	m = m2.(Model)
	if m.focus != focusFilter || !m.filterInput.Focused() {
		t.Fatal("k at the top row must focus the search bar")
	}
	if m.searchAll {
		t.Error("arrow entry must keep the current (title) layer")
	}
	// ↓ returns to the list keeping the text
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

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/Desktop/ai-sessions-manager && go test ./internal/ui/ -run 'TestSKey|TestUpAtTop' -v`
Expected: FAIL — `s` currently does nothing; `k` at the top clamps; `↓` in the bar feeds the textinput.

- [ ] **Step 3: Implement**

`internal/ui/model.go`, focusList branch — replace `case "k", "up":` and add `case "s":`:

```go
		case "k", "up":
			if m.list.cursor == 0 {
				// walking up past the top row enters the search bar
				m.focus = focusFilter
				m.filterInput.Focus()
				return m, nil
			}
			m.list.MoveCursor(-1)
			return m, m.loadTranscriptCmd()
		case "s":
			// s = search: focus the bar on the full-text layer. / stays the
			// title-filter entry; s never flips an already-on layer back.
			m.focus = focusFilter
			m.filterInput.Focus()
			if !m.searchAll {
				return m, m.toggleSearchLayer()
			}
			return m, nil
```

focusFilter branch — add alongside Esc/Enter/Tab:

```go
		case tea.KeyDown:
			m.filterInput.Blur()
			m.focus = focusList
			return m, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v` — new tests PASS; every pre-existing test PASSES (notably: `j/down` list behavior, Tab toggle, Esc reset, all mouse tests).

- [ ] **Step 5: Full gate + commit**

```bash
gofmt -l . && go vet ./... && go test -race ./...
git add internal/ui/model.go internal/ui/search_test.go
git commit -m "feat(ui): keyboard paths into the search bar — s for search, arrow-key entry/exit"
```

---

### Task 2: README + gate + install

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Document**

In the Usage key table, add after the `/` row:

```markdown
| `s` | focus the search bar on the full-text layer |
```

and extend the `↑/↓` row's description to: `move selection (over project headers and sessions); ↑ at the top enters the search bar, ↓ in the bar returns`.

In the Search section, after the Tab sentence, add: `Pressing `s` in the list jumps straight to the full-text layer; `↑` at the top of the list also enters the bar, and `↓` leaves it.`

- [ ] **Step 2: Full gate**

```bash
gofmt -l . && go vet ./... && go test -race ./...
```

- [ ] **Step 3: Install + commit**

```bash
make install && sm --version
git add README.md
git commit -m "docs: keyboard access to the search bar"
```
