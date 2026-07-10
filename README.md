# sm — AI sessions manager

A single-binary TUI that lists every Claude Code session on the machine,
groups them by project, previews transcripts, and resumes any session in
its original directory.

## Install

    make install        # builds and copies to ~/.local/bin/sm

Requires Go ≥1.24 to build; the binary itself has no dependencies.

## Use

    sm                      # browse ~/.claude/projects
    sm --projects-dir DIR   # browse another location

| Key | Action |
|---|---|
| `↑/↓` `j/k` | move selection (over project headers and sessions) |
| `enter` | resume selected session (`claude --resume` in its cwd); on a project header, fold/unfold it |
| `space` | fold/unfold the current project group |
| `g` | toggle grouping by project ⇄ flat recency |
| `tab` | toggle focus list ⇄ preview |
| `/` | fuzzy filter (enter keeps it, esc clears it) |
| `n` | new session in a picked directory |
| `d` | delete session (moved to `~/.claude/projects/.trash/`, never rm'd) |
| `e` | show/hide empty sessions |
| `r` | rescan |
| `q` | quit |

Sessions are grouped under a project header (`▾` expanded, `▸` folded) with
a session count; fold a long project to tuck it away. Filtering falls back
to a flat, relevance-ordered list.

Deleted sessions are recoverable: move the `.jsonl` back out of `.trash/`.
