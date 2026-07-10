package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/sahilm/fuzzy"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

// sessionLines is the number of terminal rows one session occupies:
// title, meta, blank separator.
const sessionLines = 3

// listPane is a hand-rolled scrolling session list. Sessions render as
// three rows (title, meta, blank); in group-by-project mode a one-row
// header precedes each project's first session. Scrolling is tracked in
// terminal lines so headers don't throw the window off.
type listPane struct {
	sessions       []store.Session
	visible        []int          // indexes into sessions, in display order
	counts         map[string]int // visible session count per project label
	cursor         int            // index into visible
	lineOffset     int            // first visible terminal line (scrolling)
	width          int
	height         int
	filter         string
	showEmpty      bool
	groupByProject bool
	focused        bool
	styles         styles
}

func (l *listPane) SetSize(w, h int) {
	l.width, l.height = w, h
	l.ensureVisible()
}

func (l *listPane) SetSessions(s []store.Session) {
	l.sessions = s
	l.refresh()
}

func (l *listPane) Sessions() []store.Session { return l.sessions }
func (l *listPane) Len() int                  { return len(l.visible) }

func (l *listPane) ApplyEnrich(i int, m store.Meta) {
	if i < 0 || i >= len(l.sessions) {
		return
	}
	l.sessions[i].Apply(m)
	l.refresh()
}

func (l *listPane) SetFilter(q string) {
	l.filter = q
	l.cursor = 0
	l.lineOffset = 0
	l.refresh()
}

func (l *listPane) ToggleEmpty() {
	l.showEmpty = !l.showEmpty
	l.refresh()
}

// ToggleGroup switches between project-clustered and flat recency order.
func (l *listPane) ToggleGroup() {
	l.groupByProject = !l.groupByProject
	l.refresh()
}

func (l *listPane) MoveCursor(delta int) {
	l.cursor += delta
	if l.cursor < 0 {
		l.cursor = 0
	}
	if l.cursor >= len(l.visible) {
		l.cursor = len(l.visible) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	l.ensureVisible()
}

func (l *listPane) Selected() (store.Session, int, bool) {
	if len(l.visible) == 0 {
		return store.Session{}, -1, false
	}
	i := l.visible[l.cursor]
	return l.sessions[i], i, true
}

func (l *listPane) RemoveSession(i int) {
	if i < 0 || i >= len(l.sessions) {
		return
	}
	l.sessions = append(l.sessions[:i], l.sessions[i+1:]...)
	l.refresh()
}

func haystack(s store.Session) string {
	return strings.ToLower(s.Title + " " + s.Project() + " " + s.FirstPrompt)
}

// grouped reports whether project headers are shown. Filtering falls
// back to a flat, relevance-ordered list.
func (l *listPane) grouped() bool {
	return l.groupByProject && l.filter == ""
}

func (l *listPane) refresh() {
	// 1. Select sessions (recency order, honoring the empty toggle / filter).
	base := l.visible[:0]
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

	// 2. Count per project and, when grouping, cluster by project. Projects
	//    keep first-appearance order — since base is recency-sorted, that is
	//    most-recent project first, sessions within a project by recency.
	l.counts = map[string]int{}
	for _, si := range base {
		l.counts[l.sessions[si].Project()]++
	}
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
		clustered := base[:0]
		for _, p := range order {
			clustered = append(clustered, buckets[p]...)
		}
		l.visible = clustered
	} else {
		l.visible = base
	}

	if l.cursor >= len(l.visible) {
		l.cursor = len(l.visible) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	l.ensureVisible()
}

// layout returns, for each visible row, the terminal line its block starts
// on, whether a group header precedes it, and the total line count.
func (l *listPane) layout() (start []int, header []bool, total int) {
	start = make([]int, len(l.visible))
	header = make([]bool, len(l.visible))
	pos := 0
	prev := ""
	for r, si := range l.visible {
		p := l.sessions[si].Project()
		if l.grouped() && p != prev {
			header[r] = true
			pos++ // header line
			prev = p
		}
		start[r] = pos
		pos += sessionLines
	}
	return start, header, pos
}

// ensureVisible scrolls the minimum amount to keep the selected session
// (and its header, when it starts a group) inside the viewport.
func (l *listPane) ensureVisible() {
	if len(l.visible) == 0 || l.height <= 0 {
		l.lineOffset = 0
		return
	}
	start, header, total := l.layout()
	top := start[l.cursor]
	if header[l.cursor] {
		top-- // pull the group header into view too
	}
	bottom := start[l.cursor] + sessionLines - 2 // through the meta line
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
	if len(l.visible) == 0 {
		if l.filter != "" {
			return l.styles.ListMeta.Render("no matches")
		}
		return l.styles.ListMeta.Render("no sessions")
	}

	var lines []string
	prev := ""
	for row, si := range l.visible {
		s := l.sessions[si]
		if l.grouped() && s.Project() != prev {
			label := fmt.Sprintf("▸ %s (%d)", s.Project(), l.counts[s.Project()])
			lines = append(lines, l.styles.GroupHeader.Render(store.Truncate(label, l.width)))
			prev = s.Project()
		}

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
		prefix := "  "
		titleStyle, metaStyle := l.styles.ListTitle, l.styles.ListMeta
		if row == l.cursor {
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
