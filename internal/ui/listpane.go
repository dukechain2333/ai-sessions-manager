package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/sahilm/fuzzy"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

// sessionLines is the number of terminal rows one session occupies:
// title, meta, blank separator.
const sessionLines = 3

// row is one navigable line in the list: either a project header or a
// session. In group-by-project mode headers precede each project's
// sessions and can be folded to hide them.
type row struct {
	header  bool
	project string
	session int // index into listPane.sessions; valid when !header
}

// listPane is a hand-rolled scrolling session list. Sessions render as
// three rows (title, meta, blank); in group-by-project mode a one-row
// header precedes each project's first session, and folded projects show
// only their header. Scrolling is tracked in terminal lines so the mixed
// header/session heights don't throw the window off.
type listPane struct {
	sessions       []store.Session
	rows           []row
	counts         map[string]int  // session count per project label
	folded         map[string]bool // project label -> collapsed
	total          int             // visible session count (ignores fold)
	cursor         int             // index into rows
	lineOffset     int             // first visible terminal line (scrolling)
	width          int
	height         int
	filter         string
	showEmpty      bool
	groupByProject bool
	focused        bool
	styles         styles
	search         []store.SessionHits // non-nil: flat search-results mode
}

func (l *listPane) SetSize(w, h int) {
	l.width, l.height = w, h
	l.ensureVisible()
}

func (l *listPane) SetSessions(s []store.Session) {
	l.sessions = s
	l.refresh()
	l.cursorToFirstSession()
}

func (l *listPane) Sessions() []store.Session { return l.sessions }

// Len reports the number of sessions on display (ignoring fold state), for
// the header count.
func (l *listPane) Len() int { return l.total }

func (l *listPane) ApplyEnrich(i int, m store.Meta) {
	if i < 0 || i >= len(l.sessions) {
		return
	}
	sel, ok := l.selectedSession()
	l.sessions[i].Apply(m)
	l.refresh()
	if ok {
		l.selectSession(sel)
	}
}

func (l *listPane) SetFilter(q string) {
	l.filter = q
	l.cursor = 0
	l.lineOffset = 0
	l.refresh()
	l.cursorToFirstSession()
}

// SetSearchResults switches the pane to full-text results: sessions in the
// given order, flat, with hit counts on the meta line. nil switches back.
// SetSearchResults takes ownership of hits and may mutate it (RemoveSession);
// callers must not retain or reuse the slice.
func (l *listPane) SetSearchResults(hits []store.SessionHits) {
	l.search = hits
	l.cursor = 0
	l.lineOffset = 0
	l.refresh()
	l.cursorToFirstSession()
}

// searchHits returns the full-text hit count for a session index, 0 when
// search mode is off or the session is not in the results.
func (l *listPane) searchHits(sessionIdx int) int {
	for _, h := range l.search {
		if h.Session == sessionIdx {
			return h.MsgHits
		}
	}
	return 0
}

// ToggleEmpty flips whether "empty" (hook-only) sessions are shown. No-op
// while browsing search results: search mode has its own row set that
// ignores this flag, so toggling it would silently change state that only
// takes visible effect after the user leaves search mode.
func (l *listPane) ToggleEmpty() {
	if l.search != nil {
		return
	}
	sel, ok := l.selectedSession()
	l.showEmpty = !l.showEmpty
	l.refresh()
	if ok {
		l.selectSession(sel)
	}
}

// ToggleGroup switches between project-clustered and flat recency order,
// keeping the current session selected. No-op while browsing search
// results (same reasoning as ToggleEmpty).
func (l *listPane) ToggleGroup() {
	if l.search != nil {
		return
	}
	sel, ok := l.selectedSession()
	l.groupByProject = !l.groupByProject
	l.refresh()
	if ok {
		l.selectSession(sel)
	} else {
		l.cursorToFirstSession()
	}
}

// ToggleFold collapses or expands the project the cursor is currently in
// (whether the cursor sits on the header or one of its sessions), then
// parks the cursor on that project's header. No-op when not grouped.
func (l *listPane) ToggleFold() {
	if !l.grouped() || l.cursor < 0 || l.cursor >= len(l.rows) {
		return
	}
	p := l.rows[l.cursor].project
	if l.folded == nil {
		l.folded = map[string]bool{}
	}
	l.folded[p] = !l.folded[p]
	l.refresh()
	for i, r := range l.rows {
		if r.header && r.project == p {
			l.cursor = i
			break
		}
	}
	l.ensureVisible()
}

// OnHeader reports whether the cursor is on a project header row.
func (l *listPane) OnHeader() bool {
	return l.cursor >= 0 && l.cursor < len(l.rows) && l.rows[l.cursor].header
}

