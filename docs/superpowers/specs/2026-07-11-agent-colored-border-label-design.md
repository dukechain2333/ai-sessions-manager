# Agent-colored focused border and project label — Design

**Date:** 2026-07-11
**Status:** Approved (design conversation, 2026-07-11)

## Problem

The focused pane border and the bottom-left project label are always coral
(the Claude accent), even when the selection is a Codex session or a
Codex-majority project. The color should reflect the agent.

## Goal

1. The **focused** pane's border color follows the **selected session's
   agent**: coral for Claude, teal-green for Codex. The unfocused pane stays
   dim gray (focus cue preserved).
2. The **bottom-left project label** color follows the **majority agent** among
   that project's sessions.

## Colors

- Claude accent (unchanged): `lipgloss.AdaptiveColor{Light:"#C15F3C", Dark:"#D97757"}`.
- Codex accent (already used for the `codex` tag): `lipgloss.AdaptiveColor{Light:"#0A7C66", Dark:"#10A37F"}`.

## Design

### Border (model.go `View()`)

The two-pane branch currently uses the static `m.st.PaneFocused` (coral border)
for the focused pane and `m.st.PaneBlurred` (dim) for the other. Replace the
focused style with one built from the selected session's agent color:

- `borderColor := m.st.AgentAccent(selectedAgent)` where `selectedAgent` is
  `Selected().Agent` when a session is selected, else `AgentClaude` (default
  coral fallback for header/empty selections).
- The focused pane style becomes
  `lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(borderColor)`.
- The unfocused pane keeps `m.st.PaneBlurred`. Applies to both the narrow
  (single-pane) and two-pane branches.

### Project label (model.go `View()`)

The bottom-left label (from the current-project-label feature) is rendered
`Bold(true).Foreground(m.st.Accent)`. Change the foreground to the majority
agent's color:

- `labelColor := m.st.AgentAccent(m.list.projectMajorityAgent(project))` where
  `project = Selected().Project()` (the label is only shown when a session is
  selected, so a project always exists here).

### Helpers

- `styles` gains a field `CodexAccent lipgloss.AdaptiveColor` (set to the codex
  teal in `defaultStyles`) so both accents are reachable, and a method:
  ```go
  func (s styles) AgentAccent(a store.Agent) lipgloss.AdaptiveColor {
      if a == store.AgentCodex {
          return s.CodexAccent
      }
      return s.Accent
  }
  ```
- `listPane` gains:
  ```go
  // projectMajorityAgent returns the agent with the most sessions in project.
  // Ties break to the currently selected session's agent so the label and the
  // focused border agree.
  func (l *listPane) projectMajorityAgent(project string) store.Agent
  ```
  It counts `l.sessions` whose `Project() == project` by agent; the majority
  wins; on a tie (or no clear majority) it returns the selected session's agent
  (`Selected()`), falling back to `AgentClaude` when nothing is selected.

## Testing

- `styles_test`/`model_test`: `AgentAccent(AgentClaude)` == the coral accent,
  `AgentAccent(AgentCodex)` == the teal accent.
- `listpane_test`: `projectMajorityAgent` returns Codex for a Codex-majority
  project, Claude for a Claude-majority one, and the selected agent on a tie.
- `model_test` (view-level): with a selected Codex session and the list
  focused, `m.View()` contains the teal border color's ANSI escape and NOT for
  a selected Claude session (assert on the rendered color code, e.g. via
  `lipgloss.Color` rendering, or check the focused-border helper directly).

## Out of scope (YAGNI)

Coloring the unfocused pane, dialog borders, the app title/`✻` mark, or the
per-session title colors (already agent-colored). Configurable palettes.
