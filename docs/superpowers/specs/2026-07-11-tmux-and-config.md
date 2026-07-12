# tmux integration, configurable colors, and config.json — Design

**Date:** 2026-07-11
**Status:** Approved (design conversation, 2026-07-11)

## Problem

Two feature requests, plus the config surface they both need:

1. Resuming (and starting) an agent session should run inside a **tmux**
   session that survives detaching, so work keeps running in the background and
   can be re-attached. The panel should show which sessions have a live tmux and
   let the user kill it (all of a project's at once from the project header).
   tmux killed outside sm must be detected automatically.
2. Users should be able to **override the per-agent theme colors**.
3. Both are driven by a **config.json**, documented in the README.

## Scope decisions (locked during brainstorming)

- **Resume flow:** resume attaches into the tmux (sm suspends via the existing
  `ExecProcess` path); detaching (Ctrl-b d) returns to sm with the tmux still
  alive; re-resuming re-attaches the same tmux.
- **Default:** `tmux.enabled` defaults to **false**. When set true, sm checks
  that `tmux` is on `PATH`; if it is not, sm shows one startup error dialog and
  runs that session with integration off (inline resume).
- **Coverage:** tmux applies to **both resume and new** sessions. New-session
  tmux linking to a panel row is best-effort (adoption, below).

## Section 1 — Config file

### Location and precedence

- Default path: `$XDG_CONFIG_HOME/sm/config.json`, falling back to
  `~/.config/sm/config.json` when `XDG_CONFIG_HOME` is unset.
- Overridable with a new `--config <path>` CLI flag.
- Missing file → built-in defaults, no error.
- Malformed JSON → one dismissible startup error dialog, then built-in
  defaults for the whole config.
- Missing individual keys → that key's built-in default.

### Schema (every key optional)

```json
{
  "tmux": { "enabled": false },
  "colors": {
    "claude": { "light": "#C15F3C", "dark": "#D97757" },
    "codex":  { "light": "#0A7C66", "dark": "#10A37F" }
  }
}
```

- `tmux.enabled` (bool, default `false`) — enable tmux integration. When true
  and `tmux` is absent from `PATH`, sm shows a startup error
  ("tmux integration is enabled but tmux was not found on PATH") and treats
  integration as off for the run.
- `colors.claude`, `colors.codex` — each with optional `light` / `dark` hex
  strings (`#RRGGBB`). A present, valid value overrides the built-in accent for
  that agent+variant; an omitted or invalid value falls back to the built-in
  default for that field. Validation: `#` followed by exactly 6 hex digits.

### Ownership

A new `internal/config` package owns path resolution, loading, and validation.
It exposes a `Config` value with resolved fields and a load function returning
`(Config, error)` where the error is only for the malformed-JSON dialog (a
missing file returns defaults + nil error). `cmd/sm/main.go` loads it and passes
the resolved colors into `defaultStyles` and the tmux settings into `ui.New`.

## Section 2 — tmux lifecycle

### Naming

Each sm-managed tmux is named `sm-<agent>-<id8>`:

- `<agent>` is `claude` or `codex`.
- `<id8>` is the first 8 characters of the session UUID (lowercased). tmux
  mangles `.` in session names, so the full UUID is never embedded; 8 hex chars
  is enough to disambiguate in practice.

The name is a pure function of `(agent, id)`, so any listed session can be
checked for a live tmux with zero stored state.

### Live discovery (single source of truth)

sm never persists tmux state. It runs:

```
tmux list-sessions -F '#{session_name}'
```

filters names to the `sm-` prefix, and builds a set. A session's marker is on
iff `sm-<agent>-<id8>` is in that set. This makes **external kills
self-correcting**: a session killed with `tmux kill-session` outside sm simply
drops out of the next listing and its marker clears — there is no stale state to
reconcile. The set is refreshed:

- on a ~2s `tea.Tick` while integration is on,
- immediately after a resume/new attach returns (the existing `agentExitMsg`),
- immediately after a kill.

`tmux list-sessions` exits non-zero with "no server running" when no tmux
sessions exist; that is treated as an empty set, not an error.

### Resume

When integration is on, resume builds a tmux command instead of the bare agent
command:

```
tmux new-session -A -s sm-<agent>-<id8> -c <cwd> <resume-cmd…>
```

run through the existing `ExecProcess` path (sm suspends, user is attached).
`-A` attaches if the session exists, else creates it and runs the resume
command; re-resuming a running session re-attaches. When the agent process
exits, tmux closes the session (default, no `remain-on-exit`) and the marker
clears; when the user detaches, the agent keeps running, tmux persists, and the
marker stays on. Attach-return triggers the existing rescan.

When integration is off, resume runs the bare agent command exactly as today.

### New sessions

A new session has no UUID at launch, so it starts in a provisional tmux:

```
tmux new-session -s sm-<agent>-pending-<unixnano> -c <cwd> <new-cmd…>
```

with `tmux set-option -t <name> @sm_cwd <cwd>` and `@sm_agent <agent>` recorded
on the session so adoption can find it. (`-A` is irrelevant for a fresh name.)

### Adoption (best-effort new-session linking)

On each rescan, for every live provisional tmux (`sm-<agent>-pending-*`):

1. Read its `@sm_cwd` and `@sm_agent`.
2. Among scanned sessions with the same `CWD` and `Agent` that are **not**
   already backed by a live `sm-<agent>-<id8>` tmux, pick the one with the most
   recent `LastActivity`.
3. If found, `tmux rename-session -t <provisional> sm-<agent>-<id8>`.

After adoption the row shows the marker like any resumed session. Ambiguity
(two new sessions started in one directory before returning) resolves by
recency and is cosmetic; kill-all still catches both by project. An orphaned
provisional tmux (its session never appears, or sm restarted) is left running
and untracked; it is still killable by project once a session in that cwd is
adopted, and is documented as a known edge in the README.

### tmux boundary (testability)

All tmux calls go through a small injectable interface on the model
(default implementation shells out; tests inject a fake), covering:

- `List() (map[string]bool, error)` — the `sm-`-prefixed live set.
- `Kill(name string) error`.
- `Rename(from, to string) error`.
- `Option(name, key string) (string, error)` — read `@sm_cwd` / `@sm_agent`.
- command builders for resume/new that produce the `tmux …` argv (so the
  `ExecProcess` launch itself stays the existing injected `runCmd`).

This mirrors the existing injected `runCmd`/`trashFn`/`now` seams; no real tmux
runs in unit tests.

## Section 3 — UI

### Live-tmux marker

- A session row with a live tmux gets a trailing `●` in that agent's accent
  color (config-overridable coral/teal).
- A project header shows `●` when **any** session under it has a live tmux
  (logical OR of its children), visible even when the project is folded.
- Markers are derived from the discovery set each render; no stored state.

### Kill key `x`

- On a session row: `tmux kill-session -t sm-<agent>-<id8>`; no-op if not live.
  Immediate (no confirm) — re-resuming is cheap.
- On a project header: kill every live tmux belonging to that project's
  sessions (iterate the header's child rows; include provisional tmux whose
  `@sm_cwd` maps to this project). Guarded by a confirm dialog
  ("Kill N tmux in `<project>`? y/n") because it is plural and destructive.
- After any kill, refresh the discovery set immediately.

### Help row

- Add `x kill` to the help bar between `d delete` and `/ filter`; the existing
  help-bar hit-testing makes it mouse-clickable automatically, and the bar keeps
  truncating on narrow terminals.
- When integration is off, the `x kill` item is hidden and `x` is inert, so the
  bar and behavior are unchanged for non-tmux users.

### Colors

The config's per-agent light/dark overrides are applied inside `defaultStyles`,
so every existing consumer of the accents — pane borders, the bottom-left
project label, agent tags, and the new markers — picks up custom colors with no
per-call change.

## Section 4 — Testing

Unit (with the injected tmux fake and config fixtures):

- Name derivation: `tmuxName(AgentClaude, "abcd1234-…") == "sm-claude-abcd1234"`.
- Marker on/off from a given discovery set; header marker == OR of children.
- `x` on a session issues one `Kill` of the right name; no-op when not live.
- `x` on a header opens the confirm, and confirming issues `Kill` for each live
  child tmux.
- Adoption: renames the provisional tmux to the newest matching session's name;
  skips sessions already backed by a live tmux; no-op when nothing matches.
- Config: defaults (missing file), partial override, malformed JSON → error +
  defaults, invalid hex → per-field fallback, `--config` path override.
- tmux-missing-at-startup: integration enabled + `tmux` absent → startup error
  and integration treated as off (resume builds the bare agent command).

Manual/e2e (in the plan, like prior features): with real tmux — resume a
session, confirm attach; detach and confirm the marker turns on; kill with `x`
and confirm it clears; kill an external tmux and confirm the marker
self-corrects on the next tick.

## Non-goals (YAGNI)

- tmux window/pane layout control or multi-pane arrangements.
- Attaching to arbitrary pre-existing (non-sm) tmux sessions.
- Persisting any tmux state to disk.
- Per-session color overrides (only per-agent) or configurable keybindings.
- Reconciling provisional tmux across an sm restart (orphans are left alone).
