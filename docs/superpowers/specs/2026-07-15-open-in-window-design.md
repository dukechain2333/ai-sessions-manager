# Design: `open_in` — launch sessions in the current terminal or a new tmux window

**Date:** 2026-07-15
**Status:** Approved

## Problem

Resuming (`enter`) or starting (`n`) a session always suspends sm and runs the
agent in the current terminal. Users — especially those working over SSH —
want the option to open the agent in a *new terminal window* instead, so sm
stays visible and multiple sessions can run side by side. Over SSH, a "new
window" must live inside the same SSH connection; spawning a local OS terminal
from the remote host is not portably possible.

## Decision summary

- **Mechanism (local and SSH alike): a new tmux window** in the tmux session
  the user is already attached to. tmux runs inside the SSH connection, so the
  new window is naturally "on the SSH". Local users get the same behavior; no
  OS-terminal spawning, no platform branches. Requires sm itself to be running
  inside tmux.
- **Scope:** the setting governs both resume (`enter`) and new session (`n`).
- **Control:** config only — no runtime override key.
- **Orthogonal to `tmux.enabled`:** `open_in` decides *where* the agent opens;
  `tmux.enabled` decides whether sm *tracks* it (`sm-` naming, ● markers, `x`
  kill). All four combinations are legal.
- **Tracking extends to window level** so window-mode launches keep the ●/`x`
  experience when tmux integration is on.

## 1. Config

New top-level key, sibling of `view`:

```json
{
  "view": "list",
  "open_in": "current",
  "tmux": { "enabled": false },
  "colors": { "...": "..." }
}
```

- `"current"` (default, backwards compatible): today's behavior — sm suspends
  via `tea.ExecProcess` and the agent runs in the current terminal.
- `"window"`: the agent runs in a new tmux window of the user's current tmux
  session; sm does not suspend and stays in its own window.
- Any other value silently falls back to `"current"` (same validation pattern
  as `view`).
- `Config` gains an `OpenIn string` field; `Default()` returns `"current"`;
  `DefaultFileJSON` adds the key (pinned by the existing default-parses-to-
  Default test).

## 2. Launch matrix

Both `enter` (resume) and `n` (new session) go through the same launch path
(`runAgentCmd`), so one matrix covers both:

| | `tmux.enabled: false` | `tmux.enabled: true` |
|---|---|---|
| `open_in: current` | (today) ExecProcess runs the agent directly | (today) ExecProcess runs `tmux new-session -A -s sm-…` |
| `open_in: window` | `tmux new-window -c <cwd> <agent…>` — plain unnamed window, untracked | `tmux new-window -n sm-<agent>-<id8> -c <cwd> <agent…>` — window is tracked |

Window-mode preconditions, checked at launch time; failure shows the existing
error dialog style:

1. `tmux` on `PATH` (existing check, reused).
2. `$TMUX` set — sm must itself be running inside tmux. Error text:
   `open_in "window" requires running sm inside tmux`.

Window mode must NOT use `tea.ExecProcess` (that suspends the TUI). It uses a
non-suspending runner: plain `exec.Command` — `tmux new-window` returns
immediately and switches focus to the new window (tmux default). After launch,
trigger a tmux re-list so the ● marker appears promptly.

For new sessions in window mode with tracking on, the window is created as
`sm-<agent>-pending-<nonce>` (same provisional scheme as sessions).
`new-window -n` disables tmux's automatic-rename for that window, so names
stay stable until adoption renames them.

## 3. Window-level tracking (only when `tmux.enabled: true`)

Today's tracking is pure name discovery over a `map[string]bool`; extending
the discovered name space to windows leaves the upper layers almost untouched.

- **Discovery:** `Runner.List()` returns the union of `sm-`-prefixed names
  from `tmux list-sessions` and `tmux list-windows -a`. Marker rendering and
  project-header aggregation are unchanged (they only consume the set).
- **Operations:** `Kill`, `Rename`, and `Path` resolve a name first as a
  session, then as a window (`kill-window` / `rename-window` / window pane's
  `pane_current_path`). Window targets use tmux window ids (`%N`) resolved
  from `list-windows -a`, so duplicate window names across tmux sessions
  cannot mis-target.
- **Adoption:** `adoptPending` matches by name and is form-agnostic; pending
  *windows* participate in the same flow, with the rename going through
  `rename-window`.
- **`enter` on a session with a live tracked tmux:** jump to it regardless of
  `open_in` — session form attaches via ExecProcess (today's behavior); window
  form switches via `select-window`. If sm is itself running *outside* tmux
  when the live tmux is window-form, `select-window` alone would change
  nothing visible — instead ExecProcess-attach to the owning tmux session with
  that window selected (`tmux attach -t <session> \; select-window -t %N`).
  `open_in` only governs the form of *new* launches, which also makes a
  same-id dual-form conflict impossible.
- **`x`:** window form kills via `kill-window`; the project-header bulk kill
  covers both forms (it iterates the same name set).

## 4. Errors and edge cases

- tmux missing from `PATH`, or `$TMUX` unset, with `open_in: window` → error
  dialog (section 2).
- User kills a window outside sm → it vanishes from the next `List()` poll,
  identically to the existing session-form behavior. No new logic.
- `open_in: window` + `tmux.enabled: false` → the window carries no `sm-`
  name and is deliberately untracked. This is the defined behavior of that
  combination, not a bug.
- Malformed `open_in` value → default `"current"`.

## 5. Testing

- **config:** parse `open_in`; invalid value falls back; `DefaultFileJSON` ↔
  `Default()` pin test updated.
- **tmux package:** argv builders for window creation (resume and pending
  forms); fake `Runner` grows window operations; window-id resolution.
- **ui/model:** launch command assertions for all four matrix cells; missing
  `$TMUX` error path; `enter` jumps to a live window (`select-window`); `x`
  uses `kill-window` for window form; adoption of a pending window.
- **README:** update the Configuration and tmux integration sections.
