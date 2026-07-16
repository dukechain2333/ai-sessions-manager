# TUI Settings Dialog — Design

**Date:** 2026-07-16
**Status:** Approved

## Problem

Today the only way to change sm's configuration is hand-editing
`~/.config/sm/config.json`. For part of the user base that is too much
friction. Add a settings dialog inside the TUI that can view and edit every
existing config key, then write the file back.

## Decisions (confirmed with user)

1. **Apply-on-restart.** Saving only writes `config.json`; the running TUI
   keeps its current state. After a successful save, show "Saved — restart
   sm to apply". No hot-reload of any setting.
2. **Full rewrite on save.** The file is re-serialized in the canonical
   shape of `config.DefaultFileJSON` (object form of `open_in`, all known
   keys present). User shorthand (`"open_in": "window"`) and omitted keys
   are normalized away. Unknown keys are dropped — the parser doesn't
   support any today, so real loss is zero.
3. **Centered modal form**, reusing the existing dialog mechanism, opened
   with `,` from the list view.

## Settings surface (all 6 leaf keys / 8 form rows)

| Row | Key | Kind | Values / validation |
|---|---|---|---|
| view | `view` | enum | `list` / `tabs` |
| open_in mode | `open_in.mode` | enum | `current` / `window` |
| iterm2 ssh | `open_in.iterm2.ssh` | text | free-form ("" = disabled) |
| tmux enabled | `tmux.enabled` | bool | toggle |
| claude light | `colors.claude.light` | hex text | `hexRE` (`^#[0-9a-fA-F]{6}$`) |
| claude dark | `colors.claude.dark` | hex text | `hexRE` |
| codex light | `colors.codex.light` | hex text | `hexRE` |
| codex dark | `colors.codex.dark` | hex text | `hexRE` |

Form initial values come from the resolved `config.Config` loaded at
startup (defaults already filled in).

## UI / interaction

```
┌─ Settings ──────────────────────┐
│ view          ◂ list ▸          │
│ open_in mode  ◂ current ▸       │
│ iterm2 ssh    generalserver     │
│ tmux enabled  [x]               │
│ claude light  #C15F3C █         │
│ claude dark   #D97757 █         │
│ codex light   #0A7C66 █         │
│ codex dark    #10A37F █         │
│                                 │
│ j/k move  enter edit  s save    │
│ esc close without saving        │
└─────────────────────────────────┘
```

- New `dialogSettings` value in `dialogKind`; opened by `,` when no other
  dialog is active and focus is not in a text input. Footer help gains
  `, settings`.
- Navigation: `j`/`k`/`↑`/`↓` move the cursor.
- Enum rows: `enter`/`←`/`→`/`h`/`l` cycle values.
- Bool row: `enter`/`space` toggles.
- Text rows: `enter` opens an inline `textinput` seeded with the current
  value; `enter` commits, `esc` abandons that row's edit (dialog stays
  open). Hex rows validate on commit with `hexRE`; invalid input is
  rejected — the row shows an inline error and stays in edit mode.
- Color rows render a live swatch (`█` colored with the row's committed
  value).
- While not editing a row: `s` saves, `esc`/`q` closes discarding changes.
- Save success → dialog closes, info dialog shows "Saved — restart sm to
  apply" (reuses the existing error-dialog rendering). Save failure (e.g.
  unwritable path) → error dialog with the underlying error; settings
  changes are kept in the form so the user can retry.

## Code changes

### `internal/config`

- `Save(path string, cfg Config) error` — marshal `cfg` to the canonical
  file shape (pretty-printed, `open_in` as object), `MkdirAll` the parent,
  write `0644`.
- Tests: round-trip `Load(Save(cfg)) == cfg` for non-default values;
  `Save(Default())` parses back to `Default()`; save creates parent dirs.

### `internal/ui`

- New files `settings.go` + `settings_test.go` (model.go is already
  1300+ lines; don't grow it further). Contains the form state
  (`settingsForm`: rows, cursor, editing flag, textinput, error text),
  its update function, and its view rendering.
- `Model` gains `configPath string` and an injected
  `saveConfig func(string, config.Config) error` (defaults to
  `config.Save`; stubbed in tests).
- `ui.New` signature gains the config path:
  `New(projectsDir, codexDir, configPath string, cfg config.Config)`.
- `cmd/sm/main.go` passes the resolved `path` (already computed at
  main.go:42) into `ui.New`.

### Docs

- README config section: mention pressing `,` inside sm as the alternative
  to hand-editing config.json.

## Error handling

- Unwritable config path → error dialog, form state preserved.
- Invalid hex on commit → inline row error, edit mode retained.
- No concurrency handling: last write wins over concurrent hand-edits
  (accepted consequence of full-rewrite).

## Testing

- config: round-trip, canonical-shape pin, dir creation (temp dirs).
- ui (table-driven, following existing `*_test.go` style):
  - `,` opens the dialog; `esc`/`q` closes without writing.
  - Cursor movement clamps at both ends.
  - Enum cycling and bool toggling update the form value.
  - Text edit commit updates the row; invalid hex is rejected with an
    inline error.
  - `s` calls the injected save with a `Config` matching the form, and
    the confirmation dialog appears.
  - Save error surfaces the error dialog.

## Out of scope

- Hot-applying settings to the running TUI.
- Preserving unknown JSON keys or user formatting.
- New config keys (this dialog only edits what exists today).
