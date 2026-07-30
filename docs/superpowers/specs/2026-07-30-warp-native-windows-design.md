# Warp native windows — third backend design

Date: 2026-07-30
Status: approved pending user review

## Goal

`open_in: "window"` opens native Warp windows, on par with the existing
iTerm2 and Ghostty mechanisms: locally when sm runs inside Warp, and over
SSH via the existing `sm ssh` bridge. Today Warp is the only supported-
terminal family that degrades to tmux windows — which is also where Warp's
own UX is weakest (blocks and the input editor die inside tmux).

## Validated mechanism (spike, 2026-07-30)

Warp has no AppleScript dictionary, no local-window CLI (`oz` is cloud-agent
only), and ignores iTerm2's OSC 1337 `Custom=id` sequences. The only
programmatic "open a window and run a command" path is a **Tab Config**
file plus the `warp://` URI scheme. Spike-verified on Warp
`v0.2026.07.22.09.01.stable_01` (macOS):

- A TOML file in `~/.warp/tab_configs/` with a single `[[panes]]` entry and
  `commands = ["<one shell line>"]` (plain strings, run sequentially).
- `open "warp://tab_config/<filename>"` opens a **new tab in the frontmost
  Warp window** and runs the commands (`?new_window=true` opens a separate
  window instead — both forms spike-verified, ≈1s); the URI matches the
  **filename** (extension optional, case-insensitive), not the `name` key.
- Hot discovery works: the file — and the `tab_configs` dir itself — can be
  created moments before the URI is opened; no Warp restart.
- The file is **re-read on every URI open**, so one fixed file overwritten
  per launch is sound. End-to-end latency measured ≈1s.

## Design: a third backend, strictly mirroring Ghostty

### `internal/warp` (new package, mirrors `internal/ghostty`)

- `New() (*Opener, error)` — usable only when the terminal is Warp:
  `TERM_PROGRAM == "WarpTerminal"`, with `WARP_TERMINAL_SESSION_UUID` as
  the inside-tmux fallback (tmux rewrites `TERM_PROGRAM` but inherits
  `WARP_*`; the Ghostty analog is `GHOSTTY_RESOURCES_DIR`). macOS and
  Linux; Linux additionally needs a URI opener (`xdg-open`) on PATH.
