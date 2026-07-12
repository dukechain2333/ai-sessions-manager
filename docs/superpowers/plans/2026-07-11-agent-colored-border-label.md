# Agent-Colored Border & Project Label Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Color the focused pane border by the selected session's agent (coral=Claude, teal=Codex) and the bottom-left project label by the project's majority agent.

**Architecture:** Add `styles.AgentAccent(agent)` (coral/teal) and `listPane.projectMajorityAgent(project)`. Two `Model` helpers return the border/label `AdaptiveColor` (testable without ANSI parsing); `View()` builds the focused pane style and the label style from them.

**Tech Stack:** Go ≥1.24, Bubble Tea v1, Lipgloss v1. No new dependencies.

## Global Constraints

- Module `github.com/dukechain2333/ai-sessions-manager`; Go at `~/.local/go` (prefix shell steps with `export PATH=$HOME/.local/go/bin:$PATH`).
- Claude accent `{Light:"#C15F3C", Dark:"#D97757"}`; Codex accent `{Light:"#0A7C66", Dark:"#10A37F"}` (both already in `defaultStyles`).
- Only the FOCUSED pane border changes color; the unfocused pane keeps `PaneBlurred` (dim). No-selection (cursor on a header) falls back to the Claude accent.
- Project-label majority ties break to the selected session's agent.
- gofmt-clean; `go vet ./...`, `go test -race ./...` green; commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- After the task: `make install`. DO NOT publish/tag/release until the user asks.

---

### Task 1: Agent-colored focused border and majority-colored project label

**Files:**
- Modify: `internal/ui/styles.go` (add `CodexAccent` field + `AgentAccent` method)
- Modify: `internal/ui/listpane.go` (add `projectMajorityAgent`)
- Modify: `internal/ui/model.go` (add `focusedBorderColor`/`projectLabelColor`; use them in `View()`)
- Test: `internal/ui/styles_test.go` (new), `internal/ui/listpane_test.go`, `internal/ui/model_test.go`

**Interfaces:**
- Consumes: `store.Agent`, `store.AgentClaude`, `store.AgentCodex`; `store.Session.Project()`; `m.list.Selected() (store.Session,int,bool)`; `m.list.Sessions()`.
- Produces: `func (s styles) AgentAccent(a store.Agent) lipgloss.AdaptiveColor`; `func (l *listPane) projectMajorityAgent(project string) store.Agent`; `func (m Model) focusedBorderColor() lipgloss.AdaptiveColor`; `func (m Model) projectLabelColor() lipgloss.AdaptiveColor`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/styles_test.go`:
```go
package ui

