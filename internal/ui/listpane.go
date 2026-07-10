package ui

import (
	"strings"
	"time"

	"github.com/sahilm/fuzzy"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

// listPane is a hand-rolled scrolling session list: three terminal
// rows per item (title, meta, blank), fuzzy-filterable.
type listPane struct {
	sessions  []store.Session
	visible   []int // indexes into sessions, in display order
	cursor    int   // index into visible
	offset    int   // first visible row index (scrolling)
	width     int
	height    int
	filter    string
	showEmpty bool
	focused   bool
	styles    styles
}

func (l *listPane) SetSize(w, h int) {
	l.width, l.height = w, h
	l.clampScroll()
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
	l.offset = 0
	l.refresh()
}

func (l *listPane) ToggleEmpty() {
	l.showEmpty = !l.showEmpty
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
	l.clampScroll()
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

func (l *listPane) refresh() {
	l.visible = l.visible[:0]
	if l.filter == "" {
		for i, s := range l.sessions {
			if s.Empty() && !l.showEmpty {
				continue
			}
			l.visible = append(l.visible, i)
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
			l.visible = append(l.visible, m.Index)
		}
	}
	if l.cursor >= len(l.visible) {
		l.cursor = len(l.visible) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	l.clampScroll()
}

func (l *listPane) maxItems() int {
	n := l.height / 3
	if n < 1 {
		n = 1
	}
	return n
}

func (l *listPane) clampScroll() {
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+l.maxItems() {
		l.offset = l.cursor - l.maxItems() + 1
	}
	if l.offset < 0 {
		l.offset = 0
	}
}

func (l *listPane) View() string {
	if len(l.visible) == 0 {
		if l.filter != "" {
			return l.styles.ListMeta.Render("no matches")
		}
		return l.styles.ListMeta.Render("no sessions")
	}
	end := l.offset + l.maxItems()
	if end > len(l.visible) {
		end = len(l.visible)
	}
	var b strings.Builder
	for row := l.offset; row < end; row++ {
		s := l.sessions[l.visible[row]]
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
		b.WriteString(titleStyle.Render(store.Truncate(prefix+title, l.width)) + "\n")
		b.WriteString(metaStyle.Render(store.Truncate("  "+meta, l.width)) + "\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
