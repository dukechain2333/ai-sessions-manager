# Auto-create a default config.json on first run — Design

**Date:** 2026-07-11
**Status:** Approved (design conversation, 2026-07-11)

## Problem

`sm` reads `config.json` if present but never creates one, so a new user has
nothing to find or edit — they must hand-write the file to discover the
available options. Creating it at install time is not viable: `sm` ships via
Homebrew, apt/`.deb`/`.rpm`, the universal `install.sh`, and manual download;
package managers run as root in a sandbox and must not write into an end
user's `$HOME` (there may be no single "the user" at install time). The
binary is the only place that reliably runs as the user.

## Goal

On first run, if no config file exists at the default location, `sm` writes a
default `config.json` there (defaults spelled out), then continues normally.
Later runs leave the file untouched. The written defaults are
behavior-identical to having no file (tmux off, default colors), so this only
adds a discoverable, editable file — it changes no behavior.

## Design

### `internal/config`

- **`DefaultFileJSON`** — a package constant holding the pretty-printed default
  config text:
  ```json
  {
    "tmux": { "enabled": false },
    "colors": {
      "claude": { "light": "#C15F3C", "dark": "#D97757" },
      "codex":  { "light": "#0A7C66", "dark": "#10A37F" }
    }
  }
  ```
  A unit test asserts `Load`-ing this text returns exactly `Default()`, so the
  template cannot drift from the real defaults.

- **`EnsureDefault(path string) (created bool, err error)`** — if `path` exists,
  no-op returning `(false, nil)`. Otherwise `os.MkdirAll(dir, 0o755)` then
  `os.WriteFile(path, []byte(DefaultFileJSON), 0o644)`, returning `(true, nil)`.
  It NEVER overwrites an existing file, so a user's edited config is always
  safe. A write/mkdir failure returns `(false, err)`.

### Wiring (`cmd/sm/main.go`)

After resolving the path (`config.Path(*configPath)`) and before
`config.Load`:

- Scaffold only when using the **default** location — i.e. when the
  `--config` flag was not given (`*configPath == ""`). An explicit `--config`
  path is the user's to manage; auto-creating it would mask typos.
- Call `config.EnsureDefault(path)`. On error, warn to stderr
  (`sm: config: <err>`) and continue — non-fatal. `config.Load` then returns
  built-in defaults for the missing file, so `sm` still runs.
- The `created` return is not surfaced to the user (silent creation, per the
  approved design); it exists for testability.

## Testing

- `Load(DefaultFileJSON written to a temp file) == Default()` — drift guard.
- `EnsureDefault` on a missing path creates the file (with parent dirs) and
  returns `created == true`; the file's contents parse back to `Default()`.
- `EnsureDefault` on an existing path is a no-op: returns `created == false`
  and leaves the file's original (e.g. user-edited) contents byte-for-byte
  unchanged.
- Manual: with a temp `XDG_CONFIG_HOME`, run `sm` once → the config appears;
  edit it, run again → edits preserved. (Do not touch the real
  `~/.config/sm/config.json`.)

## Out of scope (YAGNI)

- No `sm --init` command.
- No overwriting, merging, or migrating an existing config.
- No install-script / packaging changes.
- No scaffolding at a custom `--config` path.
- No surfacing "created config at …" to the user.
