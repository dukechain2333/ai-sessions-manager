# cs — Claude Code Session Manager (TUI) — Design

**Date:** 2026-07-10
**Status:** Approved by William (design conversation, 2026-07-10)

## Problem

Claude Code sessions are scattered across per-directory histories under
`~/.claude/projects/<dir-slug>/<session-uuid>.jsonl` (currently ~120 sessions in 11
directories on GeneralServer). There is no way to see them all in one place, tell them
apart, or jump back into one without remembering which directory it lived in.

An earlier idea (dockerized web app with click-to-resume) was rejected because a web
page cannot cleanly open a client terminal. A TUI that runs where the sessions live
makes resume trivial and works identically over ssh.

## Goal

A single-binary terminal app, `cs`, that:

1. Lists every Claude Code session on the machine in one recency-sorted list.
2. Previews any session's transcript before resuming.
3. Resumes a session with one keypress, in its original working directory.
4. Can start a new session in a chosen directory and delete (trash) old sessions.

## Stack

- Go (single static binary; `go build`, no runtime deps on target machines)
- Bubble Tea (TUI framework), Bubbles (list/textinput/viewport components),
  Lipgloss (styling)

## Data source (read model)

Session files: `~/.claude/projects/<dir-slug>/<session-uuid>.jsonl`, one JSON record
per line. Record types observed (Claude Code ~2.1.x):

- `user` / `assistant` — messages; carry `cwd`, `sessionId`, `timestamp`, `gitBranch`,
  `version`, and `message` (Anthropic API shape; `content` is a string or block array).
  Some `user` records are meta (`isMeta`, local-command caveats, hook attachments) and
  are not real prompts.
- `ai-title` — `{"aiTitle": "..."}`, the display title Claude Code generated. May
  appear multiple times; last one wins.
- `summary` — compaction summaries.
- `system`, `attachment`, `file-history-snapshot`, `mode`, `permission-mode`,
  `last-prompt`, etc. — ignored except where noted.

Derived per-session metadata:

| Field | Source | Fallback |
|---|---|---|
| Title | last `ai-title` record | first real user prompt, truncated |
| Working dir | `cwd` of last message record | decoded from dir-slug |
| Project label | last path component of cwd | dir-slug |
| Last activity | max `timestamp` | file mtime |
| Git branch | last `gitBranch` | empty |
| Message count | count of real user + assistant records | 0 |

Sessions with zero real user messages ("empty" — hook/meta-only files) are hidden by
default behind a show-empty toggle.

### Loading strategy

Two-phase so the UI is instant even with many/large files:

1. **Scan** (synchronous, <10 ms): glob the project dirs, build list entries from
   filename + mtime, render immediately, sorted by mtime desc.
2. **Enrich** (background, concurrent workers): stream-parse each file line by line,
   keeping only the lightweight metadata above; send updates to the UI as each file
   finishes. Malformed lines are skipped, never fatal.

Full transcript parsing happens only when a session is selected in the UI, on demand,
with a small LRU cache (e.g. 8 transcripts) keyed by path+mtime.

## UI

Two-pane layout with fuzzy filter (approved mockup):

```
┌ cs ─ Claude Sessions ────────────────────────────────────┐
│ 🔍 filter…                                    120 sessions │
├───────────────────────────┬───────────────────────────────┤
│ ▶ Create slides from      │ Transcript ─────────────────  │
│   Hyper_SAGNN notes       │ › You: make slides from       │
│   hyper-sagnn · 2d ago    │   my notes...                 │
│                           │ ● Claude: I'll read the       │
│   Slide deck polish       │   notes file first...         │
│   hyper-sagnn · 5d ago    │                               │
├───────────────────────────┴───────────────────────────────┤
│ ↵ resume  tab focus  n new  d delete  / filter  q quit    │
└────────────────────────────────────────────────────────────┘
```

- **Left pane — session list.** One flat list, recency-sorted. Each row: title (bold),
  project badge + relative time (dim), git branch if present. `/` enters filter mode
  (typing edits the fuzzy query live, matching title + project + first prompt;
  `enter` keeps the filter and returns focus to the list, `esc` clears it) — explicit
  `/` avoids clashing with `j/k` navigation.
- **Right pane — transcript preview.** Scrollable viewport of the selected session.
  User messages prefixed `›`, assistant `●`, styled distinctly; assistant tool calls
  collapsed to dim one-liners (e.g. `⚒ Bash: git status`); thinking omitted. `Tab`
  toggles focus between list and preview (focused pane gets an accented border).
- **Bottom — key hints**, context-sensitive (different hints in dialogs).
- Adaptive colors so it reads well on light and dark terminals; degrade gracefully
  below ~80 columns by hiding the preview pane.

### Keybindings

| Key | Action |
|---|---|
| `↑/↓` `j/k` | move selection |
| `enter` | resume selected session |
| `tab` | toggle focus list ⇄ preview |
| `/` | enter fuzzy-filter mode |
| `esc` | clear filter / close dialog |
| `n` | new session (directory picker) |
| `d` | delete session (confirm dialog) |
| `e` | toggle show-empty sessions |
| `r` | rescan |
| `q` / `ctrl+c` | quit |

## Actions

- **Resume (`enter`):** `tea.ExecProcess` runs `claude --resume <sessionId>` with
  `cmd.Dir = session.cwd`, stdio attached to the real terminal; on exit the TUI
  restores and triggers a rescan. If `cwd` no longer exists, a dialog says so and
  offers the directory picker instead.
- **New session (`n`):** directory picker listing known project dirs (decoded from
  `~/.claude/projects` slugs, deduped, existing-only) plus a free-form path input with
  `~` expansion; runs `claude` in the chosen dir via `ExecProcess`.
- **Delete (`d`):** confirm dialog showing title + project; on confirm, move the
  `.jsonl` to `~/.claude/projects/.trash/<dir-slug>/<uuid>.jsonl` (`os.Rename`, create
  dirs as needed). Never `rm`. No un-trash UI (manual `mv` suffices; YAGNI).

## Errors

- `claude` not on PATH → styled error dialog with the exact problem.
- Unreadable/corrupt session file → skipped with the entry marked `(unreadable)`.
- Resume of a session whose file vanished mid-session → rescan and show notice.

## Architecture

```
cmd/cs/main.go          — flag parsing (--projects-dir override), tea.NewProgram
internal/store/         — scan, jsonl parsing, metadata, transcript, trash
internal/ui/            — Bubble Tea root model + panes, dialogs, styles, keymap
```

`store` is UI-free and fully unit-testable; `ui` consumes it through plain structs
(`Session`, `Transcript`, `Message`). Root model owns focus/dialog state and delegates
to pane sub-models.

## Testing

- `store`: table-driven unit tests against fixture `.jsonl` files copied (sanitized)
  from real sessions — title extraction, meta-record filtering, empty detection,
  malformed-line tolerance, trash move.
- `ui`: table-driven tests of `Update` for key handling (filter, focus, dialogs)
  using Bubble Tea's pure model functions.
- End-to-end: run the binary against the real `~/.claude/projects` on GeneralServer
  and on auxserver (`ssh auxserver`, x86_64, has its own sessions).

## Delivery

- Git repo `~/claude-sessions`, Go module `github.com/dukechain2333/claude-sessions`.
- `make build` → `./cs`; `make install` → `~/.local/bin/cs`.

## Out of scope (YAGNI)

Web UI, remote/multi-host aggregation, session export, un-trash UI, config file,
mouse support, Windows terminals.