func (l *listPane) MoveCursor(delta int) {
	l.cursor += delta
	if l.cursor < 0 {
		l.cursor = 0
	}
	if l.cursor >= len(l.rows) {
		l.cursor = len(l.rows) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	l.ensureVisible()
}

// RowAtLine maps a visible content line (0 = the first line currently on
// screen) to the row under it, accounting for scroll offset and the mixed
// one-line-header / three-line-session heights. ok is false below the last
// row or above the top.
func (l *listPane) RowAtLine(visible int) (int, bool) {
	if visible < 0 {
		return 0, false
	}
	line := visible + l.lineOffset
	start, total := l.layout()
	if line >= total {
		return 0, false
	}
	for i := len(start) - 1; i >= 0; i-- {
		if line >= start[i] {
			return i, true
		}
	}
	return 0, false
}

// SetCursor moves the cursor to row i, ignoring out-of-range values.
func (l *listPane) SetCursor(i int) {
	if i < 0 || i >= len(l.rows) {
		return
	}
	l.cursor = i
	l.ensureVisible()
}

// Selected returns the session under the cursor. ok is false when the
// cursor is on a header row or the list is empty.
func (l *listPane) Selected() (store.Session, int, bool) {
	if l.cursor < 0 || l.cursor >= len(l.rows) || l.rows[l.cursor].header {
		return store.Session{}, -1, false
	}
	i := l.rows[l.cursor].session
	return l.sessions[i], i, true
}

func (l *listPane) selectedSession() (int, bool) {
	if l.cursor < 0 || l.cursor >= len(l.rows) || l.rows[l.cursor].header {
		return -1, false
	}
	return l.rows[l.cursor].session, true
}

func (l *listPane) selectSession(sessionIdx int) {
	for i, r := range l.rows {
		if !r.header && r.session == sessionIdx {
			l.cursor = i
			l.ensureVisible()
			return
		}
	}
	l.cursorToFirstSession()
}

func (l *listPane) cursorToFirstSession() {
	for i, r := range l.rows {
		if !r.header {
			l.cursor = i
			l.ensureVisible()
			return
		}
	}
	l.cursor = 0
	l.ensureVisible()
}

func (l *listPane) RemoveSession(i int) {
	if i < 0 || i >= len(l.sessions) {
		return
	}
	l.sessions = append(l.sessions[:i], l.sessions[i+1:]...)
	l.refresh()
	if l.search != nil {
		kept := l.search[:0]
		for _, h := range l.search {
			switch {
			case h.Session == i:
				continue
			case h.Session > i:
				h.Session--
			}
			kept = append(kept, h)
		}
		l.search = kept
		l.refresh()
	}
}

func haystack(s store.Session) string {
	return strings.ToLower(s.Title + " " + s.Project() + " " + s.FirstPrompt)
}

// grouped reports whether project headers are shown. Filtering falls back
// to a flat, relevance-ordered list; so does search-results mode (whose
// rows are already flat and never carry header:true, but ToggleFold must
// still stand down explicitly rather than silently folding a project the
// current search-result row happens to belong to).
func (l *listPane) grouped() bool {
	return l.groupByProject && l.filter == "" && l.search == nil
}

func (l *listPane) refresh() {
	// Search-results mode: flat, given order, suppresses filter/grouping.
	if l.search != nil {
		l.rows = l.rows[:0]
		l.counts = map[string]int{}
		l.total = 0
		for _, h := range l.search {
			if h.Session < 0 || h.Session >= len(l.sessions) {
				continue
			}
			l.total++
			l.rows = append(l.rows, row{project: l.sessions[h.Session].Project(), session: h.Session})
		}
		if l.cursor >= len(l.rows) {
			l.cursor = len(l.rows) - 1
		}
		if l.cursor < 0 {
			l.cursor = 0
		}
		l.ensureVisible()
		return
	}

	// 1. Select sessions (recency order, honoring the empty toggle / filter).
	var base []int
	if l.filter == "" {
		for i, s := range l.sessions {
			if s.Empty() && !l.showEmpty {
				continue
			}
			base = append(base, i)
		}
	} else {
		targets := make([]string, len(l.sessions))
		for i, s := range l.sessions {
			targets[i] = haystack(s)
		}
		for _, m := range fuzzy.Find(strings.ToLower(l.filter), targets) {
			s := l.sessions[m.Index]
			if s.Empty() && !l.showEmpty {
				continue
			}
			base = append(base, m.Index)
		}
	}
	l.total = len(base)

	// 2. Count per project.
	l.counts = map[string]int{}
	for _, si := range base {
		l.counts[l.sessions[si].Project()]++
	}

	// 3. Build rows. Grouped: header per project (first-appearance order,
	//    i.e. most-recent project first since base is recency-sorted), then
	//    that project's sessions unless folded. Flat: sessions only.
	l.rows = l.rows[:0]
	if l.grouped() {
		order := []string{}
		buckets := map[string][]int{}
		for _, si := range base {
			p := l.sessions[si].Project()
			if _, seen := buckets[p]; !seen {
				order = append(order, p)
			}
			buckets[p] = append(buckets[p], si)
		}
		for _, p := range order {
			l.rows = append(l.rows, row{header: true, project: p})
			if l.folded[p] {
				continue
			}
			for _, si := range buckets[p] {
				l.rows = append(l.rows, row{project: p, session: si})
			}
		}
	} else {
		for _, si := range base {
			l.rows = append(l.rows, row{project: l.sessions[si].Project(), session: si})
		}
	}

	if l.cursor >= len(l.rows) {
		l.cursor = len(l.rows) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	l.ensureVisible()
}

// layout returns the terminal line each row starts on and the total lines.
func (l *listPane) layout() (start []int, total int) {
	start = make([]int, len(l.rows))
	pos := 0
	for i, r := range l.rows {
		start[i] = pos
		if r.header {
			pos++
		} else {
			pos += sessionLines
		}
	}
	return start, pos
}

// ensureVisible scrolls the minimum amount to keep the selected row inside
// the viewport.
func (l *listPane) ensureVisible() {
	if len(l.rows) == 0 || l.height <= 0 {
		l.lineOffset = 0
		return
	}
	start, total := l.layout()
	top := start[l.cursor]
	bottom := top
	if !l.rows[l.cursor].header {
		bottom = top + 1 // through the meta line
	}
	if top < l.lineOffset {
		l.lineOffset = top
	}
	if bottom >= l.lineOffset+l.height {
		l.lineOffset = bottom - l.height + 1
	}
	if maxOff := total - l.height; l.lineOffset > maxOff {
		l.lineOffset = maxOff
	}
	if l.lineOffset < 0 {
		l.lineOffset = 0
	}
}

func (l *listPane) View() string {
	if l.total == 0 {
		if l.search != nil || l.filter != "" {
			return l.styles.ListMeta.Render("no matches")
		}
		return l.styles.ListMeta.Render("no sessions")
	}

	var lines []string
	for i, r := range l.rows {
		if r.header {
			indicator := "▾"
			if l.folded[r.project] {
				indicator = "▸"
			}
			name := fmt.Sprintf("%s %s", indicator, r.project)
			count := fmt.Sprintf("(%d)", l.counts[r.project])
			label := store.Truncate(name+" "+count, l.width)
			style := l.styles.GroupHeader
			if i == l.cursor {
				style = l.styles.GroupHeaderSel
			}
			// Split the name from the "(n)" count so they can carry
			// different styles while the rendered text stays byte-identical
			// to the un-split label. If truncation ate into (or removed)
			// the count suffix, fall back to a single style for the whole
			// (already-truncated) string — still exactly the same visible
			// characters, just without the count's distinct color.
			rendered := style.Render(label)
			if suffix := " " + count; strings.HasSuffix(label, suffix) {
				rendered = style.Render(label[:len(label)-len(suffix)]) + " " + l.styles.GroupCount.Render(count)
			}
			lines = append(lines, rendered)
			continue
		}

		s := l.sessions[r.session]
		title := s.Title
		if title == "" {
			if s.Enriched {
				title = "(untitled)"
			} else {
				title = s.ID
			}
		}
		meta := s.Project() + " · " + humanTime(s.LastActivity, time.Now())
		if s.GitBranch != "" {
			meta += " · " + s.GitBranch
		}
		if s.Unreadable {
			meta += " · (unreadable)"
		}
		if n := l.searchHits(r.session); n > 0 {
			meta += " · " + fmt.Sprintf("%d hit", n)
			if n != 1 {
				meta += "s"
			}
		}
		prefix := "  "
		titleStyle, metaStyle := l.styles.ListTitle, l.styles.ListMeta
		if i == l.cursor {
			prefix = "▶ "
			titleStyle, metaStyle = l.styles.ListTitleSel, l.styles.ListMetaSel
		}
		lines = append(lines,
			titleStyle.Render(store.Truncate(prefix+title, l.width)),
			metaStyle.Render(store.Truncate("  "+meta, l.width)),
			"")
	}

	// Window to [lineOffset, lineOffset+height).
	start := l.lineOffset
	if start > len(lines) {
		start = len(lines)
	}
	end := start + l.height
	if l.height <= 0 || end > len(lines) {
		end = len(lines)
	}
	return strings.TrimRight(strings.Join(lines[start:end], "\n"), "\n")
}
