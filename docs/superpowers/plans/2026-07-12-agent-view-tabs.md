# Agent View Modes (list ⇄ tabs) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two selectable view modes — today's mixed **list mode** (default, untouched) and a new **tab mode** where the list shows one agent at a time with title-bar tabs — switched live with `v` and defaulted by `config.json`'s `"view"`.

**Architecture:** `listPane.activeAgent` generalizes the list: `""` (zero value) is the mixed list running today's exact code paths; `claude`/`codex` filter row-building to one agent. Per-view cursor/scroll/fold state parks in a map keyed by all three values. `Model` gains `tabsMode`, the `v` toggle, mode-branched `a`/`n`, title tabs, and view-tinted chrome. Nothing is deleted — subheaders and tags simply never render in a single-agent view.

**Tech Stack:** Go ≥ 1.24, Bubble Tea / lipgloss (already in go.mod). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-12-agent-view-tabs-design.md` (v2)

## Global Constraints

- Module path: `github.com/dukechain2333/ai-sessions-manager`.
- Branch: `feat/agent-tabs` (already checked out; spec/plan committed).
- **List mode (the default) must stay byte-for-byte today's behavior** — every pre-existing test keeps passing unmodified. Only ADD tests in this plan; never edit or delete an existing one (exceptions, all in Task 6: `TestZoneAt`'s title-row case, plus pure x-offset recomputation in the help-bar click tests that the inserted `v view` item shifts — the same surgery commit 3ba1c74 performed for `s search`).
- UI copy is English. Exact new strings: config key `"view": "list" | "tabs"`; tab labels `[Claude 52]` / `Codex 18` (active bracketed, two-space separated); help-bar item `v view`; search empty state `no matches · 3 hits in Codex — press a` (singular `1 hit`).
- Two-agent signal is `len(m.providers) > 1` (Codex provider registers only when its dir exists) — same signal `launchNewSession` uses today.
- Tests: plain `testing`, existing helper style. `make test` and `make vet` green at every commit.
- Commits: conventional style, each ending with the trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: config — `"view"` key

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `Config.View string` — `"list"` (default) or `"tabs"`; `DefaultFileJSON` gains `"view": "list"`. Task 4 reads `cfg.View`.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go` (it already imports `os`, `path/filepath`, `testing`):

