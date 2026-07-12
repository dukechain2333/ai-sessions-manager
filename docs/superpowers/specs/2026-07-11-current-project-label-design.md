# Current-project label on the instruction line — Design

**Date:** 2026-07-11
**Status:** Approved (design conversation, 2026-07-11)

## Problem

When scrolling through a project with many sessions, the group header scrolls
off and the user forgets which project the selection is in.

## Goal

Show the **selected session's project** at the far left of the bottom
instruction (help) row, always visible, updating as the cursor moves.

## Design

The bottom row of the UI (currently just the help/key-hint line) gains a
left segment:

```
 ▸ HyperSAGNN_Interaction   ↵ resume  tab focus  n new  d delete  / filter  q quit
```

- **Label:** `▸ <project>` where `<project>` is `Selected().Project()` of the
  cursor's session, rendered in the accent group-header style. When no session
  is selected (cursor on a header/subheader, or empty list), the label is
  empty (the help line renders as it does today).
- **Composition:** the bottom row is `labelSegment + helpLine()`, rendered and
  clamped to the terminal width exactly as the help line is today
  (`MaxWidth(m.width)`), so on a narrow terminal the key hints truncate on the
  right first — unchanged behavior.
- Shows in **all modes** (grouped, flat, search) — it simply names the
  selected session's project.

## Critical: keep the mouse help-bar hit-testing consistent

The mouse handler makes the help-bar key hints clickable by computing each
button's x-range from the left, starting at a fixed offset. Prefixing a
variable-width project label shifts every button right by the label's rendered
width, so the click math MUST offset by that same width or clicks will
mis-map (this is the coordinate-drift class of bug that prior reviews caught
when a help item was inserted).

Resolution: a single shared helper produces the label segment, and both the
renderer and the mouse hit-tester use it:

- `func (m Model) bottomLabelSegment() string` — returns the styled
  `" ▸ <project>  "` prefix (with its trailing spaces) or `""` when there is
  no selected session.
- `View()` renders `bottomLabelSegment() + helpLine()` (clamped to width).
- The mouse help-bar hit-test adds `lipgloss.Width(m.bottomLabelSegment())` to
  its base x offset for every button. When the segment is empty (width 0) the
  offset is unchanged, so the Claude-only/no-selection path behaves exactly as
  before.

Both call the same function on the same `Model` snapshot, so they cannot drift.

## Architecture / scope

Entirely in `internal/ui/model.go` (View + the `bottomLabelSegment` helper) and
`internal/ui/mouse.go` (the help-bar x offset). No list-pane, scroll-math, or
layout-height change: the bottom row is still one line.

## Testing

- **Display** (`model_test.go` view test): the rendered bottom row contains
  `▸ <project>` of the selected session; it changes when the cursor moves to a
  session in a different project; it is absent (help line only) when no session
  is selected; the row stays within the terminal width on a narrow size.
- **Mouse consistency** (`mouse_test.go`): with a selected session (non-empty
  label), a click on a help-bar key hint still triggers that hint's action —
  i.e. the button x-ranges are offset by the label width. A regression test
  that clicks, say, `q quit` and asserts it quits, computed against the
  label-shifted position.

## Out of scope (YAGNI)

Sticky top header, per-mode suppression, showing the agent alongside the
project, configurable placement.
