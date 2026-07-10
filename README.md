# cs — Claude Code session manager

A single-binary TUI that lists every Claude Code session on the machine,
previews transcripts, and resumes any session in its original directory.

## Install

    make install        # builds and copies to ~/.local/bin/cs

Requires Go ≥1.24 to build; the binary itself has no dependencies.

## Use

    cs                      # browse ~/.claude/projects
    cs --projects-dir DIR   # browse another location

| Key | Action |
|---|---|
| `↑/↓` `j/k` | move selection |
| `enter` | resume selected session (`claude --resume` in its cwd) |
| `tab` | toggle focus list ⇄ preview |
| `/` | fuzzy filter (enter keeps it, esc clears it) |
| `n` | new session in a picked directory |
| `d` | delete session (moved to `~/.claude/projects/.trash/`, never rm'd) |
| `e` | show/hide empty sessions |
| `r` | rescan |
| `q` | quit |

Deleted sessions are recoverable: move the `.jsonl` back out of `.trash/`.
