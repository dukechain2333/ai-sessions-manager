# sm — AI sessions manager

A single-binary terminal UI that finds your local AI coding-agent sessions —
**Claude Code**, and **OpenAI Codex** when present — groups them by project,
previews the transcript, and drops you back into any conversation with one
keypress, in the directory the session originally lived in.

Each agent stores its sessions as `.jsonl` files (Claude Code under
`~/.claude/projects/`, Codex under `~/.codex/sessions/`). Once you work across
many directories they become impossible to track. `sm` gathers them into one
browsable, foldable list and resumes any of them for you.

```
✻ sm · AI Sessions   52 sessions
 > filter…
╭────────────────────────────────────╮╭─────────────────────────────────────╮
│ ▾ ai-sessions-manager (1)          ││ > So currently my Claude Code       │
│ ▶ Build session history web app    ││   sessions are dispersed among      │
│   ai-sessions-manager · just now   ││   different dirs…                   │
│ ▾ HyperSAGNN_Interaction (4)       ││ ⏺ Using superpowers:brainstorming   │
│   Experiment with top 3 fit …      ││   to explore the design…            │
│   HyperSAGNN_Interaction · 4h ago  ││                                     │
│ ▸ william (12)                     ││ ⎿ Skill: superpowers:brainstorming  │
│ ▸ prs-net (2)                      ││                                     │
╰────────────────────────────────────╯╰─────────────────────────────────────╯
 ↵ resume  tab focus  n new  d delete  / filter  a agent  g group  q quit
```

## Features

- **One list, every project** — scans `~/.claude/projects/*/*.jsonl` and sorts
  by recency.
- **Grouped by project**, with foldable headers so long projects can be tucked
  away (`▾` open, `▸` folded).
- **Live transcript preview** of the highlighted session.
- **Fuzzy filter** across title, project, and first prompt.
- **Resume in the right place** — launches `claude --resume <id>` in the
  session's original directory (the one Claude filed it under), so resume
  works even for sessions that later `cd`'d elsewhere.
