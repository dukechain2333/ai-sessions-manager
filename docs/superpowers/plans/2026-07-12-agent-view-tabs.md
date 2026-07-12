# Agent View Tabs (Claude ⇄ Codex) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The list shows one agent at a time — a Claude view and a Codex view — switched by `a` or by clicking title-bar tabs, replacing the mixed list.

**Architecture:** All changes live in `internal/ui`; `internal/store` is untouched. `listPane` gains an `activeAgent` that row-building filters by, per-agent live totals for the tabs, and per-view saved state (cursor/scroll/fold). `Model` renders tabs in the title bar, rebinds `a`, launches `n` with the active agent (deleting `dialogPickAgent`), and tints all chrome with the active agent's accent. The per-project agent-subheader feature and per-row agent tags are removed.

**Tech Stack:** Go ≥ 1.24, Bubble Tea / lipgloss (already vendored in go.mod). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-12-agent-view-tabs-design.md`

## Global Constraints

- Module path: `github.com/dukechain2333/ai-sessions-manager` (import UI/store packages with it).
- Branch: work happens on `feat/agent-tabs` (already created and checked out; spec committed as `333863d`).
- UI copy is English. Exact new strings: tab labels `[Claude 52]` / `Codex 18` (active bracketed); search empty state `no matches · 3 hits in Codex — press a` (singular `1 hit`).
- Single-provider machines must be byte-for-byte unchanged: no tabs, `a` no-op, old `N sessions` title count.
- Two-agent mode is `len(m.providers) > 1` (the Codex provider registers only when its dir exists) — the same signal `launchNewSession` uses today.
- Tests: plain `testing` package, no new test deps, follow existing table/helper style. `make test` and `make vet` green at every commit.
- Commits: conventional style (`feat(ui): …`, `refactor(ui): …`, `test(ui): …`, `docs: …`), each ending with the trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Zero-value safety: many tests build `listPane{...}` directly, so an unset `activeAgent` must behave as `AgentClaude` (`l.agent()` helper below).

---

### Task 1: listPane — single-agent row building, live totals, per-view state

The pane keeps holding **all** sessions but only builds rows for the active agent. Both agents' would-be counts are tracked for the tabs. `SetAgent` swaps cursor/scroll/fold state per view. The three existing tests that render mixed-agent rows (subheaders, tags) contradict the new model and are deleted here; the machinery they covered is deleted in Task 4.

**Files:**
- Modify: `internal/ui/listpane.go`
- Test: `internal/ui/listpane_test.go`
- Test (deletion): `internal/ui/mouse_test.go` (`TestClickSubheaderIsInert`)

**Interfaces:**
- Consumes: `store.Agent`, `store.AgentClaude`, `store.AgentCodex`, existing `listPane` internals.
- Produces (used by Tasks 2–7):
  - `func (l *listPane) Agent() store.Agent` — active view (`""` ⇒ Claude).
  - `func (l *listPane) SetAgent(a store.Agent)` — switch view, swap per-view state, refresh.
  - `func (l *listPane) AgentTotal(a store.Agent) int` — sessions agent `a` would display now (honors empty toggle + filter + search).
  - `func otherAgent(a store.Agent) store.Agent` — package-level flip helper.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/listpane_test.go`:

```go
// mixedSessions is testSessions plus two codex sessions: one sharing
// project alpha, one in its own project delta.
func mixedSessions() []store.Session {
	s := testSessions()
	s = append(s,
		store.Session{ID: "x1", Slug: "-p1", CWD: "/x/alpha", Title: "Codex in alpha", FirstPrompt: "hello", Agent: store.AgentCodex, UserMessages: 1, Enriched: true, LastActivity: time.Now().Add(-30 * time.Minute)},
		store.Session{ID: "x2", Slug: "-p4", CWD: "/x/delta", Title: "Codex in delta", FirstPrompt: "yo", Agent: store.AgentCodex, UserMessages: 2, Enriched: true, LastActivity: time.Now().Add(-3 * time.Hour)},
	)
	return s
}

func newMixedPane() listPane {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(50, 30)
	l.SetSessions(mixedSessions())
	return l
}

func TestListShowsOnlyActiveAgent(t *testing.T) {
	l := newMixedPane()
	if l.Agent() != store.AgentClaude {
		t.Fatalf("default view = %v, want claude", l.Agent())
	}
	if got := l.Len(); got != 2 { // s1, s2 (s3 is empty and hidden)
		t.Fatalf("claude view Len = %d, want 2", got)
	}
	if got := l.AgentTotal(store.AgentCodex); got != 2 {
		t.Errorf("AgentTotal(codex) = %d, want 2", got)
	}
	if got := l.AgentTotal(store.AgentClaude); got != 2 {
		t.Errorf("AgentTotal(claude) = %d, want 2", got)
	}
	v := l.View()
	if strings.Contains(v, "Codex in alpha") || strings.Contains(v, "delta") {
		t.Errorf("claude view must not render codex rows:\n%s", v)
	}

	l.SetAgent(store.AgentCodex)
	if got := l.Len(); got != 2 {
		t.Fatalf("codex view Len = %d, want 2", got)
	}
	v = l.View()
	if !strings.Contains(v, "Codex in delta") {
		t.Errorf("codex view missing codex session:\n%s", v)
	}
	if strings.Contains(v, "Fix backup script") {
		t.Errorf("codex view must not render claude rows:\n%s", v)
	}
}

func TestAgentTotalsHonorFilter(t *testing.T) {
	l := newMixedPane()
	l.SetFilter("codex")
	if got := l.Len(); got != 0 {
		t.Errorf("claude view under filter 'codex': Len = %d, want 0", got)
	}
	if got := l.AgentTotal(store.AgentCodex); got != 2 {
		t.Errorf("AgentTotal(codex) under filter = %d, want 2", got)
	}
	l.SetAgent(store.AgentCodex)
	if got := l.Len(); got != 2 {
		t.Errorf("codex view under its filter: Len = %d, want 2", got)
	}
}

func TestSetAgentKeepsPerViewState(t *testing.T) {
	l := newMixedPane()
	l.ToggleFold() // cursor starts in alpha; folds it in the claude view
	if !l.folded["alpha"] {
		t.Fatal("setup: alpha should be folded in the claude view")
	}
	l.MoveCursor(1) // move off the folded header
	cur, off := l.cursor, l.lineOffset

	l.SetAgent(store.AgentCodex)
	if l.folded["alpha"] {
		t.Error("codex view must start with its own (empty) fold state")
	}
	l.ToggleFold() // fold whatever project the codex cursor is in
	l.SetAgent(store.AgentClaude)
	if !l.folded["alpha"] {
		t.Error("claude fold state lost across a round-trip")
	}
	if l.cursor != cur || l.lineOffset != off {
		t.Errorf("claude cursor/offset = %d/%d, want %d/%d", l.cursor, l.lineOffset, cur, off)
	}
}

func TestOtherAgent(t *testing.T) {
	if otherAgent(store.AgentClaude) != store.AgentCodex || otherAgent(store.AgentCodex) != store.AgentClaude {
		t.Error("otherAgent must flip between the two agents")
	}
}
```

