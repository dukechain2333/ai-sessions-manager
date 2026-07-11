# sm — Mouse Support — Design

**Date:** 2026-07-10
**Status:** Approved (design conversation, 2026-07-10)

## Problem

`sm` is keyboard-only. Every action — selecting a session, folding a project,
resuming, even quitting — requires knowing the key. Users coming from GUI tools
(or from Claude Code's own clickable UI) instinctively click list rows, the
help bar, and dialog buttons, and nothing happens.

## Goal

Make every interactive element mouse-operable without changing any existing
keyboard behavior, with zero new dependencies:

1. Click to select, double-click to resume, click headers to fold.
2. Wheel scrolling over the list and the transcript preview.
3. Clickable help-bar "buttons" and dialog buttons.
4. Keyboard-only terminals (and terminals with no mouse reporting) behave
   exactly as today.

## Interaction spec

| Zone | Single left-click | Double-click (≤400 ms) | Wheel |
|---|---|---|---|
| Session row (any of its 3 lines) | select + reload preview | resume (same path as `enter`) | move selection ±1 row (same as `j`/`k`) |
| Project header row | toggle fold (same as `enter` on header) | — (second click folds again) | move selection |
| Preview pane | focus preview (same as `tab`) | — | scroll transcript; no focus needed |
| Filter bar row | focus filter (same as `/`) | — | — |
| Help bar segment | same as pressing that key from the list (the click first returns focus to the list, so the labeled action always fires) | — | — |
| Delete dialog | `[y] confirm` / `[n] cancel` buttons; click outside cancels | — | — |
| Dir-picker dialog | row click selects; input row is already focused; click outside = `esc` | row double-click confirms (same as `enter`) | moves `dirCursor` |
| Error dialog | any click dismisses ("press any key") | — | — |
| Borders, blank areas, header/count row | no-op | — | — |

Double-click = two left presses on the same row (same zone) within 400 ms.
Wheel-as-selection (not free viewport scrolling) keeps the cursor, preview,
and scroll window in the single existing `cursor`+`ensureVisible` model — no
new "cursor off-screen" state.

While `claude` runs via `tea.ExecProcess`, Bubble Tea releases the terminal
(and mouse reporting), so claude's own mouse behavior is untouched.

Trade-off: with mouse reporting on, selecting text in the `sm` screen needs
Shift+drag (standard for mouse-enabled TUIs). Documented in the README.

## Architecture

No new dependencies. Bubble Tea v1's `tea.MouseMsg` (Action/Button API) does
everything needed.

- `cmd/sm/main.go` — add `tea.WithMouseCellMotion()` to the program options.
- `internal/ui/model.go` —
  - route `case tea.MouseMsg` to `handleMouse`;
  - extract the list/preview width arithmetic out of `layout()` into
    `paneWidths()` so rendering and hit-testing share one source of truth;
  - replace the hardcoded help string with a segment table
    (`[]helpItem{label, key}`); `View` renders by joining segments, and
    hit-testing walks the same segments' rendered widths. A clicked segment
    synthesizes the equivalent `tea.KeyMsg` and feeds it through `handleKey`,
    so a button can never drift from its key.
- `internal/ui/mouse.go` (new) — zone resolution + `handleMouse` + the
  double-click tracker (`lastClick{zone, row, at}`); wall clock injected as
  `now func() time.Time` on `Model` (default `time.Now`), following the
  existing `trashFn`/`runClaude` injection pattern.
- `internal/ui/listpane.go` — two small additions reusing `layout()`:
  `RowAtLine(contentLine int) (rowIdx int, ok bool)` and `SetCursor(rowIdx)`.
- Dialogs — the box is centered by `lipgloss.Place`; hit-testing recomputes
  the box origin with the same rounding, and inner button/row coordinates via
  the `DialogBox` style's frame sizes. Unit tests pin the rounding so a
  lipgloss behavior change fails loudly instead of mis-clicking.

### Geometry (mirrors `layout()`)

Header `y=0`, filter bar `y=1`, body top border `y=2`, pane content rows
`y ∈ [3, 2+bodyH]`, help bar = last row. List content `x ∈ [1, listW-2]`;
preview content `x ∈ [listW+1, listW+previewW]`. Narrow mode (`width < 80`):
no preview pane, list spans the width. List content line → row via
`lineOffset` + `RowAtLine`.

## Edge cases

- Filter active (flat, relevance-ordered rows): `RowAtLine` works unchanged —
  rows are rebuilt flat by `refresh()`.
- Folded projects, scrolled lists: `layout()` already accounts for both.
- Empty list / click below the last row: `RowAtLine` returns `ok=false`, no-op.
- Motion/release/right/middle events: ignored; only left press and wheel act.
- Terminals without mouse reporting: no `MouseMsg`s arrive; nothing changes.

## Testing

Table-driven unit tests in the existing style:

- `listpane`: `RowAtLine` across grouped / flat / folded / scrolled cases.
- `mouse_test.go`: zone resolution (incl. narrow mode and borders), click
  selects + reloads preview, header click folds, double-click resumes (fake
  `runClaude`, injected clock — inside vs. outside the 400 ms window),
  wheel over list and preview, filter-bar click focuses, help-bar segment
  click triggers its action, delete-dialog buttons, dir-picker click and
  double-click, click-outside dismissal.
- `make test`, `gofmt`, `go vet` per CI. Manual verification in a real
  terminal (Ghostty) before release.

## Out of scope

- Drag-to-scroll, drag text selection inside panes, hover highlights.
- Right/middle-click menus.
- Bubble Tea v2 migration.

## README

Usage section gains a **Mouse** subsection: the click/double-click/wheel
table above in short form, plus the Shift+drag-to-select-text note.
