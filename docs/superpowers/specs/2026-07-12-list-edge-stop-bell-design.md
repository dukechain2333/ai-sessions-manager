# List keyboard navigation: edge-stop with a bell — Design

**Date:** 2026-07-12
**Status:** Approved (design conversation, 2026-07-12)

## Problem

In the session list, pressing `k`/`↑` while the cursor is on the top row jumps
focus into the filter bar (`internal/ui/model.go`, the `cursor == 0` branch of
the `k`/up handler). Walking off the top of the list to reach the filter is
unwanted: the user wants the cursor to stay on the list and only reach the
filter/search bar deliberately. The bottom edge already clamps but gives no
feedback.

## Goal

1. `k`/`↑` at the topmost row keeps the cursor there and rings the terminal
   bell (a "little noise") instead of entering the filter bar.
2. `j`/`↓` at the bottommost row does the same: stays put and rings the bell.
3. The filter/search bar is reachable only via `/` (fuzzy filter), `s`
   (full-text search), or a mouse click — never by keyboard navigation off the
   top of the list.
4. No new keybindings; `/` and `s` are unchanged.

## Design

### Navigation (`internal/ui/model.go`, `internal/ui/listpane.go`)

- **Remove** the `if m.list.cursor == 0 { focus = focusFilter … }` branch from
  the `k`/up handler.
- `listPane.MoveCursor(delta int)` becomes `MoveCursor(delta int) (moved bool)`
  — it already clamps at the edges and skips inert subheaders; it now returns
  whether the cursor position actually changed. `false` means an edge was hit
  (top when going up, bottom when going down) or the list is empty.
- The `j`/down and `k`/up handlers:
  - if `MoveCursor` returned `true` → `return m, m.loadTranscriptCmd()` (as
    today).
  - if it returned `false` (edge) → `return m, m.bell` (ring, cursor unchanged,
    focus stays `focusList`).

### The bell (`internal/ui/model.go`)

- A model field `bell tea.Cmd` (injectable for tests), defaulting to a command
  that writes the BEL byte (`"\a"`) to **stderr**:
  ```go
  func ringBell() tea.Msg {
      fmt.Fprint(os.Stderr, "\a")
      return nil
  }
  ```
  Writing to stderr (not stdout) keeps it clear of Bubble Tea's alt-screen
  frame on stdout, so the frame is never corrupted.
- The terminal decides how BEL renders — audible beep, visual flash, or
  silent — per the user's terminal bell setting. No app-side configuration.
- `New` sets `bell: ringBell`. Tests override it with a spy to assert it fired.

### Scope

- Keyboard list navigation only: `j`, `k`, `↑`, `↓`. Applies in every view
  (mixed list, single-agent tab views, and full-text search results) because
  they all route through the same `MoveCursor`.
- Mouse-wheel scrolling past an edge stays silent (unchanged) — this is
  specifically about keyboard navigation.
- Leaving the filter bar (down / enter / esc / click-away) is unchanged. Only
  the list→filter keyboard entry path is removed.

## Testing

- `listpane_test`: `MoveCursor` returns `false` at the top (up) and bottom
  (down) and `true` for an interior move; the cursor index is unchanged on the
  `false` cases.
- `model_test`: `k` at the top row and `j` at the bottom row keep
  `focus == focusList`, leave the selected session unchanged, and return the
  bell command (a spy `bell` fires when the returned cmd is run). An interior
  move returns the transcript-load command, not the bell, and does not fire the
  spy.
- Update the existing test that asserts "`k` at row 0 enters the bar"
  (`internal/ui/search_test.go`) to expect stay-put + bell + `focusList`.
- `/` and `s` still focus the filter/search bar (regression: unchanged).

## Docs

- README key table: drop the "↑ at the top enters the search bar, ↓ in the bar
  returns" clause from the `↑/↓ j/k` row; note that the filter/search bar is
  reached with `/`, `s`, or a mouse click.

## Out of scope (YAGNI)

- No configurable bell (on/off, volume, visual vs audible) — defer to the
  terminal.
- No bell on mouse-wheel edge scrolls.
- No new `f` keybinding (confirmed: keep `/` and `s`).
- No change to how the filter bar is exited.