import (
	"testing"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

func TestAgentAccent(t *testing.T) {
	st := defaultStyles()
	if st.AgentAccent(store.AgentClaude) != st.Accent {
		t.Error("claude accent should be the coral Accent")
	}
	if st.AgentAccent(store.AgentCodex) != st.CodexAccent {
		t.Error("codex accent should be the teal CodexAccent")
	}
	if st.Accent == st.CodexAccent {
		t.Error("Accent and CodexAccent must differ")
	}
}
```

Add to `internal/ui/listpane_test.go`:
```go
func TestProjectMajorityAgent(t *testing.T) {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(80, 40)
	l.SetSessions([]store.Session{
		{ID: "a", CWD: "/x/cx", Agent: store.AgentCodex, UserMessages: 1, Enriched: true, LastActivity: time.Now()},
		{ID: "b", CWD: "/x/cx", Agent: store.AgentCodex, UserMessages: 1, Enriched: true, LastActivity: time.Now()},
		{ID: "c", CWD: "/x/cx", Agent: store.AgentClaude, UserMessages: 1, Enriched: true, LastActivity: time.Now()},
		{ID: "d", CWD: "/x/cl", Agent: store.AgentClaude, UserMessages: 1, Enriched: true, LastActivity: time.Now()},
	})
	if got := l.projectMajorityAgent("cx"); got != store.AgentCodex {
		t.Errorf("cx majority = %v, want codex", got)
	}
	if got := l.projectMajorityAgent("cl"); got != store.AgentClaude {
		t.Errorf("cl majority = %v, want claude", got)
	}
}

func TestProjectMajorityAgentTieUsesSelected(t *testing.T) {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(80, 40)
	l.SetSessions([]store.Session{
		{ID: "x1", CWD: "/x/tie", Agent: store.AgentCodex, UserMessages: 1, Enriched: true, LastActivity: time.Now()},
		{ID: "c1", CWD: "/x/tie", Agent: store.AgentClaude, UserMessages: 1, Enriched: true, LastActivity: time.Now().Add(-time.Hour)},
	})
	l.selectSession(0) // codex session selected
	if got := l.projectMajorityAgent("tie"); got != store.AgentCodex {
		t.Errorf("tie with codex selected = %v, want codex", got)
	}
}
```

Add to `internal/ui/model_test.go`:
```go
func TestFocusedBorderAndLabelColorFollowAgent(t *testing.T) {
	m := newTestModel() // grouped; cursor on first session s1 (claude, project "alpha")
	if m.focusedBorderColor() != m.st.Accent {
		t.Error("claude selection should give the coral border color")
	}
	if m.projectLabelColor() != m.st.Accent {
		t.Error("claude-majority project should give the coral label color")
	}
	// Flip the selected session and its whole project to codex, refresh, re-select.
	for i := range m.list.sessions {
		if m.list.sessions[i].CWD == "/x/alpha" {
			m.list.sessions[i].Agent = store.AgentCodex
		}
	}
	m.list.SetSessions(m.list.sessions)
	if m.focusedBorderColor() != m.st.CodexAccent {
		t.Error("codex selection should give the teal border color")
	}
	if m.projectLabelColor() != m.st.CodexAccent {
		t.Error("codex-majority project should give the teal label color")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'TestAgentAccent|TestProjectMajorityAgent|TestFocusedBorderAndLabelColor'`
Expected: FAIL (undefined `CodexAccent`, `AgentAccent`, `projectMajorityAgent`, `focusedBorderColor`, `projectLabelColor`).

- [ ] **Step 3: Add the styles helper**

In `internal/ui/styles.go`, add the field to the `styles` struct after `Accent`:
```go
	Accent         lipgloss.AdaptiveColor
	CodexAccent    lipgloss.AdaptiveColor
```
In `defaultStyles()`'s returned struct literal, set it (the `codex` local already exists):
```go
		Accent:         accent,
		CodexAccent:    codex,
```
Add the method (below `defaultStyles`), importing `store`:
```go
// AgentAccent is the accent color for an agent: coral for Claude, teal for Codex.
func (s styles) AgentAccent(a store.Agent) lipgloss.AdaptiveColor {
	if a == store.AgentCodex {
		return s.CodexAccent
	}
	return s.Accent
}
```
Ensure `internal/ui/styles.go` imports `"github.com/dukechain2333/ai-sessions-manager/internal/store"`.

- [ ] **Step 4: Add `projectMajorityAgent`**

In `internal/ui/listpane.go`, add:
```go
// projectMajorityAgent returns the agent with the most sessions in project.
// Ties (and a project with no sessions) break to the selected session's agent
// so the label and the focused border agree; Claude if nothing is selected.
func (l *listPane) projectMajorityAgent(project string) store.Agent {
	claude, codex := 0, 0
	for _, s := range l.sessions {
		if s.Project() != project {
			continue
		}
		if s.Agent == store.AgentCodex {
			codex++
		} else {
			claude++
		}
	}
	switch {
	case codex > claude:
		return store.AgentCodex
	case claude > codex:
		return store.AgentClaude
	}
	if s, _, ok := l.Selected(); ok {
		return s.Agent
	}
	return store.AgentClaude
}
```

- [ ] **Step 5: Add the Model color helpers and use them in `View()`**

In `internal/ui/model.go`, add (near `projectLabelText`):
```go
// focusedBorderColor is the border color of the focused pane: the selected
// session's agent accent, or the default accent when nothing is selected.
func (m Model) focusedBorderColor() lipgloss.AdaptiveColor {
	if s, _, ok := m.list.Selected(); ok {
		return m.st.AgentAccent(s.Agent)
	}
	return m.st.Accent
}

// projectLabelColor is the bottom-left label color: the accent of the majority
// agent in the selected session's project.
func (m Model) projectLabelColor() lipgloss.AdaptiveColor {
	if s, _, ok := m.list.Selected(); ok {
		return m.st.AgentAccent(m.list.projectMajorityAgent(s.Project()))
	}
	return m.st.Accent
}
```
In `View()`, replace the focused-pane style selection. Change:
```go
		listStyle, prevStyle := m.st.PaneBlurred, m.st.PaneBlurred
		if m.focus == focusPreview {
			prevStyle = m.st.PaneFocused
		} else {
			listStyle = m.st.PaneFocused
		}
```
to:
```go
		focused := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(m.focusedBorderColor())
		listStyle, prevStyle := m.st.PaneBlurred, m.st.PaneBlurred
		if m.focus == focusPreview {
			prevStyle = focused
		} else {
			listStyle = focused
		}
```
And change the label render from:
```go
	styledLabel := lipgloss.NewStyle().Bold(true).Foreground(m.st.Accent).
		MaxWidth(m.width).Render(label)
```
to:
```go
	styledLabel := lipgloss.NewStyle().Bold(true).Foreground(m.projectLabelColor()).
		MaxWidth(m.width).Render(label)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/ui/ -run 'TestAgentAccent|TestProjectMajorityAgent|TestFocusedBorderAndLabelColor' -v`
Expected: PASS.

- [ ] **Step 7: Full verification**

Run:
```bash
export PATH=$HOME/.local/go/bin:$PATH
gofmt -w internal/ui/styles.go internal/ui/listpane.go internal/ui/model.go internal/ui/styles_test.go internal/ui/listpane_test.go internal/ui/model_test.go
gofmt -l internal cmd            # expect empty
go vet ./... && go test -race ./...
```
Expected: gofmt clean, vet clean, all tests pass (incl. `-race`).

- [ ] **Step 8: Commit**

```bash
git add internal/ui/styles.go internal/ui/listpane.go internal/ui/model.go internal/ui/styles_test.go internal/ui/listpane_test.go internal/ui/model_test.go
git commit -m "feat(ui): agent-colored focused border and majority-colored project label

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [ ] **Step 9: Live check (controller installs)**

```bash
export PATH=$HOME/.local/go/bin:$PATH && make build
tmux kill-session -t clr 2>/dev/null
tmux new-session -d -s clr -x 130 -y 40 "./sm"; sleep 3
# move onto a codex session (filter to one) and confirm the focused list border + label turn teal
tmux send-keys -t clr '/'; sleep 0.3; tmux send-keys -t clr Tab; sleep 0.3; tmux send-keys -t clr Escape; sleep 0.3
tmux capture-pane -t clr -pe | sed -n '3,6p'   # -e keeps ANSI; look for the teal vs coral border code
tmux send-keys -t clr 'q'; sleep 0.5; tmux kill-session -t clr 2>/dev/null
```
Expected: eyeballing the running app, the focused pane's border and the bottom-left project label are coral on a Claude session and teal on a Codex session. (Do NOT `make install` or publish — the controller installs after review.)
