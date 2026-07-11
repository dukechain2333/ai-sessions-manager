# sm — Claude Code Visual Theme — Design

**Date:** 2026-07-11
**Status:** Approved (design conversation, 2026-07-11)

## Problem

`sm` ships a magenta/purple accent, `›`/`●`/`⚒` message prefixes, and a `🔍`
filter prompt. The user wants it to read like Claude Code's native TUI.

## Goal

Adopt Claude Code's visual language — coral/clay accent over layered grays,
its `>` / `⏺` / `⎿` glyph system, a `> ` input prompt, a `✻` title mark —
with **zero functional change**: every keybinding, mouse zone, layout
measurement, and piece of help/dialog text stays byte-identical.

## Visual spec

| Element | Today | Claude Code theme |
|---|---|---|
| Accent (selection, focused border, title mark, header count) | magenta/purple (`212`/`56`) | coral clay — dark `#D97757`, light `#C15F3C` (AdaptiveColor, both themes kept) |
| Text / dim layers | `252`/`241` etc. | layered grays: text dark `#DEDEDE` / light `#333333`; dim dark `#767676` / light `#8A8A8A` |
| Warn/error | red `203`/`160` | unchanged |
| Preview user prefix | `› ` | `> ` (bold, text color) |
| Preview assistant prefix | `● ` | `⏺ ` (text color) |
| Preview tool prefix | `⚒ ` | `⎿ ` (dim) |
| Filter prompt | `🔍 ` | `> ` (accent-colored) |
| Title bar | ` sm · AI Sessions ` | `✻ sm · AI Sessions` — `✻` in accent, name bold |
| Group headers | bold + underline | bold, **no underline**; `(n)` count in accent |
| Pane borders | rounded; dim/accent | rounded kept (Claude Code's input box is a rounded frame); unfocused = faint gray, focused = coral |
| Help bar / dialogs | dim gray | colors follow the new gray/accent palette; **text unchanged** |

## Hard invariants (tests already pin most of these)

- Help-bar text stays exactly `" ↵ resume  tab focus  n new  d delete  / filter  g group  space fold  e empty  r rescan  q quit"`.
- Dialog border glyphs stay `RoundedBorder` (the corner-scan test looks for `╭`).
- Hit-testing geometry is unchanged EXCEPT one deliberate 1-column tweak:
  the filter-prompt toggle zone narrows from `x <= 2` to `x <= 1`, because
  `> ` occupies columns 0-1 where `🔍 ` occupied 0-2 — the zone remains
  exactly "the prompt glyph". (The existing icon test clicks x=1 and the
  bar-body test clicks x=10; both stay valid.)
- `zoneAt`, pane widths, row heights: untouched.
- Placeholders `filter…` / `search…`: unchanged.

## Touch points

- `internal/ui/styles.go` — palette + the underline removal (whole-file restyle).
- `internal/ui/preview.go` — the three prefix strings.
- `internal/ui/model.go` — filter prompt string; title-bar rendering (`✻` mark).
- `internal/ui/mouse.go` — one comment (`🔍 icon` → `> prompt glyph`); no code.
- Docs: README Search section's "🔍 icon" wording → "the `>` prompt";
  Mouse section unaffected; the search spec's interaction-table row likewise
  reworded (docs-only historical note, not behavior).

## Testing

- Existing suite is the main gate: prefix-sensitive tests (if any assert
  `› `/`● `/`⚒ ` literally) are updated to the new glyphs; style-only changes
  are invisible under the test Ascii profile by design.
- New pins: preview render contains `> `/`⏺ `/`⎿ ` prefixes per kind; title
  line contains `✻ sm`; filter prompt renders `> `; group header no longer
  underlined is untestable under Ascii profile (style-only) — not asserted.
- Gate: `gofmt -l .` empty, `go vet ./...`, `go test -race ./...`; manual
  look in Ghostty (dark + light) by the human.

## Out of scope

- Welcome banner, spinner phrases, any text/copy changes beyond the title mark.
- Layout changes, borderless redesign.
- Upstream PR (stays on the local fork, branch `feat/search` continuation —
  cosmetics land after the search feature's final fixes).
