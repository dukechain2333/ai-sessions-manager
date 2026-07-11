# Codex session support — Design

**Date:** 2026-07-11
**Status:** Approved (design conversation, 2026-07-11)

## Problem

`sm` browses and resumes **Claude Code** sessions. Many users also run
**OpenAI Codex** (`codex` CLI), whose sessions live in a different place and
format. They should appear in the same list, visually distinguished, and be
resumable — with an optional view that groups each project's sessions by
agent.

## Goal

1. List, preview, and resume Codex sessions alongside Claude sessions in one
   recency-sorted list.
2. Codex sessions render in a distinct accent (**OpenAI teal-green**); Claude
   keeps its current coral.
3. Default ordering unchanged (recency within project groups). A toggle
   sub-groups each project's sessions by agent (Claude / Codex).
4. New-session and delete work for both agents.
5. Codex support activates only when `~/.codex` exists — zero change for
   Claude-only users.

## Codex on-disk format (verified against real rollouts, cli 0.142.2)

Sessions: `~/.codex/sessions/<YYYY>/<MM>/<DD>/rollout-<ts>-<uuid>.jsonl`
(date-nested, unlike Claude's per-project-dir layout). One JSON record per
line:

- **`session_meta`** (first record) — `payload.session_id`, `payload.cwd`
  (the session's origin directory), `payload.timestamp`, `cli_version`.
- **`response_item`** — `payload.type` is one of:
  - `message` with `role` ∈ {user, assistant, developer} and `content` array
    of `{type: input_text|output_text, text}`.
  - `function_call` — a tool call, `name` + `arguments` (a JSON string).
  - `function_call_output`, `reasoning` — ignored (reasoning is Codex's
    "thinking").
- **`event_msg`**, **`turn_context`** — ignored.

Real user prompts are `message`/`role:user` whose text does **not** start with
`<` (the first user records are `<environment_context>` / permissions blocks —
harness-injected, same pattern as Claude). Codex has no AI-generated title.

**Resume:** `codex resume <session_id>` (looks the rollout up globally by
UUID; still launched in the session's `cwd` so it operates in the right
directory). **New:** `codex` in a directory.

## Architecture — provider model

`Session` gains:

```go
type Agent string
const (AgentClaude Agent = "claude"; AgentCodex Agent = "codex")
// Session gains: Agent Agent
```

A `Provider` interface (`internal/store/provider.go`) captures everything
agent-specific:

```go
type Provider interface {
    Agent() Agent
    Available() bool                                   // its data dir exists
    Scan() ([]Session, error)                          // fast dirent scan; sets Agent, ID, Path, LastActivity(mtime)
    ParseMetadata(path string) (Meta, error)           // title/cwd/first-prompt/counts/activity
    ParseTranscript(path string) (Transcript, error)   // user/assistant/tool messages
    Trash(s Session) (string, error)                   // provider owns its own trash dir
    ResumeCommand(s Session) (name string, args []string) // ("claude",{"--resume",id}) / ("codex",{"resume",id})
    NewCommand() (name string, args []string)             // ("claude",nil) / ("codex",nil)
    Binary() string                                       // "claude" / "codex" for PATH checks
}
```

Each provider is constructed with its own base directory (Claude: the
`--projects-dir` value, default `~/.claude/projects`; Codex: `~/.codex/sessions`),
so `Trash`/`Scan` need no directory argument. The UI builds the provider set
once at startup and passes it to `ScanAll`.

- **`claudeProvider`** (`internal/store/claude.go`) wraps the existing
  `scan.go`/`parse.go`/`transcript.go`/`trash.go` logic (largely a move, not a
  rewrite; those files' functions become the provider's methods or are called
  by it).
- **`codexProvider`** (`internal/store/codex.go`) implements the same over the
  rollout format above. Scan walks `~/.codex/sessions` recursively for
  `rollout-*.jsonl`; ID = the UUID; `Slug` unused (project comes from `cwd`).
  Codex trash: move the rollout into `~/.codex/sessions/.trash/` (never `rm`,
  matching Claude's guarantee).
- **`store.ScanAll(providers)`** runs each `Available()` provider's `Scan`
  concurrently and returns the merged slice sorted by `LastActivity` desc.
- A registry maps `Agent → Provider` so enrich, transcript, trash, and resume
  dispatch on `Session.Agent`. `store.ProviderFor(agent)` returns the provider.

`Project()` remains `basename(CWD)`, so a Claude and a Codex session in the
same directory share one project group.

The existing `SearchIndex` is provider-agnostic: it indexes each session's
`ParseTranscript` output keyed by session path, so it covers Codex for free
once transcripts are produced (the index dispatches parsing through the
registry).

## UI

### Color per agent (`internal/ui/styles.go`)

Add a Codex accent — `AccentCodex` = OpenAI teal-green,
`lipgloss.AdaptiveColor{Light: "29", Dark: "36"}` (approx `#10A37F`), with
selected/dim variants. In `listpane` rendering, a session's title (and its
selected style) uses the agent's accent: Claude = existing coral accent,
Codex = teal-green. The meta line gains a short agent tag — `claude` / `codex`
— colored to match, so agent is legible without relying on color alone.
Project headers and app chrome are unchanged.

### Agent grouping toggle

- New field `listPane.groupByAgent bool`, default false. Key **`a`** toggles
  it; a `a agent` entry is added to the help bar (and is mouse-clickable via
  the existing help-bar click path).
- **Off (default):** unchanged — sessions recency-ordered within each project.
- **On:** within each project group, sessions partition into a Claude
  subsection then a Codex subsection, each recency-ordered, each preceded by a
  dim subheader (`─ Claude ─` / `─ Codex ─`). A project with only one agent
  renders flat (no subheader).
- Row model gains a `subheader` row kind. `MoveCursor` **skips** subheader rows
  (they are inert labels); project headers still fold (folding a project hides
  its subheaders and sessions). Line-based scrolling counts subheaders as
  1-line rows.
- Search-results and active-filter views fall back to flat (no agent
  subgrouping), consistent with how project grouping already behaves.

### Resume / new / delete wiring (`internal/ui/model.go`)

- `runClaude` generalizes to `runAgent(agent store.Agent, dir, sessionID
  string)` that resolves the command via the provider's `ResumeCommand`.
  `claudeExitMsg`/`claudeMissingMsg` become agent-neutral (`agentExitMsg`,
  `agentMissingMsg{agent}`); the "not found on PATH" dialog names the specific
  binary.
- **New session (`n`):** if a session is selected, target dir = its project
  `CWD`; open an **agent-pick dialog** (`[1] Claude  [2] Codex`) and launch the
  chosen agent's `NewCommand` there. If nothing is selected, fall back to the
  existing directory picker, then the agent pick.
- **Delete (`d`):** dispatch `Trash` through the selected session's provider.

## Errors

- Missing agent binary at resume/new → styled dialog naming the binary.
- Unreadable/corrupt rollout → session skipped or marked `(unreadable)`, same
  as Claude.
- No `~/.codex` → Codex provider reports `Available() == false`; list shows
  Claude only.

## Testing

- **store:** `codex_test.go` with sanitized fixture rollouts — metadata
  (session_id, cwd, title fallback, message counts, activity), transcript
  extraction (user/assistant/tool one-liners; developer/reasoning excluded;
  `<…>` context excluded), empty-session detection, trash-to-`.trash`,
  malformed-line tolerance. Provider registry + `ScanAll` merge/sort (Claude +
  Codex intermixed by recency). `Available()` gating.
- **ui:** per-agent title/tag color; agent-grouping layout (mixed project
  subdivides with both subheaders; single-agent project stays flat); cursor
  skips subheaders; fold still hides a grouped project; new-session agent-pick
  dialog launches the right `NewCommand` in the current project dir; resume
  dispatches `codex resume <id>` vs `claude --resume <id>` (injected runner).
- **e2e:** build and run against the real `~/.claude` + `~/.codex` on this
  machine — Codex sessions appear teal, resume launches `codex resume <id>` in
  the origin cwd (fake-`codex` captures the command), `a` toggles subgrouping,
  narrow/scroll/fold still correct.

## Delivery

After implementation and verification, `make install` to `~/.local/bin/sm` on
this machine for the user to try. **Do not** cut a release / publish until the
user explicitly asks.

## Out of scope (YAGNI)

Codex-specific config parsing, non-CLI Codex sessions, cross-agent session
merging, and per-agent filtering beyond the grouping toggle.