```go
func TestLoadView(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"view": "tabs"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil || cfg.View != "tabs" {
		t.Fatalf(`view "tabs": cfg.View=%q err=%v`, cfg.View, err)
	}
	if err := os.WriteFile(p, []byte(`{"view": "bogus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(p)
	if err != nil || cfg.View != "list" {
		t.Fatalf(`unknown view must fall back to "list": cfg.View=%q err=%v`, cfg.View, err)
	}
	if def := Default(); def.View != "list" {
		t.Fatalf(`Default().View = %q, want "list"`, def.View)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/config/ -run TestLoadView -v`
Expected: compile FAIL — `cfg.View` undefined.

- [ ] **Step 3: Implement in `internal/config/config.go`**

3a. `Config` gains a field:

```go
// Config is the resolved configuration (defaults already filled in).
type Config struct {
	TmuxEnabled bool
	Claude      AgentColors
	Codex       AgentColors
	View        string // startup view mode: "list" (mixed) or "tabs" (per-agent)
}
```

3b. `Default()` returns `View: "list"` (add the field to the literal).

3c. `DefaultFileJSON` gains the key as its first line:

```go
const DefaultFileJSON = `{
  "view": "list",
  "tmux": { "enabled": false },
  "colors": {
    "claude": { "light": "#C15F3C", "dark": "#D97757" },
    "codex":  { "light": "#0A7C66", "dark": "#10A37F" }
  }
}
`
```

3d. `fileConfig` gains `View *string \`json:"view"\`` and `Load` applies it after the colors block:

```go
	if f.View != nil && (*f.View == "list" || *f.View == "tabs") {
		cfg.View = *f.View
	}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/config/ -v`
Expected: PASS — including the existing `TestDefaultFileJSONParsesToDefault` pin, which now covers `"view"` automatically. If that pin test fails, the `DefaultFileJSON` literal and `Default()` drifted — align them.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): view key picks the startup view mode (list/tabs)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: listPane — agent views over the same rows ("" = mixed)

**Files:**
- Modify: `internal/ui/listpane.go`
- Test: `internal/ui/listpane_test.go`

**Interfaces:**
- Consumes: `store.Agent`, `store.AgentClaude`, `store.AgentCodex`.
- Produces (used by Tasks 3–6):
  - `func (l *listPane) Agent() store.Agent` — `""` = mixed list, else the single-agent view.
  - `func (l *listPane) SetAgent(a store.Agent)` — switch view, park/restore per-view state, refresh; a never-visited view starts on its first session.
  - `func (l *listPane) AgentTotal(a store.Agent) int` — sessions agent `a` would display now (honors empty toggle + filter + search).
  - `func otherAgent(a store.Agent) store.Agent` — flips claude ⇄ codex.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/listpane_test.go`:

```go
// mixedSessions is testSessions plus two codex sessions: one sharing
// project alpha, one in its own project delta.
func mixedSessions() []store.Session {
	s := testSessions()
	s = append(s,
		store.Session{ID: "x1", Slug: "-p1", CWD: "/x/alpha", Title: "Rollout in alpha", FirstPrompt: "hello", Agent: store.AgentCodex, UserMessages: 1, Enriched: true, LastActivity: time.Now().Add(-30 * time.Minute)},
		store.Session{ID: "x2", Slug: "-p4", CWD: "/x/delta", Title: "Rollout in delta", FirstPrompt: "yo", Agent: store.AgentCodex, UserMessages: 2, Enriched: true, LastActivity: time.Now().Add(-3 * time.Hour)},
	)
	return s
}

func newMixedPane() listPane {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(50, 30)
	l.SetSessions(mixedSessions())
	return l
}

func TestAgentViewFiltersRows(t *testing.T) {
	l := newMixedPane()
	if l.Agent() != "" {
		t.Fatalf("default view = %q, want \"\" (mixed)", l.Agent())
	}
	if got := l.Len(); got != 4 { // s1, s2, x1, x2 (s3 is empty and hidden)
		t.Fatalf("mixed Len = %d, want 4", got)
	}
	if got := l.AgentTotal(store.AgentClaude); got != 2 {
		t.Errorf("AgentTotal(claude) = %d, want 2", got)
	}
	if got := l.AgentTotal(store.AgentCodex); got != 2 {
		t.Errorf("AgentTotal(codex) = %d, want 2", got)
	}

	l.SetAgent(store.AgentClaude)
	if got := l.Len(); got != 2 {
		t.Fatalf("claude view Len = %d, want 2", got)
	}
	if v := l.View(); strings.Contains(v, "Rollout in alpha") || strings.Contains(v, "delta") {
		t.Errorf("claude view must not render codex rows:\n%s", v)
	}

	l.SetAgent(store.AgentCodex)
	v := l.View()
	if !strings.Contains(v, "Rollout in delta") {
		t.Errorf("codex view missing its session:\n%s", v)
	}
	if strings.Contains(v, "Fix backup script") {
		t.Errorf("codex view must not render claude rows:\n%s", v)
	}

	l.SetAgent("")
	if got := l.Len(); got != 4 {
		t.Errorf("back to mixed: Len = %d, want 4", got)
	}
}

func TestAgentTotalsHonorFilter(t *testing.T) {
	l := newMixedPane()
	l.SetFilter("rollout")
	if got := l.AgentTotal(store.AgentClaude); got != 0 {
		t.Errorf("AgentTotal(claude) under filter = %d, want 0", got)
	}
	if got := l.AgentTotal(store.AgentCodex); got != 2 {
		t.Errorf("AgentTotal(codex) under filter = %d, want 2", got)
	}
	l.SetAgent(store.AgentClaude)
	if got := l.Len(); got != 0 {
		t.Errorf("claude view under filter 'rollout': Len = %d, want 0", got)
	}
}

func TestSetAgentKeepsPerViewState(t *testing.T) {
	l := newMixedPane()
	l.ToggleFold() // cursor starts in alpha; folds it in the mixed view
	if !l.folded["alpha"] {
		t.Fatal("setup: alpha should be folded in the mixed view")
	}
	l.MoveCursor(1)
	cur, off := l.cursor, l.lineOffset

	l.SetAgent(store.AgentCodex)
	if l.folded["alpha"] {
		t.Error("codex view must start with its own (empty) fold state")
	}
	l.ToggleFold() // fold something in the codex view

	l.SetAgent("")
	if !l.folded["alpha"] {
		t.Error("mixed fold state lost across a round-trip")
	}
	if l.cursor != cur || l.lineOffset != off {
		t.Errorf("mixed cursor/offset = %d/%d, want %d/%d", l.cursor, l.lineOffset, cur, off)
	}
}

func TestSearchResultsRespectAgentView(t *testing.T) {
	l := newMixedPane()
	// Hits on one claude session (index 0) and both codex sessions (3, 4).
	hits := []store.SessionHits{{Session: 0, MsgHits: 1}, {Session: 3, MsgHits: 5}, {Session: 4, MsgHits: 2}}
	l.SetSearchResults(append([]store.SessionHits(nil), hits...))
	if got := l.Len(); got != 3 {
		t.Fatalf("mixed search Len = %d, want 3", got)
	}
	l.SetAgent(store.AgentCodex)
	l.SetSearchResults(append([]store.SessionHits(nil), hits...))
	if got := l.Len(); got != 2 {
		t.Errorf("codex search Len = %d, want 2", got)
	}
	if got := l.AgentTotal(store.AgentClaude); got != 1 {
		t.Errorf("AgentTotal(claude) in search = %d, want 1", got)
	}
}

func TestOtherAgent(t *testing.T) {
	if otherAgent(store.AgentClaude) != store.AgentCodex || otherAgent(store.AgentCodex) != store.AgentClaude {
		t.Error("otherAgent must flip between the two agents")
	}
}
```

(`TestSearchResultsRespectAgentView` re-sends a fresh hits copy after `SetAgent` because `SetSearchResults` documents ownership transfer of its slice.)

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/ui/ -run 'TestAgentViewFiltersRows|TestAgentTotalsHonorFilter|TestSetAgentKeepsPerViewState|TestSearchResultsRespectAgentView|TestOtherAgent' -v`
Expected: compile FAIL — `Agent`, `SetAgent`, `AgentTotal`, `otherAgent` undefined.

- [ ] **Step 3: Implement in `internal/ui/listpane.go`**

3a. Fields (after `tmuxLive`):

```go
	activeAgent store.Agent               // "" = mixed list; else only that agent renders
	agentTotals map[store.Agent]int       // per-agent visible counts under the current mode
	savedViews  map[store.Agent]viewState // parked cursor/scroll/fold of inactive views
```

3b. Near the `row` type:

```go
// viewState is the navigation state parked for an inactive view ("" is the
// mixed list, claude/codex the single-agent views).
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

3c. Accessors + `SetAgent` (near `ToggleGroup`):

```go
// Agent is the active view: "" is the mixed list, otherwise only that
// agent's sessions render.
func (l *listPane) Agent() store.Agent { return l.activeAgent }

// AgentTotal reports how many sessions agent a would display right now,
// honoring the empty toggle and any live filter or search results.
func (l *listPane) AgentTotal(a store.Agent) int { return l.agentTotals[a] }

// SetAgent switches the pane to view a, parking the current view's cursor,
// scroll, and fold state and restoring a's. refresh clamps the restored
// cursor if that view's rows changed while it was parked; a never-visited
// view starts on its first session (not row 0, which is a header when
// grouped — that would clear the preview).
func (l *listPane) SetAgent(a store.Agent) {
	if a == l.activeAgent {
		return
	}
	if l.savedViews == nil {
		l.savedViews = map[store.Agent]viewState{}
	}
	l.savedViews[l.activeAgent] = viewState{cursor: l.cursor, lineOffset: l.lineOffset, folded: l.folded}
	st, seen := l.savedViews[a]
	l.activeAgent = a
	l.cursor, l.lineOffset, l.folded = st.cursor, st.lineOffset, st.folded
	l.refresh()
	if !seen {
		l.cursorToFirstSession()
	}
}
```

3d. `refresh()`, search branch — partition while building rows. Replace the loop body inside `if l.search != nil { … }` so it reads:

```go
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
			if l.activeAgent != "" && ag != l.activeAgent {
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

3e. `refresh()`, browse/filter path — after `base` is built (the existing step-1 selection code is untouched), replace `l.total = len(base)` with:

```go
	// 2. Count per agent and narrow to the active view ("" keeps all).
	l.agentTotals = map[store.Agent]int{}
	for _, si := range base {
		l.agentTotals[l.sessions[si].Agent]++
	}
	act := base
	if l.activeAgent != "" {
		act = nil
		for _, si := range base {
			if l.sessions[si].Agent == l.activeAgent {
				act = append(act, si)
			}
		}
	}
	l.total = len(act)
```

Then change the two later `for _, si := range base` loops (per-project counts; row building) to `for _, si := range act`, and renumber the step comments (2→3, 3→4). Everything else — including the `groupByAgent` subheader branch, which is naturally inert in a single-agent view — stays.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/`
Expected: PASS — the new tests and every pre-existing test (`activeAgent` zero value keeps mixed behavior).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/listpane.go internal/ui/listpane_test.go
git commit -m "feat(ui): listPane agent views — mixed list plus filtered claude/codex views

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: listPane — single-agent view visuals (no tags, view accents, search hint)

**Files:**
- Modify: `internal/ui/listpane.go`
- Test: `internal/ui/listpane_test.go`

**Interfaces:**
- Consumes: `l.activeAgent`, `l.styles.AgentAccent`, `agentTitle` (exists at the bottom of listpane.go), `otherAgent`.
- Produces: `func (l *listPane) accent() lipgloss.AdaptiveColor` — active view's accent (`AgentAccent("")` is already the Claude accent).

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/listpane_test.go`:

```go
func TestAgentViewHidesTagsAndSubheaders(t *testing.T) {
	l := newMixedPane()
	l.ToggleAgentGroup() // a bare field write would skip refresh() and leave rows stale
	if v := l.View(); !strings.Contains(v, "─ Claude ─") {
		t.Fatalf("setup: mixed view with groupByAgent should render subheaders:\n%s", v)
	}
	l.SetAgent(store.AgentClaude)
	v := l.View()
	if strings.Contains(v, "claude") || strings.Contains(v, "codex") {
		t.Errorf("single-agent view must not render per-row agent tags:\n%s", v)
	}
	if strings.Contains(v, "─ Claude ─") || strings.Contains(v, "─ Codex ─") {
		t.Errorf("single-agent view must not render agent subheaders:\n%s", v)
	}
	l.SetAgent("")
	if v := l.View(); !strings.Contains(v, "claude") || !strings.Contains(v, "codex") {
		t.Errorf("mixed view must keep per-row tags:\n%s", v)
	}
}

func TestAccentFollowsActiveView(t *testing.T) {
	l := newMixedPane()
	if l.accent() != l.styles.Accent {
		t.Error("mixed view accent should be the default (claude) accent")
	}
	l.SetAgent(store.AgentCodex)
	if l.accent() != l.styles.CodexAccent {
		t.Error("codex view accent should be the codex accent")
	}
}

func TestSearchEmptyStateHintsOtherView(t *testing.T) {
	l := newMixedPane()
	l.SetAgent(store.AgentClaude)
	// Hits land only on the two codex sessions (slice indices 3 and 4).
	l.SetSearchResults([]store.SessionHits{{Session: 3, MsgHits: 5}, {Session: 4, MsgHits: 1}})
	if got := l.Len(); got != 0 {
		t.Fatalf("claude view Len = %d, want 0", got)
	}
	if v := l.View(); !strings.Contains(v, "no matches · 2 hits in Codex — press a") {
		t.Errorf("empty search view = %q, want the cross-view hint", v)
	}
	// One hit: singular wording.
	l.SetAgent(store.AgentCodex)
	l.SetSearchResults([]store.SessionHits{{Session: 0, MsgHits: 2}}) // s1, claude
	if v := l.View(); !strings.Contains(v, "no matches · 1 hit in Claude — press a") {
		t.Errorf("singular hint missing: %q", v)
	}
	// The mixed list keeps the bare string.
	l.SetAgent("")
	l.SetSearchResults([]store.SessionHits{})
	if v := l.View(); !strings.Contains(v, "no matches") || strings.Contains(v, "press a") {
		t.Errorf("mixed empty search view = %q, want bare \"no matches\"", v)
	}
}
```

(`mixedSessions` titles avoid the lowercase strings `claude`/`codex`, so the tag assertions are exact.)

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/ui/ -run 'TestAgentViewHidesTags|TestAccentFollowsActiveView|TestSearchEmptyStateHints' -v`
Expected: compile FAIL (`l.accent` undefined); after a stub, tag/hint assertions FAIL.

- [ ] **Step 3: Implement in `internal/ui/listpane.go`**

3a. The accent helper (near `SetAgent`):

```go
// accent is the active view's accent: the agent's color in a single-agent
// view, the default (Claude) accent in the mixed list.
func (l *listPane) accent() lipgloss.AdaptiveColor {
	return l.styles.AgentAccent(l.activeAgent)
}
```

3b. In `refresh()`'s grouped row-building, subheaders must not appear in a single-agent view even with `groupByAgent` on. The existing guard `projectHasBothAgents(l.sessions, projSessions)` already returns false there (projSessions is filtered), so **no change is needed** — this is covered by `TestAgentViewHidesTagsAndSubheaders`'s setup asserting subheaders in mixed and absence in the agent view.

3c. In `View()`'s empty-state block, replace:

```go
	if l.total == 0 {
		if l.search != nil || l.filter != "" {
			return l.padHeight(l.styles.ListMeta.Render("no matches"))
		}
		return l.padHeight(l.styles.ListMeta.Render("no sessions"))
	}
```

with:

```go
	if l.total == 0 {
		if l.search != nil && l.activeAgent != "" {
			if n := l.agentTotals[otherAgent(l.activeAgent)]; n > 0 {
				plural := "s"
				if n == 1 {
					plural = ""
				}
				hint := fmt.Sprintf("no matches · %d hit%s in %s — press a", n, plural, agentTitle(otherAgent(l.activeAgent)))
				return l.padHeight(l.styles.ListMeta.Render(hint))
			}
		}
		if l.search != nil || l.filter != "" {
			return l.padHeight(l.styles.ListMeta.Render("no matches"))
		}
		return l.padHeight(l.styles.ListMeta.Render("no sessions"))
	}
```

3d. In `View()`'s header branch, tint selection/count/dot with the view accent only in single-agent views. Replace the style pick:

```go
			style := l.styles.GroupHeader
			if i == l.cursor {
				style = l.styles.GroupHeaderSel
				if l.activeAgent != "" {
					style = style.Foreground(l.accent())
				}
			}
```

the count split:

```go
			rendered := style.Render(label)
			if suffix := " " + count; strings.HasSuffix(label, suffix) {
				cntStyle := l.styles.GroupCount
				if l.activeAgent != "" {
					cntStyle = cntStyle.Foreground(l.accent())
				}
				rendered = style.Render(label[:len(label)-len(suffix)]) + " " + cntStyle.Render(count)
			}
```

and the live-tmux dot (a filtered view must not use the all-sessions majority):

```go
			if l.projectHasLiveTmux(r.project) {
				dot := l.styles.AgentAccent(l.projectMajorityAgent(r.project))
				if l.activeAgent != "" {
					dot = l.accent()
				}
				rendered += " " + lipgloss.NewStyle().Foreground(dot).Render("●")
			}
```

3e. In `View()`'s session-row tail, make the tag mixed-only. Replace from `tag := s.Agent.Label()` down to the `lines = append(…)` call with:

```go
		prefix := "  "
		titleStyle, metaStyle := l.styles.ListTitle, l.styles.ListMeta
		if i == l.cursor {
			prefix = "▶ "
			titleStyle, metaStyle = l.styles.ListTitleSel, l.styles.ListMetaSel
			if s.Agent == store.AgentCodex {
				titleStyle = l.styles.CodexTitleSel
			}
		}
		titleWidth := l.width
		marker := ""
		if l.tmuxLive[tmuxNameFor(s)] {
			titleWidth -= 2 // reserve space for " ●"
			marker = " " + lipgloss.NewStyle().Foreground(l.styles.AgentAccent(s.Agent)).Render("●")
		}
		// The mixed list tags every row with its agent; a single-agent view
		// doesn't need to — the tab bar carries the identity.
		metaLine := metaStyle.Render(store.Truncate("  "+meta, l.width))
		if l.activeAgent == "" {
			tag := s.Agent.Label()
			tagStyle := l.styles.ClaudeTag
			if s.Agent == store.AgentCodex {
				tagStyle = l.styles.CodexTag
			}
			metaLine = metaStyle.Render(store.Truncate("  "+meta, l.width-len(tag)-3)) + " " + tagStyle.Render(tag)
		}
		lines = append(lines,
			titleStyle.Render(store.Truncate(prefix+title, titleWidth))+marker,
			metaLine,
			"")
```

(Selected title/meta styles need no accent() work: in the codex view every selected row is codex and already takes `CodexTitleSel`; in the claude view `ListTitleSel` is already coral.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/`
Expected: PASS — new tests plus all pre-existing tag/subheader tests (mixed default unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/listpane.go internal/ui/listpane_test.go
git commit -m "feat(ui): single-agent views drop tags/subheaders, tint by view, hint across views

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Model — `v` mode toggle, mode-branched `a`, title tabs, config default, chrome tint

**Files:**
- Modify: `internal/ui/model.go`, `internal/ui/styles.go`
- Test: `internal/ui/model_test.go`

**Interfaces:**
- Consumes: `l.SetAgent/Agent/AgentTotal`, `otherAgent`, `agentTitle`, `cfg.View`, `m.st.AgentAccent`.
- Produces (used by Tasks 5–6):
  - `Model.tabsMode bool`, `Model.tabView store.Agent` fields.
  - `func (m *Model) toggleViewMode()`; `func (m *Model) setAgentView(a store.Agent)`.
  - `func (m Model) switchAgentView(a store.Agent) (tea.Model, tea.Cmd)` — guarded; used by `a` and tab clicks.
  - `type agentTab struct { label string; agent store.Agent }`; `func (m Model) agentTabs() []agentTab` — nil unless `tabsMode && len(providers) > 1`.
  - `func (s styles) TitleMarkFor(a store.Agent) string` (replaces `TitleMark`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/model_test.go`:

```go
// newTwoAgentModel is newTestModel with a second (codex) provider and a
// mixed-agent session set, for mode/view tests. Starts in list mode.
func newTwoAgentModel(t *testing.T) Model {
	t.Helper()
	// The claude dir must EXIST: with it missing and a codex provider
	// registered, defaultTabView legitimately picks Codex and the
	// claude-first assertions below would be wrong.
	m := New(t.TempDir(), "/nonexistent-codex-dir", config.Default())
	m.providers = append(m.providers, store.NewCodexProvider(t.TempDir()))
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(Model)
	m2, _ = m.Update(scanDoneMsg{sessions: mixedSessions()})
	return m2.(Model)
}

func TestVKeyTogglesViewMode(t *testing.T) {
	m := newTwoAgentModel(t)
	if m.tabsMode || m.list.Agent() != "" {
		t.Fatalf("default = tabsMode=%v agent=%q, want list mode / mixed", m.tabsMode, m.list.Agent())
	}
	m2, cmd := m.Update(key("v"))
	m = m2.(Model)
	if !m.tabsMode || m.list.Agent() != store.AgentClaude {
		t.Errorf("v: tabsMode=%v agent=%q, want tab mode / claude", m.tabsMode, m.list.Agent())
	}
	if cmd == nil {
		t.Error("mode switch should reload the preview")
	}
	m2, _ = m.Update(key("v"))
	m = m2.(Model)
	if m.tabsMode || m.list.Agent() != "" {
		t.Errorf("v v: tabsMode=%v agent=%q, want list mode / mixed", m.tabsMode, m.list.Agent())
	}
}

func TestAKeyPerMode(t *testing.T) {
	m := newTwoAgentModel(t)
	before := m.list.groupByAgent
	m2, _ := m.Update(key("a"))
	m = m2.(Model)
	if m.list.groupByAgent == before {
		t.Error("list mode `a` must toggle agent subgrouping")
	}
	flag := m.list.groupByAgent
	m2, _ = m.Update(key("v"))
	m = m2.(Model)
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	if m.list.Agent() != store.AgentCodex {
		t.Error("tab mode `a` must switch to the codex view")
	}
	if m.list.groupByAgent != flag {
		t.Error("tab mode `a` must not touch subgrouping")
	}
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	if m.list.Agent() != store.AgentClaude {
		t.Error("tab mode `a` must switch back to claude")
	}
}

func TestTabViewRememberedAcrossModes(t *testing.T) {
	m := newTwoAgentModel(t)
	m2, _ := m.Update(key("v")) // tabs: claude
	m = m2.(Model)
	m2, _ = m.Update(key("a")) // codex
	m = m2.(Model)
	m2, _ = m.Update(key("v")) // back to list
	m = m2.(Model)
	if m.list.Agent() != "" {
		t.Fatalf("list mode agent = %q, want mixed", m.list.Agent())
	}
	m2, _ = m.Update(key("v")) // tabs again
	m = m2.(Model)
	if m.list.Agent() != store.AgentCodex {
		t.Errorf("re-entering tab mode = %q, want the remembered codex view", m.list.Agent())
	}
}

func TestTitleTabsOnlyInTabMode(t *testing.T) {
	m := newTwoAgentModel(t)
	if v := m.View(); !strings.Contains(v, "4 sessions") || strings.Contains(v, "[Claude") {
		t.Errorf("list mode title must keep the plain count:\n%s", v)
	}
	m2, _ := m.Update(key("v"))
	m = m2.(Model)
	v := m.View()
	if !strings.Contains(v, "[Claude 2]") || !strings.Contains(v, "Codex 2") {
		t.Errorf("tab mode title must show both tabs, active bracketed:\n%s", v)
	}
	if strings.Contains(v, "sessions") {
		t.Errorf("tab mode title must not show the old count string:\n%s", v)
	}
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	if v := m.View(); !strings.Contains(v, "[Codex 2]") || !strings.Contains(v, "Claude 2") {
		t.Errorf("codex view must bracket the codex tab:\n%s", v)
	}
}

func TestTitleTabsNeedTwoProviders(t *testing.T) {
	m := newTestModel() // single provider
	m2, _ := m.Update(key("v"))
	m = m2.(Model)
	if !m.tabsMode {
		t.Fatal("v should still enter tab mode with one provider")
	}
	if v := m.View(); strings.Contains(v, "[Claude") || !strings.Contains(v, "2 sessions") {
		t.Errorf("single-provider tab mode keeps the plain count:\n%s", v)
	}
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	if m.list.Agent() != store.AgentClaude {
		t.Error("single provider: tab-mode `a` must be a no-op")
	}
}

func TestStartupModeFromConfig(t *testing.T) {
	cfg := config.Default()
	cfg.View = "tabs"
	m := New("/nonexistent-projects-dir", "/nonexistent-codex-dir", cfg)
	if !m.tabsMode || m.list.Agent() != store.AgentClaude {
		t.Errorf("config view=tabs: tabsMode=%v agent=%q, want tabs/claude", m.tabsMode, m.list.Agent())
	}
	m = New("/nonexistent-projects-dir", t.TempDir(), cfg) // codex registers, claude dir missing
	if m.list.Agent() != store.AgentCodex {
		t.Errorf("claude dir missing: startup tab view = %q, want codex", m.list.Agent())
	}
}

func TestChromeColorsFollowActiveView(t *testing.T) {
	m := newTwoAgentModel(t)
	m2, _ := m.Update(key("v"))
	m = m2.(Model)
	m2, _ = m.Update(key("a")) // codex view
	m = m2.(Model)
	// Even with nothing selected the color keys off the view, not the
	// selection — the branch the old selected-session logic gets wrong.
	m.list.SetFilter("zzzz-no-match")
	if _, _, ok := m.list.Selected(); ok {
		t.Fatal("setup: filter should leave no selection")
	}
	if m.focusedBorderColor() != m.st.CodexAccent {
		t.Error("empty codex view should still give the teal border color")
	}
}

func TestSwitchKeepsFilterApplied(t *testing.T) {
	m := newTwoAgentModel(t)
	m2, _ := m.Update(key("v"))
	m = m2.(Model)
	m2, _ = m.Update(key("/"))
	m = m2.(Model)
	for _, r := range "rollout" {
		m2, _ = m.Update(key(string(r)))
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // back to list focus
	m = m2.(Model)
	if got := m.list.Len(); got != 0 {
		t.Fatalf("claude view filtered by 'rollout': Len = %d, want 0", got)
	}
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	if got := m.list.Len(); got != 2 {
		t.Errorf("codex view must re-apply the live filter: Len = %d, want 2", got)
	}
}
```

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/ui/ -run 'TestVKey|TestAKeyPerMode|TestTabView|TestTitleTabs|TestStartupMode|TestChromeColors|TestSwitchKeepsFilter' -v`
Expected: compile FAIL — `m.tabsMode` undefined.

- [ ] **Step 3: Implement in `internal/ui/model.go`**

3a. Fields (after `focus focusArea` in the `Model` struct):

```go
	tabsMode bool        // false: mixed list (default); true: per-agent tab views
	tabView  store.Agent // last active tab view, restored on re-entering tab mode
```

3b. Helpers (near `toggleSearchLayer`):

```go
// defaultTabView is the first tab view: Claude, unless its projects dir is
// missing while a Codex provider registered.
func (m Model) defaultTabView() store.Agent {
	if len(m.providers) > 1 && !m.providers[0].Available() {
		return store.AgentCodex
	}
	return store.AgentClaude
}

// setAgentView switches the list view and re-tints the one piece of chrome
// not re-derived every render: the filter prompt. AgentAccent("") is the
// default accent, so the mixed list keeps today's coral prompt.
func (m *Model) setAgentView(a store.Agent) {
	m.list.SetAgent(a)
	m.filterInput.PromptStyle = lipgloss.NewStyle().Foreground(m.st.AgentAccent(a))
}

// toggleViewMode flips list ⇄ tab mode. Entering tab mode restores the last
// tab view (Claude on first entry); leaving parks it and returns to the
// mixed list.
func (m *Model) toggleViewMode() {
	if m.tabsMode {
		m.tabsMode = false
		m.tabView = m.list.Agent()
		m.setAgentView("")
		return
	}
	m.tabsMode = true
	if m.tabView == "" {
		m.tabView = m.defaultTabView()
	}
	m.setAgentView(m.tabView)
}

// switchAgentView activates tab view a. No-op outside tab mode, with a
// single provider, or when a is already active. Shared by the `a` key and
// title-tab clicks.
func (m Model) switchAgentView(a store.Agent) (tea.Model, tea.Cmd) {
	if !m.tabsMode || len(m.providers) <= 1 || a == "" || a == m.list.Agent() {
		return m, nil
	}
	m.setAgentView(a)
	m.lastClickRow = -1 // rows renumbered — a stale click must not pair
	return m, m.loadTranscriptCmd()
}
```

3c. In `New()`, wire the config default (after the `ret := Model{…}` literal and before `ret.index, ret.indexErr = …`):

```go
	if cfg.View == "tabs" {
		ret.tabsMode = true
		ret.tabView = ret.defaultTabView()
		ret.setAgentView(ret.tabView)
	}
```

3d. In `handleKey`'s focusList switch, rebind `a` and add `v`:

```go
		case "a":
			if m.tabsMode {
				return m.switchAgentView(otherAgent(m.list.Agent()))
			}
			m.list.ToggleAgentGroup()
			return m, m.loadTranscriptCmd()
		case "v":
			m.toggleViewMode()
			m.lastClickRow = -1 // rows renumbered
			// A mode switch can land on the same selected session ID;
			// clear previewFor so the reload isn't short-circuited
			// (same precedent as toggleSearchLayer).
			m.previewFor = ""
			return m, m.loadTranscriptCmd()
```

3e. The tab table (near `projectLabelText`, which mouse code mirrors the same way):

```go
// agentTab is one title-bar tab: its rendered label and the agent a click
// on it activates. View() and the mouse hit-test share this table.
type agentTab struct {
	label string
	agent store.Agent
}

// agentTabs returns the title-bar tabs (Claude first, active bracketed,
// live per-view counts), or nil in list mode / with a single provider.
func (m Model) agentTabs() []agentTab {
	if !m.tabsMode || len(m.providers) <= 1 {
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

3f. In `View()`, replace from `count := fmt.Sprintf("%d sessions", m.list.Len())` through the `header := lipgloss.JoinHorizontal(…)` statement with:

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
		m.st.TitleMarkFor(m.list.Agent()), // ✻ in the active view's accent
		m.st.AppTitle.Render(" sm · AI Sessions  "),
	}
	for i, tb := range tabs {
		st := m.st.Count
		if tb.agent == m.list.Agent() {
			st = lipgloss.NewStyle().Bold(true).Foreground(m.st.AgentAccent(tb.agent))
		}
		lbl := tb.label
		if i < len(tabs)-1 {
			lbl += "  " // two-space separator; tabAt (Task 6) mirrors this
		}
		segs = append(segs, st.Render(lbl))
	}
	segs = append(segs, m.st.Count.Render(status))
	header := lipgloss.JoinHorizontal(lipgloss.Top, segs...)
```

(With tabs, the `· N matched` suffix is dropped — the tab counts carry it. In list mode and single-provider tab mode the strings are exactly today's.)

3g. Chrome colors — replace `focusedBorderColor` and `projectLabelColor` with view-branched versions (list mode keeps today's logic):

```go
// focusedBorderColor is the border color of the focused pane: the active
// view's accent in tab mode; in the mixed list, the selected session's
// agent accent (default accent with no selection), as before.
func (m Model) focusedBorderColor() lipgloss.AdaptiveColor {
	if a := m.list.Agent(); a != "" {
		return m.st.AgentAccent(a)
	}
	if s, _, ok := m.list.Selected(); ok {
		return m.st.AgentAccent(s.Agent)
	}
	return m.st.Accent
}

// projectLabelColor is the bottom-left label color: the active view's
// accent in tab mode; the majority agent of the selected session's project
// in the mixed list, as before.
func (m Model) projectLabelColor() lipgloss.AdaptiveColor {
	if a := m.list.Agent(); a != "" {
		return m.st.AgentAccent(a)
	}
	if s, _, ok := m.list.Selected(); ok {
		return m.st.AgentAccent(m.list.projectMajorityAgent(s.Project()))
	}
	return m.st.Accent
}
```

3h. In `internal/ui/styles.go`, replace `TitleMark` with:

```go
// TitleMarkFor is the ✻ mark tinted with agent a's accent ("" — the mixed
// list — gets the default accent).
func (s styles) TitleMarkFor(a store.Agent) string {
	return lipgloss.NewStyle().Foreground(s.AgentAccent(a)).Render("✻")
}
```

(`TitleMark` had exactly one caller — the `View()` header, rewritten in 3f. `go build ./...` confirms.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ && go vet ./...`
Expected: PASS / clean. Every pre-existing model test exercises list mode and must pass untouched.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/styles.go internal/ui/model_test.go
git commit -m "feat(ui): v toggles list/tab modes; tab mode gets title tabs and view-tinted chrome

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Model — tab-mode `n` launches the active agent

**Files:**
- Modify: `internal/ui/model.go`
- Test: `internal/ui/actions_test.go`

**Interfaces:**
- Consumes: `m.list.Agent()`, `store.ProviderFor`, `binLookPath`, `m.runAgentCmd`.
- List mode keeps `dialogPickAgent` and the single-provider fast path exactly as today.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/actions_test.go`:

```go
func TestNewSessionUsesActiveTabView(t *testing.T) {
	m := newTwoAgentModel(t)
	m2, _ := m.Update(key("v")) // tab mode, claude view
	m = m2.(Model)
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir // s1 (claude) is the selection
	rec := &resumeRecorder{}
	m.runCmd = rec.cmd
	m2, _ = m.Update(key("n"))
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Fatalf("tab-mode `n`: dialog = %v, want none (no agent picker)", m.dialog)
	}
	if rec.name != "claude" || rec.dir != dir {
		t.Errorf("claude view `n` launched %q in %q, want claude in %q", rec.name, rec.dir, dir)
	}

	dir2 := t.TempDir()
	for i := range m.list.sessions {
		if m.list.sessions[i].Agent == store.AgentCodex {
			m.list.sessions[i].CWD = dir2
		}
	}
	m2, _ = m.Update(key("a")) // codex view
	m = m2.(Model)
	m2, _ = m.Update(key("n"))
	m = m2.(Model)
	if rec.name != "codex" || rec.dir != dir2 {
		t.Errorf("codex view `n` launched %q in %q, want codex in %q", rec.name, rec.dir, dir2)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run TestNewSessionUsesActiveTabView -v`
Expected: FAIL — the two-provider path opens `dialogPickAgent`.

- [ ] **Step 3: Implement**

In `internal/ui/model.go`, `launchNewSession` gains a tab-mode branch before the existing logic (the rest of the function is unchanged):

```go
// launchNewSession starts a new session in dir. In tab mode the view IS
// the agent choice, so it launches directly. In list mode: a single
// provider launches directly; two or more fall back to dialogPickAgent.
func (m Model) launchNewSession(dir string) (Model, tea.Cmd) {
	m.dialog = dialogNone
	if a := m.list.Agent(); a != "" {
		p := store.ProviderFor(m.providers, a)
		if p == nil {
			m.dialog = dialogError
			m.errText = a.Label() + " is not available"
			return m, nil
		}
		if err := binLookPath(p.Binary()); err != nil {
			m.dialog = dialogError
			m.errText = p.Binary() + " not found on PATH"
			return m, nil
		}
		return m, m.runAgentCmd(p, dir, nil)
	}
	if len(m.providers) == 1 {
		p := m.providers[0]
		if err := binLookPath(p.Binary()); err != nil {
			m.dialog = dialogError
			m.errText = p.Binary() + " not found on PATH"
			return m, nil
		}
		return m, m.runAgentCmd(p, dir, nil)
	}
	m.pendingNewDir = dir
	m.dialog = dialogPickAgent
	return m, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/`
Expected: PASS — including the untouched `TestNewSessionPicker` / `TestNewSessionTypedPath` / `TestNewSessionOpensAgentPicker` / `TestNewSessionSingleProviderLaunchesDirectly` (all list-mode).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/actions_test.go
git commit -m "feat(ui): tab-mode n starts the active view's agent directly

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Mouse — clickable tabs, `v view` help button

**Files:**
- Modify: `internal/ui/mouse.go`, `internal/ui/model.go` (`tabAt` beside `agentTabs`)
- Test: `internal/ui/mouse_test.go`

**Interfaces:**
- Consumes: `m.agentTabs()`, `m.switchAgentView`.
- Produces: `zoneTabs`; `func (m Model) tabAt(x int) (store.Agent, bool)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/mouse_test.go`:

```go
// TestTabAtMatchesRenderedHeader pins tabAt's geometry to View()'s header
// segments (mark + app title + two-space-separated tabs) — the same
// single-source discipline as helpBar and dialogOrigin.
func TestTabAtMatchesRenderedHeader(t *testing.T) {
	m := newTwoAgentModel(t)
	m2, _ := m.Update(key("v"))
	m = m2.(Model)
	pos := lipgloss.Width("✻ sm · AI Sessions  ")
	for _, tb := range m.agentTabs() {
		w := lipgloss.Width(tb.label)
		for _, dx := range []int{0, w - 1} {
			ag, ok := m.tabAt(pos + dx)
			if !ok || ag != tb.agent {
				t.Errorf("tabAt(%d) = (%v,%v), want (%v,true)", pos+dx, ag, ok, tb.agent)
			}
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
	m2, _ := m.Update(key("v"))
	m = m2.(Model)
	x := lipgloss.Width("✻ sm · AI Sessions  ") + lipgloss.Width(m.agentTabs()[0].label) + 2
	m2, _ = m.Update(click(x, 0))
	m = m2.(Model)
	if m.list.Agent() != store.AgentCodex {
		t.Error("clicking the codex tab must switch to the codex view")
	}
	m2, _ = m.Update(click(x, 0)) // now inside the active (bracketed) codex tab
	m = m2.(Model)
	if m.list.Agent() != store.AgentCodex {
		t.Error("clicking the active tab must be a no-op")
	}
}

func TestTitleClickInertInListMode(t *testing.T) {
	m := newTwoAgentModel(t) // list mode
	m2, _ := m.Update(click(25, 0))
	m = m2.(Model)
	if m.list.Agent() != "" || m.tabsMode {
		t.Error("list mode: title clicks must not switch modes or views")
	}
}

func TestHelpBarHasViewButton(t *testing.T) {
	m := newTwoAgentModel(t)
	if !strings.Contains(helpLineFor(m.helpItems()), "v view") {
		t.Error("help bar should list the v view toggle")
	}
}
```

Update one existing case in `TestZoneAt` (~line 37): change `{"title row", 5, 0, zoneNone, 0}` to `{"title row", 5, 0, zoneTabs, 0}` — the title row is now a zone; x=5 simply misses every tab. **This is the single sanctioned edit to a pre-existing test.**

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/ui/ -run 'TestTabAt|TestClickTab|TestTitleClick|TestHelpBarHasView|TestZoneAt' -v`
Expected: compile FAIL — `zoneTabs`, `m.tabAt` undefined.

- [ ] **Step 3: Implement**

3a. `internal/ui/model.go`, beside `agentTabs`:

```go
// tabAt maps a title-row x to the tab under it, mirroring View()'s header:
// the "✻ sm · AI Sessions  " prefix, then tab labels joined by two spaces.
// ok is false between/beyond tabs, in list mode, and with one provider
// (agentTabs is nil in both of the latter cases).
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

3b. `internal/ui/mouse.go` — add `zoneTabs` to the `zone` consts (after `zoneHelp`); make the title row a zone in `zoneAt`:

```go
	switch {
	case y == 0:
		return zoneTabs, 0
	case y == 1:
		return zoneFilter, 0
	case y == m.height-1:
		return zoneHelp, 0
	}
```

and route it in `handleMouse`'s left-click `switch z`:

```go
	case zoneTabs:
		if ag, ok := m.tabAt(msg.X); ok {
			return m.switchAgentView(ag)
		}
		return m, nil
```

3c. `internal/ui/mouse.go` — add the help item to the `helpBar` table, right after `{"a agent", runeKey("a")}`:

```go
	{"v view", runeKey("v")},
```

(`clickHelp` and rendering are table-driven, so the button works with no further code. The full bar now needs ~113 columns; update the "~105 columns" comment in `model.go`'s `View()` to say ~113.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ && go vet ./...`
Expected: PASS / clean.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mouse.go internal/ui/model.go internal/ui/mouse_test.go
git commit -m "feat(ui): clickable title tabs and a v-view help button

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: README, full gate, manual verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README.md**

- Features — replace the tail of the "**OpenAI Codex sessions, too**" bullet ("…shown in the same list, tagged `codex` in teal-green (Claude sessions keep a coral `claude` tag) so you can tell them apart at a glance.") with:

```markdown
- **OpenAI Codex sessions, too** — when `~/.codex` exists, its sessions are
  scanned alongside Claude Code's. Two view modes, toggled with `v`: the
  default **list** mode shows one mixed list (rows tagged `claude` /
  `codex`, with optional per-project agent subheaders on `a`), while **tab**
  mode shows one agent at a time — the title bar grows `[Claude N]  Codex M`
  tabs, `a` or a click switches views, and the whole accent theme follows
  (coral for Claude, teal-green for Codex). Set `"view": "tabs"` in
  `config.json` to start there.
```

- Key table — change the `a` row and add a `v` row after it:

```markdown
| `a` | list mode: toggle per-project agent subheaders (`─ Claude ─` / `─ Codex ─`); tab mode: switch the Claude ⇄ Codex view |
| `v` | toggle view mode: mixed list ⇄ per-agent tabs |
```

- Key table `n` row — change the parenthetical "(asks whether to launch Claude or Codex when a project has both)" to "(in tab mode, launches the active view's agent; in list mode it asks when both agents are installed)".
- Configuration section (the `config.json` docs) — document the new key alongside `tmux`/`colors`:

```markdown
- `"view"`: `"list"` (default) or `"tabs"` — the view mode `sm` starts in.
  `v` toggles it live either way.
```

- [ ] **Step 2: Full gate**

Run: `make test && make vet && make build`
Expected: all green, `./sm` builds.

- [ ] **Step 3: Manual smoke on real data**

Run `./sm` in a real terminal (this machine has both `~/.claude/projects` and `~/.codex`):

1. Starts in list mode — mixed list with tags, title `N sessions`; `a` still toggles subheaders (upstream behavior intact).
2. `v` → tab mode: `[Claude N]  Codex M` tabs, coral theme, Claude-only rows, tags gone.
3. `a` → Codex view, teal theme everywhere (border, ✻, prompt, selection); `a` back restores the Claude cursor.
4. Click the inactive tab → switches; click the active one → nothing; `v` → back to the mixed list with its old cursor/folds.
5. In tab mode, `s`-search a term that only matches codex content while in the Claude view → `no matches · N hits in Codex — press a`; `a` shows them.
6. In tab mode `n` starts the active view's CLI directly; in list mode `n` still asks (two providers). Quit the launched CLIs immediately.
7. `q` quits cleanly.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: README covers list/tab view modes and the v toggle

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Post-plan notes (execution follow-ups, not tasks)

- **Merge**: after all tasks pass, use superpowers:finishing-a-development-branch (merge `feat/agent-tabs` → `main` or PR; an upstream PR to dukechain2333 is viable since main is synced to v0.3.1 and list mode is untouched).
- **Deploy on this machine**: `make install` puts the fork build at `~/.local/bin/sm`, but Homebrew's upstream 0.3.1 sits earlier on PATH at `/opt/homebrew/bin/sm`. After install run `which sm`; if it still says homebrew, `brew unlink sm` (or copy over it). Then set `"view": "tabs"` in `~/.config/sm/config.json` for the user's preferred default, and confirm with a visual check.
