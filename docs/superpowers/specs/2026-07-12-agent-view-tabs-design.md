# Agent view tabs (Claude ⇄ Codex) — Design

**Date:** 2026-07-12
**Status:** Approved (design conversation + interactive mock, 2026-07-12)

## Problem

Since Codex support (v0.3.0) the list mixes both agents' sessions, telling
them apart only by a per-row `claude`/`codex` tag and an opt-in per-project
`─ Claude ─ / ─ Codex ─` subheader toggle (`a`). Working in both agents, the
mixed list stays noisy: you scan past sessions of the agent you don't care
about right now, and the separation never rises above decoration.

## Goal

1. The list shows **exactly one agent at a time**: a Claude view and a Codex
   view. `a` — and clicking the title-bar tabs — switches between them.
2. Which view you're in is unmistakable: title-bar tabs plus the whole accent
   theme (focused border, selection, ✻ mark, tmux dots) switching coral ⇄
   teal.
3. Each view keeps its own cursor, scroll, and fold state; switching back and
   forth loses nothing.
4. Filter and full-text search operate on the current view only, with a
   cross-view hint when the other view has hits.
5. Machines with a single agent see no tabs and zero behavior change.

## Design

### Views and switching

The list pane gains an **active agent** (`claude` by default at startup;
`codex` when Claude's provider is unavailable). Row building — grouping,
folding, filtering, search results — considers only sessions whose
`Session.Agent` matches the active agent.

`a` toggles the active view. It works in every list-focused mode, including
while a filter or full-text search is applied (the query re-applies to the
other view) — unlike the old `a`, which was a no-op in search mode. While the
filter input has focus, `a` still types the letter, as today.

The strict two-view toggle **replaces** the mixed list; there is no "all"
state.

### Title bar

The session count becomes two tabs, active one bracketed and tinted in its
agent's accent, inactive one dim:

```
✻ sm · AI Sessions  [Claude 52]  Codex 18   · indexing 12/70…
```

- Tab counts are **live**: each shows the number of sessions that view would
  display right now — honoring the `e` empty toggle and any active filter or
  search. Browsing untouched, they are the full per-agent counts; mid-filter
  they double as a "the other side has N matches" signal.
- Status suffixes (`· indexing…`, `· N unindexed`, `· scanning…`) append
  after the tabs, unchanged.
- Clicking a tab activates that view (new mouse zone on the title row).

### Per-view state

Cursor position, scroll offset, and per-project fold state are kept **per
view** — two independent copies, swapped on toggle. The same project may be
folded in one view and open in the other. After a rescan, each view restores
its remembered selection when the session still exists, else falls back to
the first session (existing `selectSession` behavior).

### Theme follows the active view

The style set already carries both accents (`Accent`, `CodexAccent`). In a
single-agent view every agent-conditional color collapses to the active
agent's accent: focused pane border, selected title/meta, group-header
selection and counts, ✻ title mark, filter-prompt `>`, tmux `●` markers, and
the current-project label. The per-row `claude`/`codex` tag is **removed** —
inside a single-agent view it is pure noise; the tab bar and theme carry the
identity. (`projectMajorityAgent` and the per-row tag styles go away with
it.)

The preview pane keeps rendering whatever session is selected; its glyph
accent keys off that session's agent as today.

### Filter and search scope

`/` fuzzy filter and `s` full-text search evaluate against the active view's
sessions only. The index still covers everything (it is keyed by path,
agent-agnostic), and hits are partitioned by agent:

- Active view has hits → show them, as today.
- Active view has **zero** hits but the other view has some → the empty-state
  line says so explicitly instead of a bare "no matches":

  ```
  no matches · 3 hits in Codex — press a
  ```

  Switching views with the search live shows those hits.

### New, delete, tmux

- `n` starts a session with the **active view's agent** — the
  `dialogPickAgent` "[1] Claude [2] Codex" step disappears (its guard for a
  missing binary moves to the single-agent path). The directory picker and
  its candidates (all known dirs) are unchanged.
- `d` delete and `x` tmux-kill act on the selected session/project of the
  current view; mechanics unchanged.

### Availability and degradation

Tabs appear when **two or more providers are available** (their data dirs
exist), even if one currently has zero sessions — its view shows
"no sessions" and `n` can create the first one. With a single available
provider: no tabs, `a` is a no-op, and the UI is byte-for-byte today's
single-agent experience.

### Removals

The per-project agent subheader feature is subsumed by the views and is
deleted: `row.subheader`, `groupByAgent`, `ToggleAgentGroup`,
`projectHasBothAgents`, `agentTitle`, the subheader branches in
`refresh`/`View`/cursor movement, and their tests. The help bar keeps the
`a agent` entry (same key, new meaning).

## Implementation shape

All changes live in `internal/ui`; `internal/store` is untouched.

- **`listpane.go`** — `activeAgent store.Agent` field; row building filters
  by it; per-view `{cursor, lineOffset, folded}` swapped in a `SetAgent`
  method; per-agent visible counts exposed for the tabs; subheader machinery
  removed; empty-state string gains the cross-view hit hint.
- **`model.go`** — title bar renders tabs; `a` calls the view toggle;
  `startNew` uses the active agent; startup picks the default view;
  status-line/current-project label colors read the active accent.
- **`mouse.go`** — title-row tab hit zones; help-bar `a agent` unchanged.
- **`styles.go`** — active-accent helper (`AccentFor(activeAgent)`); drop the
  per-row tag styles.

## Testing

Unit tests (table style, matching existing suites):

- listpane: view filtering, per-view cursor/scroll/fold isolation, toggle
  round-trip preserves selection, live tab counts under filter/empty toggle,
  search-hit partition and empty-state hint.
- model: `a` toggles and re-applies filter/search; `n` skips the agent
  dialog and launches the active agent; single-provider degradation (no
  tabs, `a` no-op); default view when Claude is unavailable.
- mouse: clicking each tab switches views; existing zones unaffected.

`make test` and `make vet` green.

## Out of scope

- Remembering the last active view across runs (config or state file).
- A three-state toggle with a mixed "all" view.
- Per-view `--projects-dir`-style overrides for Codex.

## Rollout note (this machine)

The user runs a Homebrew-installed upstream `sm` 0.3.1 at
`/opt/homebrew/bin/sm`, which shadows `~/.local/bin`. After `make install`,
verify `which sm` resolves to the fork build (unlink or overwrite the brew
binary if not).