- `Open(key, line string) error` — satisfies the same shape as
  `ghostty.Opener.Open` (usable as a `bridge.Handler`):
  1. render a minimal Tab Config: `name = "sm"`, one `terminal` pane,
     `commands = [<line>]`; `line` arrives fully quoted from `bridge.Line`,
     so the only encoding concern is TOML basic-string escaping (`\` and
     `"`), implemented and unit-tested;
  2. write it to `<tab_configs dir>/sm.toml` (temp file + rename in the
     same dir), creating the dir if missing; per-OS dir: macOS
     `~/.warp/tab_configs`, Linux
     `${XDG_DATA_HOME:-~/.local/share}/warp-terminal/tab_configs`;
  3. run `open`/`xdg-open` on `warp://tab_config/sm` with the same
     hang-guarded runner idiom as ghostty (`run` injected for tests) — a
     new TAB in the frontmost Warp window, per the revised decision below.

  Implementation note: the spike's TOML carried a `directory` key; a pane
  without one is untested. Verify during implementation; if Warp rejects a
  directory-less pane, emit `directory = "~"` (the `cd` in `line` still
  decides the real working dir).
- No window handle comes back, so no refocus dedupe (`key` is accepted and
  ignored) — the Ghostty-on-Linux precedent; duplicate tracked launches
  still collapse via `tmux new-session -A` on the target machine.
- Fixed-filename overwrite has a theoretical two-keypress race (second
  write before Warp reads the first). Launches are single-keypress
  interactive events and the read happens within ~1s; accepted, documented.

### Wiring (`internal/ui/model.go`, mirrors the ghostty entries)

- `warpEnv` package-level detection var (test-overridable, like
  `ghosttyEnv` at `model.go:403`).
- `warpWindows()` = `openIn == "window" && !overSSH() && warpEnv()`.
- `nativeWindows()` (`model.go:313`) grows the fourth disjunct;
  `openWindowCmd` (`model.go:316`) grows one case: bridge → ghostty →
  **warp** → iTerm2 (the env predicates are mutually exclusive, order is
  cosmetic).
- `warpOpen` injected runner + `localWarp` `sync.OnceValues`, mirroring
  `ghosttyOpen`/`localGhostty` (`model.go:336-353`). Local launches render
  through `bridge.Line(l, "", nil)` exactly as ghostty does — no new
  command-rendering code.
- Inside the ui package the self-wrap/downgrade exemptions follow from
  joining the `nativeWindows()` union. **`cmd/sm/main.go` is separate**: its
  wrap decision re-derives the probes inline by design ("The probes mirror
  ui's exactly … so the wrap decision and the in-app launcher choice can
  never disagree", `main.go:66-71`). The `nativeWin` expression grows a
  fourth disjunct with the same warp probe (Warp env marker, `!sshEnv`,
  Linux `xdg-open`-on-PATH requirement), preserving that invariant — without
  it a local Warp user in window mode self-wraps into tmux and never
  reaches the backend.

### `sm ssh` helper (`internal/bridge/sshmain.go`)

Replace the hardcoded `ghostty.New()` (`sshmain.go:42`) with opener
selection: try `ghostty.New()`, then `warp.New()`; both failing degrades to
plain ssh with the existing message. The "window bridge ready" / usage
wording changes from naming Ghostty to naming the detected terminal. The
remote side needs nothing: any bridge-capable sm (≥0.5.0-beta) works
unchanged, since the socket protocol and validation are terminal-agnostic.

## Explicit non-goals (decided)

1. **No formal `Backend` interface extraction.** Upstream's idiom is
   function vars + a small switch; at 3-4 backends that reads fine, and
   structural divergence from upstream hurts PR mergeability.
2. **No move/rename of `iterm2.Launch`.** The bridge's reuse of the iTerm2
   payload shape is deliberate and documented upstream; renaming is churn.
3. **No config schema or settings-dialog changes.** Local Warp is
   env-detected; `sm ssh` takes its destination from the command line.
   Zero new keys.
4. **Tabs, not windows** (REVISED 2026-07-30 during live QA — user
   decision; supersedes the original "windows only" ruling). Warp launches
   open a new tab in the frontmost Warp window (`warp://tab_config/sm`,
   no `?new_window=true`) — the Warp-native idiom. Applies to both local
   launches and the `sm ssh` helper. No config knob for windows-vs-tabs;
   add one only if asked.
5. **No refocus dedupe** (no handle available) and **no reliance on
   undocumented surfaces** (`WARP_FOCUS_URL`, Warp Control): if Warp ships
   a real open-tab CLI (issue #3959), only `Opener.Open`'s internals change.
6. **Warp Stable only.** Warp Preview registers only the `warppreview://`
   URI scheme and reads `~/.warp-preview/tab_configs/`, so Preview users
   get the tmux fallback; called out in troubleshooting, not special-cased
   in code.

## Error handling

- `warp.New()` failure inside `sm ssh` → degrade to plain ssh (existing
  path). In the local TUI, `warpWindows()` simply stays false and window
  mode falls back to tmux windows, same as today.
- `Open` failures (TOML write, opener exit) surface through the existing
  `silentDoneMsg` error channel like ghostty's. Beyond a successful
  `open`/`xdg-open` exit the launch is fire-and-forget — same contract as
  the iTerm2 escape path; documented in troubleshooting.

## Testing

- `internal/warp` unit tests, mirroring `ghostty_test.go`: detection matrix
  (env pinned per the f6d965e convention), TOML rendering incl. escaping
  round-trip and quote-heavy lines, per-OS dir/URI construction, injected
  `run` capturing the opener invocation, dir-creation behavior.
- `internal/ui/model_test.go`: a `newWarpModel` builder pinning every
  consulted env stub; cases for gating (`warpWindows` true/false over ssh),
  dispatch precedence, and tmux fallback when Warp absent. **Every existing
  builder/test that pins `iTerm2Env`/`ghosttyEnv` must also pin
  `warpEnv = false`** — the f6d965e rule: a new consulted var unpinned
  makes the suite environment-dependent, and this development machine's
  terminal IS Warp, so the breakage would be immediate and local.
- `sshmain` opener selection extracted into a testable helper; selection
  covered by unit test, ssh execution untouched.
- Live QA on this machine: local resume/new/attach-live in Warp, then
  `sm ssh` to a remote box, both with `tmux.enabled` on and off. Linux
  (Warp-on-Linux desktop) is implemented per docs but flagged needs-QA.

## Docs

- `docs/native-windows.md`: a Warp section alongside iTerm2/Ghostty —
  local setup is "nothing to install", `sm ssh` works the same; notes: the
  `sm` entry visible in Warp's Tab Config pickers, no window refocus,
  Linux Wayland `xdg-open` caveat, fire-and-forget error surface, Warp
  Preview unsupported.
- README: add Warp to the supported-terminals line, pointer to the guide.

## Rollout

Branch `feat/warp-native-windows` off the freshly synced `main`; keep the
diff strictly additive to maximize upstream mergeability; PR upstream after
local + SSH QA passes (second upstream PR alongside agent-tabs).