- [ ] **Step 2: Delete the three now-contradicted tests**

They exercise mixed-agent rendering, which no longer exists:

- `internal/ui/listpane_test.go`: delete `TestListShowsAgentTag` (~line 476), `TestAgentGroupingSubheaders` (~501), `TestAgentSubheaderNotCursorable` (~521) — whole functions.
- `internal/ui/mouse_test.go`: delete `TestClickSubheaderIsInert` (~line 104) — whole function.

- [ ] **Step 3: Run tests to verify the new ones fail**

Run: `go test ./internal/ui/ -run 'TestListShowsOnlyActiveAgent|TestAgentTotalsHonorFilter|TestSetAgentKeepsPerViewState|TestOtherAgent' -v`
Expected: compile FAIL — `l.Agent`, `l.AgentTotal`, `l.SetAgent`, `otherAgent` undefined.

- [ ] **Step 4: Implement in `internal/ui/listpane.go`**

4a. Add fields to `listPane` (after `tmuxLive`):

```go
	activeAgent store.Agent               // which agent's sessions render; "" behaves as claude
	agentTotals map[store.Agent]int       // per-agent visible counts under the current mode
	savedViews  map[store.Agent]viewState // parked cursor/scroll/fold of inactive views
```

4b. Add near the `row` type:

```go
// viewState is the navigation state parked for an inactive agent view.
type viewState struct {
	cursor     int
	lineOffset int
	folded     map[string]bool
}

// otherAgent flips between the two agents.
func otherAgent(a store.Agent) store.Agent {
	if a == store.AgentCodex {
		return store.AgentClaude
	}
	return store.AgentCodex
}
```

4c. Add accessors + `SetAgent` (near `ToggleGroup`):

```go
// agent is the active view, defaulting the zero value to Claude so a
// zero-constructed pane (tests, early init) still shows sessions.
func (l *listPane) agent() store.Agent {
	if l.activeAgent == "" {
		return store.AgentClaude
	}
	return l.activeAgent
}

// Agent returns the active agent view.
func (l *listPane) Agent() store.Agent { return l.agent() }

// AgentTotal reports how many sessions agent a would display right now,
// honoring the empty toggle and any live filter or search results.
func (l *listPane) AgentTotal(a store.Agent) int { return l.agentTotals[a] }

// SetAgent switches the pane to agent a's view, parking the current view's
// cursor, scroll, and fold state and restoring a's. refresh clamps the
// restored cursor if that view's rows changed while it was parked.
func (l *listPane) SetAgent(a store.Agent) {
	if a == l.agent() {
		return
	}
	if l.savedViews == nil {
		l.savedViews = map[store.Agent]viewState{}
	}
	l.savedViews[l.agent()] = viewState{cursor: l.cursor, lineOffset: l.lineOffset, folded: l.folded}
	st := l.savedViews[a]
	l.activeAgent = a
	l.cursor, l.lineOffset, l.folded = st.cursor, st.lineOffset, st.folded
	l.refresh()
}
```

4d. Rework `refresh()`. The search branch becomes:

```go
	// Search-results mode: flat, given order, suppresses filter/grouping.
	// Rows come from the active agent's hits; the other agent's hit count
	// still feeds agentTotals for the tabs and the empty-state hint.
	if l.search != nil {
		l.rows = l.rows[:0]
		l.counts = map[string]int{}
		l.agentTotals = map[store.Agent]int{}
		l.total = 0
		for _, h := range l.search {
			if h.Session < 0 || h.Session >= len(l.sessions) {
				continue
			}
			ag := l.sessions[h.Session].Agent
			l.agentTotals[ag]++
			if ag != l.agent() {
				continue
			}
			l.total++
			l.rows = append(l.rows, row{project: l.sessions[h.Session].Project(), session: h.Session})
		}
		if l.cursor >= len(l.rows) {
			l.cursor = len(l.rows) - 1
		}
		if l.cursor < 0 {
			l.cursor = 0
		}
		l.ensureVisible()
		return
	}
```

In the browse/filter path, after `base` is built (leave the existing step-1 selection code untouched), replace the current `l.total = len(base)` line with a partition, and feed the later steps from `act` instead of `base`:

```go
	// 2. Partition by agent: rows render only the active view, but both
	//    counts are kept so the tabs can show the other side live.
	l.agentTotals = map[store.Agent]int{}
	var act []int
	for _, si := range base {
		ag := l.sessions[si].Agent
		l.agentTotals[ag]++
		if ag == l.agent() {
			act = append(act, si)
		}
	}
	l.total = len(act)
```

Then in the (renumbered) per-project count step and the row-building step, replace both `for _, si := range base` loops with `for _, si := range act`. Everything else in `refresh()` stays.

- [ ] **Step 5: Run the new tests**

Run: `go test ./internal/ui/ -run 'TestListShowsOnlyActiveAgent|TestAgentTotalsHonorFilter|TestSetAgentKeepsPerViewState|TestOtherAgent' -v`
Expected: PASS

