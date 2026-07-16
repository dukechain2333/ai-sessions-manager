# sm — AI sessions manager

A single-binary terminal UI that finds your local AI coding-agent sessions —
**Claude Code**, and **OpenAI Codex** when present — groups them by project,
previews the transcript, and drops you back into any conversation with one
keypress, in the directory the session originally lived in.

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

- **One list, every project** — scans `~/.claude/projects` (and
  `~/.codex/sessions` when present), grouped by project with foldable
  headers, sorted by recency.
- **Live transcript preview**, **fuzzy filter**, and **full-text search**
  across every message in every session.
- **Resume in the right place** — runs `claude --resume <id>` (or
  `codex resume <id>`) in the session's original directory, even for
  sessions that later `cd`'d elsewhere.
- **New session** in any known project directory; **safe delete** (files
  move to `.trash/`, never `rm`'d).
- **Two views** — a mixed list or per-agent tabs (`v` toggles), each agent
  with its own accent color.
- **Optional [tmux integration](#tmux-integration)** — launches run in
  detachable tmux sessions with live `●` markers and a kill key.
- **[Real OS windows](#opening-launches-in-new-windows)** — with
  `open_in: "window"`, launches open native iTerm2 or Ghostty windows
  while `sm` stays put; works locally and over SSH.
- Single static binary (macOS & Linux, Intel & Apple Silicon), no runtime
  dependencies.

## Install

`sm` needs the [Claude Code](https://claude.com/claude-code) CLI (`claude`)
on your `PATH` to actually resume sessions.

**Homebrew (macOS & Linux)**

```sh
brew install dukechain2333/tap/sm
```

**APT repository (Debian / Ubuntu, amd64 & arm64)** — add once, then it
upgrades like any other package:

```sh
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://dukechain2333.github.io/ai-sessions-manager/public.key \
  | sudo gpg --dearmor -o /etc/apt/keyrings/ai-sessions-manager.gpg
echo "deb [signed-by=/etc/apt/keyrings/ai-sessions-manager.gpg] https://dukechain2333.github.io/ai-sessions-manager stable main" \
  | sudo tee /etc/apt/sources.list.d/ai-sessions-manager.list
sudo apt update && sudo apt install ai-sessions-manager
```

> The package is named `ai-sessions-manager` (the command it installs is
> `sm`), because `sm` already exists in the Ubuntu archive.

**Install script** — drops the latest release binary into `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/dukechain2333/ai-sessions-manager/main/install.sh | sh
# options: … | sh -s -- --version v0.3.0 --bin /usr/local/bin
```

**Everything else** — the
[releases page](https://github.com/dukechain2333/ai-sessions-manager/releases)
has one-off `.deb` / `.rpm` packages (`sudo apt install ./<file>.deb`,
`sudo rpm -i <file>.rpm`) and plain `tar.gz` binaries for every platform.
Or build from source (Go ≥ 1.24): `git clone` this repo and `make install`.

<details>
<summary><b>Beta releases</b></summary>

Prerelease versions (`v*-beta.*`) never reach the stable channels above.
To opt in:

```sh
# Homebrew — the beta ships as its own cask (uninstall the stable one first):
brew uninstall sm 2>/dev/null; brew install --cask dukechain2333/tap/sm-beta

# Debian / Ubuntu — install the .deb straight from the release page:
curl -sLO https://github.com/dukechain2333/ai-sessions-manager/releases/download/v0.5.0-beta.3/ai-sessions-manager_0.5.0-beta.3_linux_amd64.deb
sudo apt install ./ai-sessions-manager_0.5.0-beta.3_linux_amd64.deb
```

The package versions itself `0.5.0~beta.N`, which Debian sorts *before*
`0.5.0` — when the stable release lands in the APT repo, a normal
`apt upgrade` replaces the beta automatically. On Homebrew, switch back
with `brew uninstall --cask sm-beta && brew install dukechain2333/tap/sm`.

</details>

## Usage

```sh
sm                      # browse ~/.claude/projects (and ~/.codex/sessions, if present)
sm --projects-dir DIR   # a different Claude Code location
sm --codex-dir DIR      # a different Codex sessions location
sm --config PATH        # a different config.json
sm ssh HOST             # ssh + Ghostty window bridge, see "Real OS windows"
sm --version
```

### Keys

| Key | Action |
|---|---|
| `↑/↓` `j/k` | move the selection (edges ring the bell; `↑` at the top enters the filter bar) |
| `enter` | resume the selected session; on a project header, fold/unfold |
| `space` | fold / unfold the current project group |
| `g` | toggle grouping by project ⇄ flat recency |
| `v` | toggle view: mixed list ⇄ per-agent tabs |
| `a` | list mode: per-project agent subheaders on/off; tab mode: switch Claude ⇄ Codex |
| `tab` | focus the preview pane (scroll long transcripts) and back |
| `/` | fuzzy filter (enter keeps it, esc clears it) |
| `s` | full-text search |
| `n` | new session in a picked directory (asks which agent when both are installed) |
| `d` | delete the selected session (moved to `.trash/`) |
| `x` | kill the selected session's tmux; on a project header, all of that project's (with confirm) |
| `e` | show / hide "empty" sessions (hook-only, no real prompts) |
| `r` | rescan |
| `,` | settings (edit `config.json` in-app; saved changes apply on restart) |
| `q` | quit |

### Mouse

Everything is clickable: click selects (double-click resumes), headers
fold, the scroll wheel moves the selection or scrolls the preview, and
help-bar actions and dialog buttons are buttons. With mouse reporting on,
select text with **Shift+drag** (standard for mouse-enabled TUIs).

### Search

The filter bar has two layers — **Tab** switches between them (`/` focuses
the fuzzy layer directly, `s` the full-text layer):

- **filter…** — fuzzy match over title, project, and first prompt.
- **search…** — full-text search over everything said in every session.
  Space-separated terms must all appear (AND). Results are ordered by hit
  count; the preview jumps to the first hit, and `n` / `N` (preview
  focused) step through hits.

The first search builds a plain-text index under your user cache directory
(`sm-index/`); after that only changed sessions are re-indexed.

### Resuming, and getting sessions back

`enter` suspends `sm`, runs the agent in the session's original directory,
and returns to the list when it exits. The first time Claude opens a
directory it may ask **"Is this a project you trust?"** — that's Claude
Code's own gate, not `sm`.

Deletes are just moves. To restore a session:

```sh
mv ~/.claude/projects/.trash/<project-slug>/<id>.jsonl \
   ~/.claude/projects/<project-slug>/
```

## Configuration

On first run `sm` writes this default `config.json` to
`$XDG_CONFIG_HOME/sm/config.json` (usually `~/.config/sm/config.json`);
point elsewhere with `--config`. Editing is optional, `sm` never overwrites
your changes, and a malformed file falls back to defaults with a notice.

You can also press `,` inside `sm` to edit every setting in a dialog —
saving rewrites `config.json` in the canonical shape; changes apply the
next time `sm` starts.

```json
{
  "view": "list",
  "open_in": { "mode": "current", "iterm2": { "ssh": "" } },
  "tmux": { "enabled": false },
  "colors": {
    "claude": { "light": "#C15F3C", "dark": "#D97757" },
    "codex":  { "light": "#0A7C66", "dark": "#10A37F" }
  }
}
```

| Key | Values | What it does |
|---|---|---|
| `view` | `"list"` (default) / `"tabs"` | startup view mode; `v` toggles live |
| `open_in.mode` | `"current"` (default) / `"window"` | `"current"` suspends `sm` and runs the agent in this terminal; `"window"` opens every launch in a [new window](#opening-launches-in-new-windows) while `sm` stays on screen. Shorthand: `"open_in": "window"` |
| `open_in.iterm2.ssh` | ssh destination | only for iTerm2 windows when `sm` runs over SSH — whatever you type after `ssh` on the Mac. See below |
| `tmux.enabled` | `false` (default) / `true` | launches run in tmux sessions named `sm-<agent>-<id8>`, so work survives detaching; adds the `●` markers and `x` kill. Needs `tmux` on `PATH` |
| `colors.claude` / `colors.codex` | `{"light","dark"}` hex | per-agent accent colors |

`open_in` and `tmux.enabled` compose: with tmux on, windowed launches are
tracked (`●`, `x`, re-enter); with it off, windows are untracked.

## Opening launches in new windows

With `"open_in": "window"`, resume/new open **real terminal windows** —
`sm` stays where it is. What kind of window depends on your terminal:

| Your terminal | Locally | Over SSH | One-time setup |
|---|---|---|---|
| **iTerm2** (macOS) | native window | native window on the Mac | [install the AutoLaunch script](docs/native-windows.md#iterm2-macos); over SSH also set `iterm2.ssh` |
| **Ghostty** (macOS 1.3+, Linux 1.2+) | native window | native window on the desktop | none locally; over SSH just connect with **`sm ssh <host>`** |
| anything else | tmux window | tmux window | `tmux` on `PATH` (`sm` auto-wraps itself in a tmux session named `sm`) |

The common minimal configs:

```json
{ "open_in": "window" }
```

works as-is for local iTerm2, local Ghostty, Ghostty over `sm ssh`, and
the tmux fallback. Only iTerm2-over-SSH needs the dial-back destination:

```json
{ "open_in": { "mode": "window", "iterm2": { "ssh": "myserver" } } }
```

In all native modes the windows run the same tracked tmux sessions as
everywhere else (`●`, `x`, re-enter), closing a window never kills the
session, and repeating a launch focuses its still-open window. **The full
guide — setup steps, how each mechanism works, troubleshooting, and the
security model — lives in [docs/native-windows.md](docs/native-windows.md).**

## tmux integration

- A session with a live tmux shows a `●` marker; a project header shows
  `●` when any of its sessions has one.
- `x` kills the selected session's tmux; on a project header, all of that
  project's (after a confirm). Kills done outside `sm` are noticed
  automatically.
- Known edge: a **new** session's tmux is linked to its list row on the
  next rescan by matching the newest session in that directory; starting
  two new sessions in the same directory before returning can label them
  in either order (both stay killable from the project header).

## Uninstall

```sh
brew uninstall sm                # homebrew
sudo apt remove ai-sessions-manager  # apt / deb
sudo rpm -e ai-sessions-manager  # rpm
rm -f ~/.local/bin/sm            # script / manual installs
```

## Development

```sh
make test    # go test ./...
make vet     # go vet ./...
make build   # ./sm
```

Architecture: `internal/store` is a UI-free reader over the session
`.jsonl` files; `internal/ui` is the
[Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI;
`internal/bridge` + `internal/ghostty` + `scripts/iterm2/` implement the
[native-window launchers](docs/native-windows.md). Design notes live under
`docs/`.

Releases are automated: pushing a `v*` tag runs
[GoReleaser](https://goreleaser.com) — binaries, `.deb`/`.rpm`, the GitHub
Release, the Homebrew tap, and the APT repo all come from that one tag
(prerelease tags feed only the [beta channel](#install)).

## License

[MIT](LICENSE)