- **New session** in any known project directory, and **safe delete** (files
  are moved to `~/.claude/projects/.trash/`, never `rm`'d).
- **OpenAI Codex sessions, too** — when `~/.codex` exists, its sessions are
  scanned alongside Claude Code's. Two view modes, toggled with `v`: the
  default **list** mode shows one mixed list (rows tagged `claude` /
  `codex`, with optional per-project agent subheaders on `a`), while **tab**
  mode shows one agent at a time — the title bar grows `[Claude N]  Codex M`
  tabs, `a` or a click switches views, and the whole accent theme follows
  (coral for Claude, teal-green for Codex). Set `"view": "tabs"` in
  `config.json` to start there.
- **Full-text search** across every message in every session, on top of the
  quick fuzzy filter.
- **Optional [tmux integration](#tmux-integration)** — resume into a
  detachable tmux session, with live markers and a kill key — and a
  [`config.json`](#configuration) for the toggle and per-agent colors.
- Cross-platform single static binary (macOS & Linux, Intel & Apple Silicon),
  no runtime dependencies.

## Install

`sm` needs the [Claude Code](https://claude.com/claude-code) CLI (`claude`) on
your `PATH` to actually resume sessions.

### Quick install (macOS & Linux)

```sh
curl -fsSL https://raw.githubusercontent.com/dukechain2333/ai-sessions-manager/main/install.sh | sh
```

Installs the latest release to `~/.local/bin/sm`. Options:

```sh
# specific version, or a custom install directory
curl -fsSL .../install.sh | sh -s -- --version v0.3.0 --bin /usr/local/bin
```

If `~/.local/bin` isn't on your `PATH`, the script prints the line to add.

### Homebrew (macOS & Linux)

```sh
brew install dukechain2333/tap/sm
```

Upgrades come through `brew upgrade`.

### Debian / Ubuntu (`apt`)

Add the signed APT repository once, then install and upgrade with `apt` like
any other package (amd64 and arm64 supported):

```sh
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://dukechain2333.github.io/ai-sessions-manager/public.key \
  | sudo gpg --dearmor -o /etc/apt/keyrings/ai-sessions-manager.gpg
echo "deb [signed-by=/etc/apt/keyrings/ai-sessions-manager.gpg] https://dukechain2333.github.io/ai-sessions-manager stable main" \
  | sudo tee /etc/apt/sources.list.d/ai-sessions-manager.list
sudo apt update
sudo apt install ai-sessions-manager      # installs the `sm` command
```

Upgrades then come through `sudo apt update && sudo apt upgrade`.

> The package is named `ai-sessions-manager` (the command it installs is `sm`),
> because `sm` already exists in the Ubuntu archive.

### Debian / Ubuntu (single `.deb`, no repo)

Prefer a one-off install without adding a repo? Grab the `.deb` matching your
architecture from the
[latest release](https://github.com/dukechain2333/ai-sessions-manager/releases/latest):

```sh
sudo dpkg -i ai-sessions-manager_*_linux_amd64.deb
```

### Fedora / RHEL / openSUSE (`.rpm`)

```sh
sudo rpm -i ai-sessions-manager_*_linux_amd64.rpm
```

### Manual download

Grab a `sm_<version>_<os>_<arch>.tar.gz` from the
[releases page](https://github.com/dukechain2333/ai-sessions-manager/releases),
extract, and move `sm` onto your `PATH`.

### Build from source

Requires Go ≥ 1.24.

```sh
git clone https://github.com/dukechain2333/ai-sessions-manager
cd ai-sessions-manager
make install          # builds ./sm and copies it to ~/.local/bin/sm
```

## Usage

```sh
sm                      # browse ~/.claude/projects (and ~/.codex/sessions, if present)
sm --projects-dir DIR   # browse a different Claude Code location
sm --codex-dir DIR      # browse a different Codex sessions location
sm --version
```

| Key | Action |
|---|---|
| `↑/↓` `j/k` | move the selection over project headers and sessions; at the top or bottom edge it stays put and rings the terminal bell. Reach the filter/search bar with `/`, `s`, or a mouse click. |
| `enter` | resume the selected session; on a **project header**, fold/unfold it |
| `space` | fold / unfold the current project group |
| `g` | toggle grouping by project ⇄ flat recency |
| `a` | list mode: toggle per-project agent subheaders (`─ Claude ─` / `─ Codex ─`); tab mode: switch the Claude ⇄ Codex view |
| `v` | toggle view mode: mixed list ⇄ per-agent tabs |
| `tab` | move focus to the preview pane (to scroll a long transcript) and back |
| `/` | fuzzy filter (enter keeps it, esc clears it) |
| `s` | focus the search bar on the full-text layer |
| `n` | start a new session in a picked directory (in tab mode, launches the active view's agent; in list mode it asks when both agents are installed) |
| `d` | delete the selected session (moved to `.trash/`) |
| `x` | *(tmux integration on)* kill the selected session's tmux; on a **project header**, kill all of that project's (with a confirm). In a tab view the header dot and the kill cover only the active agent's tmux; the mixed list covers both. |
| `e` | show / hide "empty" sessions (hook-only, no real prompts) |
| `r` | rescan |
| `q` | quit |

### Mouse

The whole UI is clickable — `sm` enables mouse reporting:

| Gesture | Action |
|---|---|
| click a session | select it (the preview follows) |
| double-click a session | resume it |
| click a project header | fold / unfold that project |
| scroll wheel | move the selection; over the preview, scroll the transcript |
| click the preview pane | focus it (like `tab`) |
| click the filter bar | start filtering (like `/`) |
| click a help-bar action or dialog button | performs that action (help-bar buttons act as if the list were focused) |
| click outside a dialog | cancel it |

With mouse reporting on, select text with **Shift+drag** (standard for
mouse-enabled TUIs).

### Search

The filter bar has two layers — press `/` to focus it, then **Tab** (or
click the `>` prompt) to switch. Pressing `s` in the list jumps straight to
the full-text layer; `↑` at the top of the list also enters the bar, and
`↓` leaves it.

- **filter…** — the default fuzzy filter over title, project, and first
  prompt.
- **search…** — full-text search over everything said in every session.
  Space-separated terms must all appear in a session (AND). Results are
  ordered by hit count; the preview jumps to the first hit with matches
  highlighted, and `n` / `N` (preview focused) step through hits.

The first full-text search builds a plain-text index under your user cache
directory (`sm-index/`); the title bar shows `indexing …` progress. After
that, searches are fast and only changed sessions are re-indexed.

### Resuming

Pressing `enter` on a session suspends `sm`, runs `claude --resume <id>` (or,
for a Codex session, `codex resume <id>`) in the session's original
directory, and returns you to the list when the agent exits.

The first time Claude opens a directory it may show its own **"Is this a
project you trust?"** prompt — that's Claude Code's security gate, not `sm`.
Choose *"Yes, I trust this folder"* (the default) and Claude remembers it.
Declining it, or pressing `Ctrl-C`/`/exit`, simply returns you to the list.

With tmux integration enabled (see [Configuration](#configuration)), resume
instead attaches you to a tmux session, so the work keeps running in the
background if you detach (`Ctrl-b d`).

### Recovering a deleted session

Deletes are just moves. To restore one:

```sh
mv ~/.claude/projects/.trash/<project-slug>/<id>.jsonl \
   ~/.claude/projects/<project-slug>/
```

## Configuration

On first run `sm` writes a default `config.json` at
`$XDG_CONFIG_HOME/sm/config.json` (or `~/.config/sm/config.json`) and reads it
on every launch; point elsewhere with `--config <path>`. Editing is optional —
the defaults below match `sm`'s built-in behavior — and `sm` never overwrites a
file you've changed. A malformed file falls back to defaults with a one-time
notice.

```json
{
  "view": "list",
  "open_in": "current",
  "tmux": { "enabled": false },
  "colors": {
    "claude": { "light": "#C15F3C", "dark": "#D97757" },
    "codex":  { "light": "#0A7C66", "dark": "#10A37F" }
  }
}
```

- `tmux.enabled` (default `false`) — when `true`, resume and new sessions run
  inside a tmux session named `sm-<agent>-<id8>`, so work survives detaching
  (Ctrl-b d). Requires `tmux` on `PATH`; if it is missing, `sm` shows a
  startup notice and runs without it.
- `open_in` (default `"current"`) — where `enter` (resume) and `n` (new
  session) launch the agent. `"current"` suspends `sm` and runs it in this
  terminal. `"window"` opens a new tmux window in the tmux session you are
  attached to — `sm` stays on screen, and over SSH the window naturally lives
  on the same connection. Started outside tmux, `sm` automatically
  re-launches itself inside its own tmux session (named `sm`) and reattaches
  it if one is already running — an SSH drop later, `sm` brings the whole
  workspace (sm plus every agent window) back. Needs `tmux` on `PATH`;
  without it `sm` shows a notice and falls back to `"current"` for the run.
  Works independently of `tmux.enabled`: with tmux integration on, the
  window is named `sm-…` and gets the ● marker / `x` kill treatment; with it
  off, it is a plain untracked window.
  **iTerm2 users get real OS windows:** when `sm` detects iTerm2 (via the
  `LC_TERMINAL` it forwards over ssh), the auto-relaunch attaches with
  `tmux -CC` — iTerm2's native tmux integration — so every window `sm`
  opens is a genuine iTerm2 window/tab, even over SSH. Pick windows vs
  tabs in iTerm2 under *Settings → General → tmux*.
- `colors.claude` / `colors.codex` — each takes optional `light` and `dark`
  `#RRGGBB` accents; omitted or invalid values keep the defaults.
- `"view"`: `"list"` (default) or `"tabs"` — the view mode `sm` starts in.
  `v` toggles it live either way.

### tmux integration

- A session with a live tmux shows a `●` marker; a project header shows `●`
  when any of its sessions has one.
- `x` kills the selected session's tmux; on a project header it kills all of
  that project's tmux (after a confirm).
- Killing a tmux outside `sm` (e.g. `tmux kill-session`) is detected
  automatically — the marker clears on the next refresh.
- Known edge: a **new** session's tmux is linked to its list row on the next
  rescan by matching the newest session in that directory; starting two new
  sessions in the same directory before returning can label them in either
  order (both stay killable from the project header).
- With `open_in: "window"`, tracked launches are tmux *windows* (named
  `sm-<agent>-<id8>`) instead of detached sessions; ●, `x`, and adoption
  work the same, and `enter` on a live one switches to its window.

## Uninstall

```sh
rm -f ~/.local/bin/sm            # or wherever you installed it
# packaged installs:
sudo dpkg -r ai-sessions-manager # deb
sudo rpm -e ai-sessions-manager  # rpm
brew uninstall sm                # homebrew
```

## Releasing

Releases are automated with [GoReleaser](https://goreleaser.com) and GitHub
Actions. Pushing a version tag builds the binaries, `.deb`/`.rpm` packages,
archives, and checksums, publishes a GitHub Release, updates the Homebrew cask
(tap `dukechain2333/homebrew-tap`), and refreshes the APT repo — all from the
one tag:

```sh
git tag v0.3.0
git push origin v0.3.0
```

The Homebrew tap and its `HOMEBREW_TAP_GITHUB_TOKEN` secret are already
configured; without the secret a release still succeeds and just skips the
cask.

## Development

```sh
make test    # go test ./...
make vet     # go vet ./...
make build   # ./sm
```

Architecture: `internal/store` is a UI-free reader over the session `.jsonl`
files (scan, metadata parse, transcript, trash); `internal/ui` is the
[Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI. Design notes and
the implementation plan live under `docs/superpowers/`.

## License

[MIT](LICENSE)
