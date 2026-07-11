# sm — Search-Bar Keyboard Access — Design

**Date:** 2026-07-11
**Status:** Approved (design conversation, 2026-07-11)

## Problem

The filter/search bar is reachable by mouse and by `/`, but `/` lands on the
title-filter layer (full-text needs an extra Tab), and there is no arrow-key
path between the list and the bar.

## Behavior

| Key | Where | Effect |
|---|---|---|
| `s` | list focus | focus the bar AND ensure the full-text layer is active (`search…`). If the layer is already full-text, just focus — no flip-back. `/` keeps meaning "focus on the title-filter layer" unchanged. |
| `↑` / `k` | list focus, cursor already on the FIRST row (row 0 — header or session) | focus the bar (current layer kept, text kept). Anywhere else: normal cursor move, unchanged. |
| `↓` | bar focused | leave the bar back to the list, keeping the query and its results — same preservation semantics as Enter. `↑` inside the bar stays a no-op. |

- `s` respects the existing `indexErr` guard (error dialog, same as Tab).
- Wheel-up at the top of the list does NOT enter the bar (scrolling is not a
  focus intent).
- Help bar is NOT extended (its text is byte-pinned and its mouse x-ranges
  are load-bearing; `N` set the precedent of README-only documentation).
  README documents `s`, `↑`-into-bar, and `↓`-back in the key table and the
  Search section.

## Touch points

`internal/ui/model.go` only: list-focus `case "s"` (new), `case "k", "up"`
top-row branch, focusFilter `case tea.KeyDown`. Tests in
`internal/ui/search_test.go`. README key table + Search section.

## Out of scope

Help-bar button for `s`; changing `/` semantics; wheel focus transfer.
