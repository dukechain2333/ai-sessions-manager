# sm — Full-Text Search — Design

**Date:** 2026-07-10
**Status:** Approved (design conversation, 2026-07-10)

## Problem

The filter bar only fuzzy-matches title + project label + first prompt.
Anything said *inside* a conversation is unfindable: remembering "the session
where we discussed webhook field clobbering" doesn't help unless those words
happen to be in the title. The corpus is too big to grep live per keystroke
(1.7 GB of `.jsonl` on the reference machine; single sessions up to 543 MB).

## Goal

Two search layers on the existing filter bar, switchable while typing:

1. **Title layer** (default) — the existing fuzzy filter, unchanged.
2. **Full-text layer** — searches every session's message text, backed by a
   lazily built, incrementally refreshed plain-text index cache.

Zero new dependencies; single static binary preserved.

## Interaction spec

| Action | Behavior |
|---|---|
| `/` or click the filter bar | focus the input (existing) |
| **Tab while the filter is focused** | toggle title ⇄ full-text layer (Tab is currently a dead key in this state) |
| **Click the `>` prompt** (bar columns 0–1) | toggle layer AND focus the input; clicking the rest of the bar just focuses |
| Placeholder | `filter…` (title layer) / `search…` (full-text layer) |
| Enter | keep query, focus back to the list (existing semantics, both layers) |
| Esc | clear query, blur, **and reset to the title layer** |
| `n` / `N` **with preview focused** and a full-text query active | jump to next / previous hit message in the preview (`n` in list focus keeps meaning "new session") |

Full-text results: the list shows only matching sessions, ordered by hit
count desc, then recency. The session meta line gains `· N hits` (N =
matching **messages**, the jump granularity). The preview auto-scrolls to the
first hit message and highlights query terms inverse-video (ANSI-aware; an
occurrence that spans styled boundaries degrades to jump-without-highlight
rather than corrupting output). Empty query in full-text layer shows all
sessions, like the empty filter.

Title bar count area: `146 sessions · 23 matched` when a full-text query is
active; `indexing 87/146…` while the index builds (searches during the build
return results from already-indexed sessions, suffixed `…`).

## Search semantics (full-text layer)

- Case-insensitive **substring** match. No fuzzy: over a GB-scale corpus
  fuzzy matching is all noise.
- Space-separated terms AND together at the session level (every term must
  appear somewhere in the session); a message "hits" if it contains at least
  one term; highlight covers all terms.
- Match against extracted message text only — user and assistant `message`
  records' text content. Tool outputs, meta records, attachments, base64,
  and thinking blocks are excluded at extraction time, so they can neither
  match nor bloat the index.

## Index design

- Location: `os.UserCacheDir()/sm-index/` (macOS `~/Library/Caches`, Linux
  `~/.cache`).
- One file per session: `<sha1(session path)>.txt`. First line is the
  validity key `path\tmtime\tsize`; the body is the session's messages
  joined by `\x1e` record-separator lines. A hit's message index = count of
  separators before the match offset.
- **Lazy build**: nothing happens until the user first switches to the
  full-text layer. Then a background pass (concurrent workers, same
  channel/message pattern as `store.Enrich`) walks all sessions: cache file
  fresh (path+mtime+size match) → skip; else stream-extract and rewrite.
  Extraction is line-streaming — giant files are never held in memory
  (unlike the preview path, which may legitimately parse a whole transcript
  on demand).
- Incremental forever after: re-entering full-text mode or pressing `r`
  (rescan) re-checks validity keys and re-extracts only changed sessions.
- Corrupt/unreadable cache files are treated as stale and rebuilt; a session
  whose source `.jsonl` is unreadable is skipped (consistent with Enrich's
  error handling).

## Architecture

- `internal/store/searchindex.go` (new): `SearchIndex` type owning the cache
  dir; `EnsureAll(sessions, workers, ch)` background build with progress
  results; `Search(query, sessions) []SessionHits{Index, MsgHits, First}`;
  message-text extraction shared with the transcript parser (the per-record
  text logic is factored out of `transcript.go`, not duplicated).
- `internal/ui/model.go`: search layer state on the filter (`searchAll
  bool`), Tab handling in the `focusFilter` branch, 150 ms debounce via
  `tea.Tick`, index-progress messages, title-bar count states, Esc reset.
- `internal/ui/listpane.go`: a search-results mode (matched session set +
  hit counts; flat, hits-desc order) alongside the existing filter mode.
- `internal/ui/preview.go`: render returns per-message start lines; hit
  jump + `n`/`N` + ANSI-aware term highlight.
- `internal/ui/mouse.go`: 🔍-icon click toggles the layer (a few lines in
  the `zoneFilter` branch).

## Edge cases

- Search while indexing: partial results + `…` indicator; completed build
  triggers one final re-search.
- Sessions created/trashed while sm runs: `r` rescan path refreshes both the
  session list and index validity.
- Narrow mode (no preview pane): hit counts still show in the list; `n`/`N`
  are list-focus keys there and keep their existing meanings (no preview to
  navigate).
- The 543 MB outlier: extraction streams and its cache file contains text
  only; if extraction of one file fails mid-stream, its partial cache is
  discarded (no half-indexed sessions).
- Terminal without mouse: Tab toggle covers everything the 🔍 click does.

## Testing

- store: extraction purity (tool noise excluded), cache validity/invalidations
  (mtime/size change), incremental rebuild skips fresh files, AND semantics,
  hit counting per message, corrupt-cache recovery — all against synthetic
  fixtures (a few KB), never real transcripts.
- ui: Tab toggles layer + placeholder, Esc resets layer, debounce fires one
  search per settle, matched-list ordering, meta `· N hits` rendering,
  preview first-hit jump + n/N cycling + highlight presence, 🔍-icon click
  vs bar-body click, title-bar states (matched / indexing).
- Gate: `gofmt -l .` empty, `go vet ./...`, `go test -race ./...`.

## Out of scope

- Regex or query operators beyond space-AND.
- Relevance ranking beyond hit-count/recency.
- Filesystem watching (index refresh is event-driven: mode entry / rescan).
- Upstream PR — this stays on the local fork (`feat/search`, stacked on
  `feat/mouse-support`).
