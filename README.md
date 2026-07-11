# sm — AI sessions manager

A single-binary terminal UI that finds every **Claude Code** session on your
machine, groups them by project, previews the transcript, and drops you back
into any conversation with one keypress — in the right directory, wherever the
session originally lived.

Claude Code stores each session as a `.jsonl` file under
`~/.claude/projects/<dir>/`. Once you work across many directories they become
impossible to track. `sm` gathers them into one browsable, foldable list and
runs `claude --resume` for you.

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
 ↵ resume  n new  d delete  / filter  s search  g group  space fold  q quit
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
curl -fsSL .../install.sh | sh -s -- --version v0.1.0 --bin /usr/local/bin
```

If `~/.local/bin` isn't on your `PATH`, the script prints the line to add.

### Homebrew (macOS & Linux)

Once the tap is published (see [Releasing](#releasing)):

```sh
brew install dukechain2333/tap/sm
```

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
sm                      # browse ~/.claude/projects
sm --projects-dir DIR   # browse a different location
sm --version
```

| Key | Action |
|---|---|
| `↑/↓` `j/k` | move selection (over project headers and sessions); ↑ at the top enters the search bar, ↓ in the bar returns |
| `enter` | resume the selected session; on a **project header**, fold/unfold it |
| `space` | fold / unfold the current project group |
| `g` | toggle grouping by project ⇄ flat recency |
| `tab` | move focus to the preview pane (to scroll a long transcript) and back |
| `/` | fuzzy filter (enter keeps it, esc clears it) |
| `s` | focus the search bar on the full-text layer |
| `n` | start a new session in a picked directory |
| `d` | delete the selected session (moved to `.trash/`) |
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

Pressing `enter` on a session suspends `sm`, runs `claude --resume <id>` in the
session's original directory, and returns you to the list when Claude exits.

The first time Claude opens a directory it may show its own **"Is this a
project you trust?"** prompt — that's Claude Code's security gate, not `sm`.
Choose *"Yes, I trust this folder"* (the default) and Claude remembers it.
Declining it, or pressing `Ctrl-C`/`/exit`, simply returns you to the list.

### Recovering a deleted session

Deletes are just moves. To restore one:

```sh
mv ~/.claude/projects/.trash/<project-slug>/<id>.jsonl \
   ~/.claude/projects/<project-slug>/
```

## Uninstall

```sh
rm -f ~/.local/bin/sm            # or wherever you installed it
# packaged installs:
sudo dpkg -r ai-sessions-manager # deb
sudo rpm -e ai-sessions-manager  # rpm
brew uninstall sm                # homebrew
```

## Releasing

Releases are automated with [GoReleaser](https://goreleaser.com) via GitHub
Actions. Pushing a tag builds binaries, `.deb`/`.rpm` packages, archives, and
checksums, and publishes a GitHub Release:

```sh
git tag v0.1.0
git push origin v0.1.0
```

**To also publish the Homebrew cask**, do the one-time setup:

1. Create a public repo `dukechain2333/homebrew-tap`.
2. Create a token with `contents:write` on that tap repo (a classic PAT with
   `repo` scope, or a fine-grained token scoped to `homebrew-tap`).
3. Add it to this repo's secrets as `HOMEBREW_TAP_GITHUB_TOKEN`
   (Settings → Secrets and variables → Actions).

The next tagged release pushes the cask; without the secret the release still
succeeds and just skips Homebrew.

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