- [ ] **Step 6: Run the full package and fix collateral**

Run: `go test ./internal/ui/ 2>&1 | tail -20`
Expected: PASS. If `TestAgentGroupKeyToggles` (actions_test.go) fails, leave it — it must still pass here because `groupByAgent` still exists and flips; it is rewritten in Task 3. Any other failure means a `base`→`act` slip in Step 4d — fix before committing.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/listpane.go internal/ui/listpane_test.go internal/ui/mouse_test.go
git commit -m "feat(ui): listPane renders one agent view with live per-agent totals

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: listPane — search empty-state hint for the other view

**Files:**
- Modify: `internal/ui/listpane.go` (View's empty state)
- Test: `internal/ui/listpane_test.go`

**Interfaces:**
- Consumes: `l.agentTotals`, `otherAgent`, `agentTitle` (exists at the bottom of listpane.go; it is kept — Task 4 must NOT delete it).
- Produces: empty-state string `no matches · %d hit(s) in <Agent> — press a` used only in search mode.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/listpane_test.go`:

```go
func TestSearchEmptyStateHintsOtherView(t *testing.T) {
	l := newMixedPane()
	// Hits land only on the two codex sessions (slice indices 3 and 4).
	l.SetSearchResults([]store.SessionHits{{Session: 3, MsgHits: 5}, {Session: 4, MsgHits: 1}})
	if got := l.Len(); got != 0 {
		t.Fatalf("claude view Len = %d, want 0", got)
	}
	if v := l.View(); !strings.Contains(v, "no matches · 2 hits in Codex — press a") {
		t.Errorf("empty search view = %q, want the cross-view hint", v)
	}
	l.SetAgent(store.AgentCodex)
	if got := l.Len(); got != 2 {
		t.Errorf("codex view Len = %d, want 2", got)
	}
	// One hit: singular wording.
	l.SetSearchResults([]store.SessionHits{{Session: 0, MsgHits: 2}}) // s1, claude
	if v := l.View(); !strings.Contains(v, "no matches · 1 hit in Claude — press a") {
		t.Errorf("singular hint missing: %q", v)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestSearchEmptyStateHintsOtherView -v`
Expected: FAIL — view says bare `no matches`.

- [ ] **Step 3: Implement**

In `internal/ui/listpane.go` `View()`, replace the empty-state block:

```go
	if l.total == 0 {
		if l.search != nil {
			msg := "no matches"
			if n := l.agentTotals[otherAgent(l.agent())]; n > 0 {
				plural := "s"
				if n == 1 {
					plural = ""
				}
				msg = fmt.Sprintf("no matches · %d hit%s in %s — press a", n, plural, agentTitle(otherAgent(l.agent())))
			}
			return l.padHeight(l.styles.ListMeta.Render(msg))
		}
		if l.filter != "" {
			return l.padHeight(l.styles.ListMeta.Render("no matches"))
		}
		return l.padHeight(l.styles.ListMeta.Render("no sessions"))
	}
```

(The old block folded `l.search != nil || l.filter != ""` into one case; this splits them. The fuzzy-filter empty state stays a bare `no matches` — the live tabs already show the other side.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -run 'TestSearchEmptyState|TestListFilter' -v` then `go test ./internal/ui/`
Expected: PASS (including the pre-existing `TestSearchZeroHits*` / "no matches" tests, which still match by substring).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/listpane.go internal/ui/listpane_test.go
git commit -m "feat(ui): search empty state points at the other agent view

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Model — `a` switches views, title-bar tabs, default view

**Files:**
- Modify: `internal/ui/model.go`
- Modify: `internal/ui/mouse.go` (only if the compiler complains — it shouldn't in this task)
- Test: `internal/ui/model_test.go`, `internal/ui/actions_test.go`

**Interfaces:**
- Consumes: `l.SetAgent/Agent/AgentTotal`, `otherAgent`, `agentTitle`, `m.st.AgentAccent`.
- Produces (used by Task 7's mouse work):
  - `type agentTab struct { label string; agent store.Agent }`
  - `func (m Model) agentTabs() []agentTab` — nil when `len(m.providers) <= 1`.
  - `func (m *Model) setAgentView(a store.Agent)` — switches the list + re-tints the filter prompt.
  - `func (m Model) switchAgentView(a store.Agent) (tea.Model, tea.Cmd)` — guarded switch used by key and (later) mouse.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/model_test.go`:

```go
// newTwoAgentModel is newTestModel with a second (codex) provider and a
// mixed-agent session set, for view-switching tests.
func newTwoAgentModel(t *testing.T) Model {
	t.Helper()
	m := New("/nonexistent-projects-dir", "/nonexistent-codex-dir", config.Default())
	m.providers = append(m.providers, store.NewCodexProvider(t.TempDir()))
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(Model)
	m2, _ = m.Update(scanDoneMsg{sessions: mixedSessions()})
	return m2.(Model)
}

func TestAgentKeySwitchesView(t *testing.T) {
	m := newTwoAgentModel(t)
	if m.list.Agent() != store.AgentClaude {
		t.Fatalf("default view = %v, want claude", m.list.Agent())
	}
	m2, cmd := m.Update(key("a"))
	m = m2.(Model)
	if m.list.Agent() != store.AgentCodex {
		t.Error("`a` should switch to the codex view")
	}
	if cmd == nil {
		t.Error("switching views should reload the preview")
	}
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	if m.list.Agent() != store.AgentClaude {
		t.Error("`a` should switch back to the claude view")
	}
}

func TestAgentKeyNoopWithSingleProvider(t *testing.T) {
	m := newTestModel() // exactly one provider (claude)
	m2, _ := m.Update(key("a"))
	m = m2.(Model)
	if m.list.Agent() != store.AgentClaude {
		t.Error("single provider: `a` must not switch views")
	}
}

func TestTitleShowsTabsWithLiveCounts(t *testing.T) {
	m := newTwoAgentModel(t)
	v := m.View()
	if !strings.Contains(v, "[Claude 2]") || !strings.Contains(v, "Codex 2") {
		t.Errorf("two-agent title must show both tabs, active bracketed:\n%s", v)
	}
	if strings.Contains(v, "sessions") {
		t.Errorf("two-agent title must not show the old count string:\n%s", v)
	}
	m2, _ := m.Update(key("a"))
	m = m2.(Model)
	if v := m.View(); !strings.Contains(v, "[Codex 2]") || !strings.Contains(v, "Claude 2") {
		t.Errorf("codex view must bracket the codex tab:\n%s", v)
	}
}

func TestTitleFallsBackToCountWithSingleProvider(t *testing.T) {
	m := newTestModel()
	v := m.View()
	if !strings.Contains(v, "2 sessions") {
		t.Errorf("single-provider title must keep the session count:\n%s", v)
	}
	if strings.Contains(v, "[Claude") {
		t.Errorf("single-provider title must not render tabs:\n%s", v)
	}
}

func TestDefaultViewIsCodexWhenClaudeDirMissing(t *testing.T) {
	m := New("/nonexistent-projects-dir", t.TempDir(), config.Default())
	if len(m.providers) < 2 {
		t.Fatal("setup: codex provider should have registered")
	}
	if m.list.Agent() != store.AgentCodex {
		t.Errorf("claude dir missing: default view = %v, want codex", m.list.Agent())
	}
}

func TestSwitchKeepsFilterApplied(t *testing.T) {
	m := newTwoAgentModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	for _, r := range "codex" {
		m2, _ = m.Update(key(string(r)))
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // back to list focus
	m = m2.(Model)
	if got := m.list.Len(); got != 0 {
		t.Fatalf("claude view filtered by 'codex': Len = %d, want 0", got)
	}
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	if got := m.list.Len(); got != 2 {
		t.Errorf("codex view must re-apply the live filter: Len = %d, want 2", got)
	}
}
```

In `internal/ui/actions_test.go`, replace `TestAgentGroupKeyToggles` (~line 245) entirely with:

```go
func TestAgentKeyRoundTripKeepsSelection(t *testing.T) {
	m := newTwoAgentModel(t)
	before, _, ok := m.list.Selected()
	if !ok {
		t.Fatal("setup: expected a selection")
	}
	m2, _ := m.Update(key("a"))
	m = m2.(Model)
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	after, _, ok := m.list.Selected()
	if !ok || after.ID != before.ID {
		t.Errorf("selection after a↔a round-trip = %v, want %v", after.ID, before.ID)
	}
}
```

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/ui/ -run 'TestAgentKey|TestTitle|TestDefaultView|TestSwitchKeepsFilter' -v`
Expected: FAIL — `a` still toggles `groupByAgent`; title still renders `4 sessions`; default view test fails only if codex registration broke (it should pass builds but fail on the Agent() assert — acceptable either way at this step).

- [ ] **Step 3: Implement in `internal/ui/model.go`**

3a. In `New()`, set the default view after `ret` is built (before `ret.index, ret.indexErr = …`):

```go
	// Default view: Claude — unless its projects dir is missing while a
	// Codex provider registered, then start where the sessions are.
	if len(provs) > 1 && !provs[0].Available() {
		ret.setAgentView(store.AgentCodex)
	}
```

3b. Add the view-switch helpers (near `toggleSearchLayer`):

```go
// setAgentView switches the list to agent a's view and re-tints the one
// piece of chrome that is not re-derived every render: the filter prompt.
func (m *Model) setAgentView(a store.Agent) {
	m.list.SetAgent(a)
	m.filterInput.PromptStyle = lipgloss.NewStyle().Foreground(m.st.AgentAccent(a))
}

// switchAgentView activates agent a's view. No-op with a single provider
// or when a is already active. Shared by the `a` key and tab clicks.
func (m Model) switchAgentView(a store.Agent) (tea.Model, tea.Cmd) {
	if len(m.providers) <= 1 || a == m.list.Agent() {
		return m, nil
	}
	m.setAgentView(a)
	m.lastClickRow = -1 // rows renumbered — a stale click must not pair
	return m, m.loadTranscriptCmd()
}
```

3c. Rebind the key in `handleKey` (focusList switch):

```go
		case "a":
			return m.switchAgentView(otherAgent(m.list.Agent()))
```

(The old body called `m.list.ToggleAgentGroup()`; that method still exists until Task 4 but is no longer referenced by the model.)

3d. Add the tab table (near `projectLabelText`, since mouse code will mirror it):

```go
// agentTab is one title-bar tab: its rendered label and the agent a click
// on it activates. View() and the mouse hit-test share this table.
type agentTab struct {
	label string
	agent store.Agent
}

// agentTabs returns the title-bar tabs (Claude first, active bracketed,
// live per-view counts), or nil when only one provider is registered.
func (m Model) agentTabs() []agentTab {
	if len(m.providers) <= 1 {
		return nil
	}
	mk := func(a store.Agent) agentTab {
		label := fmt.Sprintf("%s %d", agentTitle(a), m.list.AgentTotal(a))
		if m.list.Agent() == a {
			label = "[" + label + "]"
		}
		return agentTab{label: label, agent: a}
	}
	return []agentTab{mk(store.AgentClaude), mk(store.AgentCodex)}
}
```

3e. Rework the title bar in `View()`. Replace everything from `count := fmt.Sprintf("%d sessions", m.list.Len())` through the `header := lipgloss.JoinHorizontal(…)` call with:

```go
	tabs := m.agentTabs()
	status := ""
	if tabs == nil {
		status = fmt.Sprintf("%d sessions", m.list.Len())
		if m.searchAll && m.activeQuery != "" {
			status = fmt.Sprintf("%d sessions · %d matched", len(m.list.Sessions()), m.matched)
			if !m.indexReady {
				status += "…"
			}
		}
	}
	if m.indexing {
		status += fmt.Sprintf(" · indexing %d/%d…", m.indexDone, m.indexTotal)
	}
	if m.indexFailed > 0 {
		status += fmt.Sprintf(" · %d unindexed", m.indexFailed)
	}
	if m.loading {
		status += " · scanning…"
	}
	segs := []string{
		m.st.TitleMark(), // ✻ in accent
		m.st.AppTitle.Render(" sm · AI Sessions  "),
	}
	for i, tb := range tabs {
		st := m.st.Count
		if tb.agent == m.list.Agent() {
			st = lipgloss.NewStyle().Bold(true).Foreground(m.st.AgentAccent(tb.agent))
		}
		lbl := tb.label
		if i < len(tabs)-1 {
			lbl += "  " // two-space tab separator; tabAt (Task 7) mirrors this
		}
		segs = append(segs, st.Render(lbl))
	}
	segs = append(segs, m.st.Count.Render(status))
	header := lipgloss.JoinHorizontal(lipgloss.Top, segs...)
```

(With tabs, the `%d matched` suffix is dropped — the tab counts carry it. Index/scan statuses append after the tabs in both modes, per spec.)

- [ ] **Step 4: Run the new tests**

Run: `go test ./internal/ui/ -run 'TestAgentKey|TestTitle|TestDefaultView|TestSwitchKeepsFilter' -v`
Expected: PASS

- [ ] **Step 5: Full package run**

Run: `go test ./internal/ui/`
Expected: PASS. Watch for title-string assertions elsewhere (`search_test.go` asserts on matched counts in the title — if one fails, it is asserting the single-provider path via `newTestModel`, which still renders the old string; only fix a test if it constructed a two-provider model expecting the old title).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go internal/ui/actions_test.go
git commit -m "feat(ui): a switches Claude/Codex views; title bar renders live tabs

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: listPane — remove subheaders and per-row tags; accent follows the view

Everything mixed-agent inside the pane goes: subheader rows, `groupByAgent`, the per-row `claude`/`codex` tag. Selection/header accents derive from the active view.

**Files:**
- Modify: `internal/ui/listpane.go`, `internal/ui/mouse.go`, `internal/ui/styles.go`
- Test: `internal/ui/listpane_test.go`, `internal/ui/styles_test.go`

**Interfaces:**
- Consumes: `l.agent()`, `l.styles.AgentAccent`.
- Produces (used by Task 5): `func (l *listPane) accent() lipgloss.AdaptiveColor` — the active view's accent color.
- Deletes: `row.subheader`, `row.label`, `IsSubheader`, `ToggleAgentGroup`, `groupByAgent`, `projectHasBothAgents`, styles `ClaudeTag`/`CodexTag`/`CodexTitleSel`. **Keeps `agentTitle`** (tabs + hint use it) and **keeps `projectMajorityAgent` until Task 5**.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/listpane_test.go`:

```go
func TestAccentFollowsActiveView(t *testing.T) {
	l := newMixedPane()
	if l.accent() != l.styles.Accent {
		t.Error("claude view accent should be the claude accent")
	}
	l.SetAgent(store.AgentCodex)
	if l.accent() != l.styles.CodexAccent {
		t.Error("codex view accent should be the codex accent")
	}
}

func TestListRowsCarryNoAgentTag(t *testing.T) {
	l := newMixedPane()
	if v := l.View(); strings.Contains(v, "claude") || strings.Contains(v, "codex") {
		t.Errorf("single-agent view must not render per-row agent tags:\n%s", v)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'TestAccentFollowsActiveView|TestListRowsCarryNoAgentTag' -v`
Expected: compile FAIL (`l.accent` undefined), then tag assertion FAIL once it compiles.

- [ ] **Step 3: Implement the removals and the accent helper**

In `internal/ui/listpane.go`:

3a. `row`: delete the `subheader` and `label` fields (comment included).

3b. Delete whole functions: `ToggleAgentGroup`, `IsSubheader`, `projectHasBothAgents`. Delete the `groupByAgent` field. **Do not delete `agentTitle`.**

3c. Add:

```go
// accent is the active view's accent color: coral in the Claude view,
// teal in the Codex view.
func (l *listPane) accent() lipgloss.AdaptiveColor {
	return l.styles.AgentAccent(l.agent())
}
```

(`AgentAccent` already returns `lipgloss.AdaptiveColor` — no conversion needed.)

3d. Purge `subheader` handling: in `MoveCursor` remove the hop-over loop (keep plain bounds-checked stepping); in `Selected`/`selectedSession`/`selectSession`/`cursorToFirstSession`/`ToggleFold`/`CursorProject`/`layout`/`ensureVisible` remove every `|| l.rows[...].subheader` / `!r.subheader` clause; in `refresh()` grouped row-building, replace the whole `if l.groupByAgent && projectHasBothAgents(...) { ... } else { ... }` block with the plain loop:

```go
		for _, si := range projSessions {
			l.rows = append(l.rows, row{project: p, session: si})
		}
```

3e. In `View()`: delete the `if r.subheader { … }` branch; delete the `tag`/`tagStyle` lines and the codex-sel override; render selection and headers through the view accent. The session-row tail of the loop becomes:

```go
		s := l.sessions[r.session]
		title := s.Title
		if title == "" {
			if s.Enriched {
				title = "(untitled)"
			} else {
				title = s.ID
			}
		}
		meta := s.Project() + " · " + humanTime(s.LastActivity, time.Now())
		if s.GitBranch != "" {
			meta += " · " + s.GitBranch
		}
		if s.Unreadable {
			meta += " · (unreadable)"
		}
		if n := l.searchHits(r.session); n > 0 {
			meta += " · " + fmt.Sprintf("%d hit", n)
			if n != 1 {
				meta += "s"
			}
		}
		prefix := "  "
		titleStyle, metaStyle := l.styles.ListTitle, l.styles.ListMeta
		if i == l.cursor {
			prefix = "▶ "
			titleStyle = l.styles.ListTitleSel.Foreground(l.accent())
			metaStyle = l.styles.ListMetaSel.Foreground(l.accent())
		}
		titleWidth := l.width
		marker := ""
		if l.tmuxLive[tmuxNameFor(s)] {
			titleWidth -= 2 // reserve space for " ●"
			marker = " " + lipgloss.NewStyle().Foreground(l.accent()).Render("●")
		}
		lines = append(lines,
			titleStyle.Render(store.Truncate(prefix+title, titleWidth))+marker,
			metaStyle.Render(store.Truncate("  "+meta, l.width)),
			"")
```

In the header branch of the same loop, swap the accent-carrying styles:

```go
			style := l.styles.GroupHeader
			if i == l.cursor {
				style = l.styles.GroupHeaderSel.Foreground(l.accent())
			}
```
and
```go
				rendered = style.Render(label[:len(label)-len(suffix)]) + " " + l.styles.GroupCount.Foreground(l.accent()).Render(count)
```
and the header's live-tmux dot:
```go
			if l.projectHasLiveTmux(r.project) {
				rendered += " " + lipgloss.NewStyle().Foreground(l.accent()).Render("●")
			}
```

3f. In `internal/ui/mouse.go` `clickList`, delete the `if m.list.IsSubheader(row) { return m, nil }` guard.

3g. In `internal/ui/styles.go`: delete the `ClaudeTag`, `CodexTag`, `CodexTitleSel` fields and their initializers in `stylesWithColors`.

3h. In `internal/ui/styles_test.go`, `TestStylesWithColorsOverridesAccents` ends with an assertion on the deleted `st.ClaudeTag`. Replace those three lines with the same check against a surviving derived style:

```go
	// A derived style must pick up the override too.
	if st.GroupCount.GetForeground() != lipgloss.AdaptiveColor(st.Accent) {
		t.Error("GroupCount should use the overridden accent")
	}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -run 'TestAccentFollowsActiveView|TestListRowsCarryNoAgentTag' -v && go test ./internal/ui/`
Expected: PASS. `go vet ./...` clean (catches now-unused imports — remove any).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/listpane.go internal/ui/mouse.go internal/ui/styles.go internal/ui/listpane_test.go internal/ui/styles_test.go
git commit -m "refactor(ui): drop agent subheaders and per-row tags; list accent follows the view

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Model — chrome tints with the active view; drop majority-agent logic

**Files:**
- Modify: `internal/ui/model.go`, `internal/ui/styles.go`, `internal/ui/listpane.go`
- Test: `internal/ui/model_test.go`, `internal/ui/listpane_test.go` (deletions), `internal/ui/styles_test.go`

**Interfaces:**
- Consumes: `m.list.Agent()`, `m.st.AgentAccent`.
- Produces: `func (s styles) TitleMarkFor(a store.Agent) string`.
- Deletes: `styles.TitleMark`, `Model.projectLabelColor`, `listPane.projectMajorityAgent` (+ its two tests).

- [ ] **Step 1: Rewrite the failing test**

In `internal/ui/model_test.go`, replace `TestFocusedBorderAndLabelColorFollowAgent` (~line 361) entirely with:

```go
func TestChromeColorsFollowActiveView(t *testing.T) {
	m := newTwoAgentModel(t)
	if m.focusedBorderColor() != m.st.Accent {
		t.Error("claude view should give the coral border color")
	}
	m2, _ := m.Update(key("a"))
	m = m2.(Model)
	if m.focusedBorderColor() != m.st.CodexAccent {
		t.Error("codex view should give the teal border color")
	}
	// Even with nothing selected the color keys off the view, not the
	// selection — this is the branch the old selected-session logic fails.
	m.list.SetFilter("zzzz-no-match")
	if _, _, ok := m.list.Selected(); ok {
		t.Fatal("setup: filter should leave no selection")
	}
	if m.focusedBorderColor() != m.st.CodexAccent {
		t.Error("empty codex view should still give the teal border color")
	}
}
```

Delete `TestProjectMajorityAgent` and `TestProjectMajorityAgentTieUsesSelected` from `internal/ui/listpane_test.go` (~lines 244–275) — whole functions.

- [ ] **Step 2: Run to see the rewritten test fail**

Run: `go test ./internal/ui/ -run TestChromeColorsFollowActiveView -v`
Expected: FAIL on the empty-view assertion — the old `focusedBorderColor` falls back to the claude `Accent` when nothing is selected.

- [ ] **Step 3: Implement**

3a. `internal/ui/model.go` — replace `focusedBorderColor` and delete `projectLabelColor`:

```go
// focusedBorderColor is the border color of the focused pane: the active
// view's agent accent.
func (m Model) focusedBorderColor() lipgloss.AdaptiveColor {
	return m.st.AgentAccent(m.list.Agent())
}
```

In `View()`, change the label style line to use the same accent:

```go
	styledLabel := lipgloss.NewStyle().Bold(true).Foreground(m.st.AgentAccent(m.list.Agent())).
		MaxWidth(m.width).Render(label)
```

3b. `internal/ui/styles.go` — replace `TitleMark` with the agent-tinted version:

```go
// TitleMarkFor is the ✻ mark tinted with agent a's accent.
func (s styles) TitleMarkFor(a store.Agent) string {
	return lipgloss.NewStyle().Foreground(s.AgentAccent(a)).Render("✻")
}
```

In `model.go` `View()`, the header segment becomes `m.st.TitleMarkFor(m.list.Agent())`.

3c. `internal/ui/listpane.go` — delete `projectMajorityAgent` (whole function; its View-side uses were already replaced in Task 4).

3d. `TitleMark()` has exactly one caller (`model.go` `View()`, updated in 3b) and no test references — after the rename, `go build ./...` confirms nothing else breaks.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ && go vet ./...`
Expected: PASS / clean.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/styles.go internal/ui/listpane.go internal/ui/model_test.go internal/ui/listpane_test.go internal/ui/styles_test.go
git commit -m "feat(ui): border, label, and title mark tint with the active view

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Model — `n` launches the active view's agent; delete dialogPickAgent

**Files:**
- Modify: `internal/ui/model.go`
- Test: `internal/ui/actions_test.go`, `internal/ui/mouse_test.go`

**Interfaces:**
- Consumes: `m.list.Agent()`, `store.ProviderFor`, `binLookPath`, `m.runAgentCmd`.
- Deletes: `dialogPickAgent` const, its `handleDialogKey` and `dialogView` cases, the `pendingNewDir` field.

- [ ] **Step 1: Write/rewrite the failing tests**

Append to `internal/ui/actions_test.go`:

```go
func TestNewSessionUsesActiveView(t *testing.T) {
	m := newTwoAgentModel(t)
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir // s1 (claude) is the initial selection
	rec := &resumeRecorder{}
	m.runCmd = rec.cmd
	m2, _ := m.Update(key("n"))
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Fatalf("claude view `n`: dialog = %v, want none", m.dialog)
	}
	if rec.name != "claude" || rec.dir != dir {
		t.Errorf("claude view `n` launched %q in %q, want claude in %q", rec.name, rec.dir, dir)
	}

	// Codex view: give its top session a real cwd, switch, press n.
	dir2 := t.TempDir()
	for i := range m.list.sessions {
		if m.list.sessions[i].Agent == store.AgentCodex {
			m.list.sessions[i].CWD = dir2
		}
	}
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	m2, _ = m.Update(key("n"))
	m = m2.(Model)
	if rec.name != "codex" || rec.dir != dir2 {
		t.Errorf("codex view `n` launched %q in %q, want codex in %q", rec.name, rec.dir, dir2)
	}
}
```

Rewrite the three dialog-flow tests in `internal/ui/actions_test.go`:

- `TestNewSessionPicker` (~125): keep everything up to the dir-picker `Enter`, then replace the pick-agent tail — after `m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}); m = m2.(Model)` assert directly:

```go
	if m.dialog != dialogNone {
		t.Fatalf("after picking a dir: dialog = %v, want none (launch directly)", m.dialog)
	}
	if rec.name != "claude" || rec.dir != dir || len(rec.args) != 0 {
		t.Errorf("new session: name=%q dir=%q args=%v, want claude dir=%q args=[]", rec.name, rec.dir, rec.args, dir)
	}
```

- `TestNewSessionTypedPath` (~160): same surgery — after the typed-path `Enter`, assert `rec.name == "claude" && rec.dir == sub` and `m.dialog == dialogNone`; delete the `key("c")` step and the `dialogPickAgent` assert.
- `TestNewSessionOpensAgentPicker` (~190): delete the whole function (superseded by `TestNewSessionUsesActiveView`).
- `TestNewSessionSingleProviderLaunchesDirectly` (~220): keep; only update its `dialogPickAgent` references — replace `if m.dialog == dialogPickAgent` with `if m.dialog != dialogNone` (keep the rest).

In `internal/ui/mouse_test.go` (both tests already get a `rec` recorder from their `pickerModel(t)` helper):

- `TestPickDirDoubleClickConfirms` (~559): the double-click used to open the pick-agent dialog; now it launches directly. Replace everything from the `if m.dialog != dialogPickAgent …` assert through the end of the function body with:

```go
	if m.dialog != dialogNone {
		t.Fatalf("double-click confirm: dialog = %v, want none (launch directly)", m.dialog)
	}
	if rec.name != "claude" || rec.dir != dirB || len(rec.args) != 0 {
		t.Errorf("double-click launched name=%q dir=%q args=%v, want claude %q []", rec.name, rec.dir, rec.args, dirB)
	}
```

- `TestPickDirDoubleClickOverridesTypedPath` (~619): same surgery — replace from its `if m.dialog != dialogPickAgent …` assert to the end of the function with:

```go
	if m.dialog != dialogNone {
		t.Fatalf("double-click: dialog = %v, want none (launch directly)", m.dialog)
	}
	if rec.dir != dirB {
		t.Errorf("double-click launched in %q, want the clicked row %q", rec.dir, dirB)
	}
```

(Delete the now-unused `m2, _ = m.Update(key("1"))` steps in both.)

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/ui/ -run 'TestNewSession|TestPickDir' -v`
Expected: FAIL — two-provider paths still open `dialogPickAgent`.

- [ ] **Step 3: Implement in `internal/ui/model.go`**

3a. Replace `launchNewSession`:

```go
// launchNewSession starts a new session in dir with the active view's
// agent — the view is the agent choice, so there is no pick dialog.
func (m Model) launchNewSession(dir string) (Model, tea.Cmd) {
	m.dialog = dialogNone
	p := store.ProviderFor(m.providers, m.list.Agent())
	if p == nil {
		m.dialog = dialogError
		m.errText = m.list.Agent().Label() + " is not available"
		return m, nil
	}
	if err := binLookPath(p.Binary()); err != nil {
		m.dialog = dialogError
		m.errText = p.Binary() + " not found on PATH"
		return m, nil
	}
	return m, m.runAgentCmd(p, dir, nil)
}
```

3b. Delete `dialogPickAgent` from the `dialogKind` const block; delete its `case dialogPickAgent:` blocks in `handleDialogKey` and `dialogView`; delete the `pendingNewDir string` field and its two remaining writes (both were inside the deleted paths — `go build` will point at any stragglers).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ && go vet ./...`
Expected: PASS / clean.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/actions_test.go internal/ui/mouse_test.go
git commit -m "feat(ui): n starts the active view's agent; drop the pick-agent dialog

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: Mouse — clickable title-bar tabs

**Files:**
- Modify: `internal/ui/mouse.go`, `internal/ui/model.go` (tabAt lives beside agentTabs)
- Test: `internal/ui/mouse_test.go`

**Interfaces:**
- Consumes: `m.agentTabs()`, `m.switchAgentView`, `agentTab`.
- Produces: `zoneTabs` zone; `func (m Model) tabAt(x int) (store.Agent, bool)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/mouse_test.go`:

```go
// TestTabAtMatchesRenderedHeader pins tabAt's geometry to View()'s header
// segments (mark + app title + two-space-separated tabs), the same
// single-source discipline as helpBar/dialogOrigin.
func TestTabAtMatchesRenderedHeader(t *testing.T) {
	m := newTwoAgentModel(t)
	pos := lipgloss.Width("✻ sm · AI Sessions  ")
	for _, tb := range m.agentTabs() {
		w := lipgloss.Width(tb.label)
		for _, dx := range []int{0, w - 1} {
			ag, ok := m.tabAt(pos + dx)
			if !ok || ag != tb.agent {
				t.Errorf("tabAt(%d) = (%v,%v), want (%v,true)", pos+dx, ag, ok, tb.agent)
			}
		}
		if ag, ok := m.tabAt(pos + w); ok && ag == tb.agent {
			t.Errorf("tabAt(%d) still hits %v past its right edge", pos+w, tb.agent)
		}
		pos += w + 2
	}
	if _, ok := m.tabAt(pos + 5); ok {
		t.Error("far right of the tabs should miss")
	}
	if _, ok := m.tabAt(0); ok {
		t.Error("the ✻ mark is not a tab")
	}
}

func TestClickTabSwitchesView(t *testing.T) {
	m := newTwoAgentModel(t)
	// x of the codex (second) tab's first cell.
	x := lipgloss.Width("✻ sm · AI Sessions  ") + lipgloss.Width(m.agentTabs()[0].label) + 2
	m2, _ := m.Update(click(x, 0))
	m = m2.(Model)
	if m.list.Agent() != store.AgentCodex {
		t.Error("clicking the codex tab must switch to the codex view")
	}
	m2, _ = m.Update(click(x, 0)) // now the active tab: label grew brackets, x still inside it
	m = m2.(Model)
	if m.list.Agent() != store.AgentCodex {
		t.Error("clicking the active tab must be a no-op")
	}
}

func TestTitleClickSingleProviderIsNoop(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(25, 0))
	m = m2.(Model)
	if m.list.Agent() != store.AgentClaude {
		t.Error("single provider: title clicks must not switch views")
	}
}
```

Update `TestZoneAt` (~line 27): change the case `{"title row", 5, 0, zoneNone, 0}` to `{"title row", 5, 0, zoneTabs, 0}`.

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/ui/ -run 'TestTabAt|TestClickTab|TestTitleClick|TestZoneAt' -v`
Expected: compile FAIL — `zoneTabs`, `m.tabAt` undefined.

- [ ] **Step 3: Implement**

3a. `internal/ui/mouse.go` — add the zone and route it. In the `zone` consts, add `zoneTabs` after `zoneHelp`. In `zoneAt`, add a first case:

```go
	switch {
	case y == 0:
		return zoneTabs, 0
	case y == 1:
		return zoneFilter, 0
```

In `handleMouse`'s left-click `switch z`, add:

```go
	case zoneTabs:
		if ag, ok := m.tabAt(msg.X); ok {
			return m.switchAgentView(ag)
		}
		return m, nil
```

3b. `internal/ui/model.go` — beside `agentTabs`:

```go
// tabAt maps a title-row x to the tab under it, mirroring View()'s header:
// the "✻ sm · AI Sessions  " prefix, then tab labels joined by two spaces.
// ok is false between and beyond tabs, and always with a single provider.
func (m Model) tabAt(x int) (store.Agent, bool) {
	pos := lipgloss.Width("✻ sm · AI Sessions  ")
	for _, tb := range m.agentTabs() {
		w := lipgloss.Width(tb.label)
		if x >= pos && x < pos+w {
			return tb.agent, true
		}
		pos += w + 2
	}
	return "", false
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ && go vet ./...`
Expected: PASS / clean.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mouse.go internal/ui/model.go internal/ui/mouse_test.go
git commit -m "feat(ui): title-bar tabs are clickable

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 8: README, full gate, and manual verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README.md**

- Features bullet — replace the "**OpenAI Codex sessions, too**" bullet's tail ("…scanned alongside Claude Code's and shown in the same list, tagged `codex` in teal-green (Claude sessions keep a coral `claude` tag) so you can tell them apart at a glance.") with:

```markdown
- **OpenAI Codex sessions, too** — when `~/.codex` exists, its sessions are
  scanned as a second **view**: the title bar grows `[Claude N]  Codex M`
  tabs, `a` (or a click) switches between them, and the whole accent theme
  follows — coral in the Claude view, teal-green in the Codex view.
```

- Key table: change the `a` row to:

```markdown
| `a` | switch between the Claude and Codex views (shown only when both agents are present) |
```

- Key table `n` row: change the parenthetical "(asks whether to launch Claude or Codex when a project has both)" to "(launches the active view's agent)".
- Mockup header line (the fenced block near the top): change `✻ sm · AI Sessions   52 sessions` to `✻ sm · AI Sessions  [Claude 52]  Codex 18`.

- [ ] **Step 2: Full gate**

Run: `make test && make vet && make build`
Expected: all green, `./sm` binary builds.

- [ ] **Step 3: Manual smoke on real data**

Run: `./sm` in a real terminal (the machine has both `~/.claude/projects` and `~/.codex`). Verify:
1. Title shows `[Claude N]  Codex M`; list is Claude-only; theme coral.
2. `a` → Codex view, teal theme, codex sessions only; `a` back restores the Claude cursor position.
3. Click the inactive tab → switches; click active → nothing.
4. `s`, search a term that only matches codex content while in the Claude view → `no matches · N hits in Codex — press a`; press `a` → hits listed.
5. `n` in each view starts the right CLI (quit it immediately).
6. `q` quits cleanly.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: README covers the Claude/Codex view tabs

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Post-plan notes (execution follow-ups, not tasks)

- **Merge**: after all tasks pass, use superpowers:finishing-a-development-branch (merge `feat/agent-tabs` → `main`, or PR — user's call; upstream PR to dukechain2333 is an option since main is synced to v0.3.1).
- **Deploy on this machine**: `make install` puts the fork build at `~/.local/bin/sm`, but Homebrew's upstream 0.3.1 sits earlier on PATH at `/opt/homebrew/bin/sm`. After install run `which sm`; if it still says homebrew, either `brew uninstall sm` / `brew unlink sm` or copy: `cp ./sm /opt/homebrew/bin/sm`. Confirm with `sm --version` + visual check of the tabs.
