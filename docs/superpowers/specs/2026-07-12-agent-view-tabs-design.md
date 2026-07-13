# Agent view modes: list ⇄ tabs (Claude / Codex) — Design

**Date:** 2026-07-12
**Status:** Approved v2 (v1 approved same day replaced the mixed list; v2 keeps
it: both modes coexist and the user picks)

## Problem

Since Codex support (v0.3.0) the list mixes both agents' sessions, telling
them apart by a per-row `claude`/`codex` tag and an opt-in per-project
`─ Claude ─ / ─ Codex ─` subheader toggle (`a`). Working in both agents, some
sessions views want full separation — but the mixed list (and its `a`
subgrouping) is also worth keeping. Users should choose.

## Goal

1. Two view modes:
   - **List mode** (today's behavior, the default) — one mixed list, per-row
     agent tags, `a` toggles per-project agent subheaders. Byte-for-byte
     unchanged.
   - **Tab mode** — the list shows **exactly one agent at a time**; the title
     bar grows `[Claude 52]  Codex 18` tabs and `a` (or clicking a tab)
     switches views.
2. Mode is the user's choice: `v` toggles it live; `config.json`'s
   `"view": "list" | "tabs"` sets the startup default (`"list"` built-in).
3. In tab mode, which view you're in is unmistakable: tabs plus the accent
   theme (focused border, selection, ✻ mark, filter prompt, tmux dots,
   project label) switching coral ⇄ teal.
4. Each view — including the mixed list — keeps its own cursor, scroll, and
   fold state across switches.
5. In tab mode, filter and full-text search operate on the current view only,
   with a cross-view hint when the other view has hits.
6. Machines with a single agent: list mode is unchanged; tab mode shows no
   tab bar and `a` is a no-op there.

## Design

### One mechanism: the mixed list is a third view

`listPane` gains an **active agent**: `""` (the zero value) means *all
agents* — the mixed list, exactly today's code paths — while `claude` /
`codex` filter row-building to that agent. Nothing is deleted: subheaders
(`groupByAgent`) and per-row tags simply never render in a single-agent view
(a filtered project can't have both agents; tags render only when the active
agent is `""`).

Per-view navigation state (`cursor`, `lineOffset`, `folded`) is parked in a
`map[store.Agent]viewState` keyed by `""`/`claude`/`codex` and swapped by
`SetAgent`, so mode/view switches lose nothing. A restored cursor is clamped
by `refresh()` if that view's rows changed while parked.

### Mode selection

- **`v` key** (list focus, like the other toggles; also a `v view` help-bar
  button): flips list ⇄ tab mode. Entering tab mode restores the last tab
  view (first entry: Claude, or Codex when Claude's projects dir is missing
  while a Codex provider registered); leaving parks it and returns to the
  mixed list.
- **`config.json`**: top-level `"view": "list" | "tabs"` sets the startup
  mode. Missing or unrecognized values fall back to `"list"` (the file's
  existing forgiving philosophy). The scaffolded default file gains
  `"view": "list"`.

### Tab mode

- `a` toggles Claude ⇄ Codex (strict two-state). It works in every
  list-focused mode including live filter/search (the query re-applies to the
  other view). While the filter input has focus, `a` types the letter.
- **Title bar** replaces the session count with two tabs, active one
  bracketed and tinted, inactive dim: `✻ sm · AI Sessions  [Claude 52]  Codex 18`.
  Counts are **live** (they honor the `e` empty toggle and any active
  filter/search — mid-filter they double as "the other side has N matches").
  Status suffixes (`· indexing…`, `· N unindexed`, `· scanning…`) append
  after the tabs. Clicking a tab activates it (new title-row mouse zone).
  With tabs, the `· N matched` search suffix is dropped — the tab counts
  carry it.
- **No per-row agent tags** — inside a single-agent view they are pure noise;
  the tab bar and theme carry the identity.
- **Theme follows the view**: focused pane border, selected title/meta,
  group-header selection/count, header tmux dot, ✻ mark, filter-prompt `>`,
  and the current-project label all use the active agent's accent. (In list
  mode all of these keep today's logic, including the majority-agent label
  and per-session border.)
- **`n` new session** launches the **active view's agent** directly — no
  pick-agent dialog. (List mode keeps the existing flow: direct with one
  provider, `dialogPickAgent` with two.)
- **Search scope**: `/` and `s` evaluate against the active view only. The
  index still covers everything; hits are partitioned by agent. Zero hits in
  the active view with hits in the other renders
  `no matches · 3 hits in Codex — press a` (singular `1 hit`) instead of the
  bare `no matches`. The mixed list keeps today's strings.
- **Single provider**: no tab bar (title falls back to the `N sessions`
  count), `a` is a no-op; `v` still toggles the mode (the only visible
  differences in tab mode are the hidden tags).

### List mode

Today's v0.3.1 behavior, untouched: mixed recency list, per-row tags, `a`
subheaders, majority-agent label/border accents, `n`'s provider-count-based
flow, plain `no matches` empty states, `N sessions` title (with search
`· N matched` suffix).

## Implementation shape

- **`internal/config`** — `Config.View string` (+ `"view"` in
  `DefaultFileJSON`, validated load, tests).
- **`internal/ui/listpane.go`** — `activeAgent`, `agentTotals` (per-agent
  visible counts for tabs/hint), `SetAgent` + `viewState` map, conditional
  tag/accent rendering, search partition, empty-state hint. `otherAgent`
  helper. Nothing removed.
- **`internal/ui/model.go`** — `tabsMode`/`tabView` fields, `v` toggle, `a`
  branches by mode, config wiring in `New`, `agentTabs`/`tabAt` (shared by
  View and mouse), title-bar tabs with single-provider/list fallback, chrome
  accents branch on `list.Agent() != ""`.
- **`internal/ui/mouse.go`** — `zoneTabs` (title row) routed through the same
  guarded switch as `a`; `v view` help-bar item.
- **`internal/ui/styles.go`** — `TitleMarkFor(agent)` replaces `TitleMark`
  (`AgentAccent("")` is the Claude accent, so the mixed list renders as
  today).
- `internal/store` is untouched.

## Testing

- config: default `"list"`, load `"tabs"`, unknown value falls back;
  `DefaultFileJSON` pin test covers the new key automatically.
- listpane: existing tests (mixed defaults) pass unchanged; new tests for
  agent-view filtering, live totals under filter, per-view state round-trips
  (including the mixed view), tags hidden and subheaders absent in agent
  views, search partition + empty-state hint.
- model: `v` round-trip (mode + state), `a` per mode, startup mode from
  config, title tabs vs fallback, chrome colors in an empty tab view, filter
  surviving switches, tab-mode `n`.
- mouse: `zoneTabs` geometry pinned to the rendered header, tab clicks
  (inactive switches, active no-op), list-mode/single-provider title clicks
  inert, `v view` help-bar button.

## Out of scope

- Persisting the last mode/view across runs beyond the config default.
- A per-project or per-view `--projects-dir` override.
- Removing any list-mode feature.

## Rollout note (this machine)

The user runs a Homebrew-installed upstream `sm` 0.3.1 at
`/opt/homebrew/bin/sm`, which shadows `~/.local/bin`. After `make install`,
verify `which sm` resolves to the fork build (unlink or overwrite the brew
binary if not). The user's own config should set `"view": "tabs"`.
