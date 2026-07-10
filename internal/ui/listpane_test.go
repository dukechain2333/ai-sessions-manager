package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

func testSessions() []store.Session {
	return []store.Session{
		{ID: "s1", Slug: "-p1", CWD: "/x/alpha", Title: "Create slides from notes", FirstPrompt: "make slides", UserMessages: 3, Enriched: true, LastActivity: time.Now()},
		{ID: "s2", Slug: "-p2", CWD: "/x/beta", Title: "Fix backup script", FirstPrompt: "backup is broken", UserMessages: 2, Enriched: true, LastActivity: time.Now().Add(-time.Hour)},
		{ID: "s3", Slug: "-p3", CWD: "/x/gamma", Title: "", FirstPrompt: "", UserMessages: 0, Enriched: true, LastActivity: time.Now().Add(-2 * time.Hour)},
	}
}

func newTestPane() listPane {
	l := listPane{styles: defaultStyles()}
	l.SetSize(50, 30)
	l.SetSessions(testSessions())
	return l
}

func TestListFilter(t *testing.T) {
	l := newTestPane()
	if got := len(l.visible); got != 2 {
		t.Fatalf("visible = %d, want 2 (empty session hidden)", got)
	}
	l.SetFilter("backup")
	s, _, ok := l.Selected()
	if !ok || s.ID != "s2" {
		t.Errorf("filter 'backup' selected %v", s.ID)
	}
	l.SetFilter("")
	if got := len(l.visible); got != 2 {
		t.Errorf("clearing filter: visible = %d, want 2", got)
	}
}

func TestListToggleEmpty(t *testing.T) {
	l := newTestPane()
	l.ToggleEmpty()
	if got := len(l.visible); got != 3 {
		t.Errorf("visible = %d, want 3 after ToggleEmpty", got)
	}
}

func TestListCursorAndRemove(t *testing.T) {
	l := newTestPane()
	l.MoveCursor(1)
	s, idx, _ := l.Selected()
	if s.ID != "s2" {
		t.Fatalf("cursor at %s, want s2", s.ID)
	}
	l.MoveCursor(5) // clamps at end
	if _, _, ok := l.Selected(); !ok {
		t.Fatal("selection lost after clamp")
	}
	l.RemoveSession(idx)
	for _, s := range l.Sessions() {
		if s.ID == "s2" {
			t.Error("s2 still present after RemoveSession")
		}
	}
}

func TestListView(t *testing.T) {
	l := newTestPane()
	v := l.View()
	if !strings.Contains(v, "Create slides from notes") {
		t.Errorf("view missing title:\n%s", v)
	}
	if !strings.Contains(v, "alpha") {
		t.Errorf("view missing project label:\n%s", v)
	}
}

func TestListScroll(t *testing.T) {
	titles := []string{
		"Alpha task", "Bravo task", "Charlie task",
		"Delta task", "Echo task", "Foxtrot task",
	}
	sessions := make([]store.Session, len(titles))
	for i, title := range titles {
		sessions[i] = store.Session{
			ID:           "s" + string(rune('1'+i)),
			Slug:         "-p",
			CWD:          "/x/proj",
			Title:        title,
			UserMessages: 1,
			Enriched:     true,
			LastActivity: time.Now().Add(-time.Duration(i) * time.Hour),
		}
	}
	l := listPane{styles: defaultStyles()}
	l.SetSize(50, 9) // maxItems() == 3
	l.SetSessions(sessions)

	l.MoveCursor(5)
	s, _, ok := l.Selected()
	if !ok || s.ID != "s6" {
		t.Fatalf("Selected() = %v (ok=%v), want s6", s.ID, ok)
	}
	v := l.View()
	if !strings.Contains(v, "Foxtrot task") {
		t.Errorf("view missing s6 title after scroll:\n%s", v)
	}
	if strings.Contains(v, "Alpha task") {
		t.Errorf("view still shows s1 title after scroll:\n%s", v)
	}

	l.MoveCursor(-5)
	if v := l.View(); !strings.Contains(v, "Alpha task") {
		t.Errorf("view missing s1 title after scrolling back:\n%s", v)
	}
}

func groupedSessions() []store.Session {
	// beta is the most-recent project; alpha has two sessions.
	return []store.Session{
		{ID: "b1", CWD: "/x/beta", Title: "Beta newest", UserMessages: 1, Enriched: true, LastActivity: time.Now()},
		{ID: "a1", CWD: "/x/alpha", Title: "Alpha newer", UserMessages: 1, Enriched: true, LastActivity: time.Now().Add(-time.Hour)},
		{ID: "a2", CWD: "/x/alpha", Title: "Alpha older", UserMessages: 1, Enriched: true, LastActivity: time.Now().Add(-3 * time.Hour)},
	}
}

func TestListGroupingHeadersAndClustering(t *testing.T) {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(60, 40)
	l.SetSessions(groupedSessions())

	// visible clustered by project, most-recent project first, recency within.
	wantOrder := []string{"b1", "a1", "a2"}
	for i, id := range wantOrder {
		if got := l.sessions[l.visible[i]].ID; got != id {
			t.Errorf("visible[%d] = %s, want %s", i, got, id)
		}
	}

	v := l.View()
	if !strings.Contains(v, "▸ beta (1)") {
		t.Errorf("view missing beta header:\n%s", v)
	}
	if !strings.Contains(v, "▸ alpha (2)") {
		t.Errorf("view missing alpha header with count 2:\n%s", v)
	}
	// beta header must appear before alpha header.
	if strings.Index(v, "beta (1)") > strings.Index(v, "alpha (2)") {
		t.Errorf("beta group should precede alpha group:\n%s", v)
	}
}

func TestGroupToggle(t *testing.T) {
	l := listPane{styles: defaultStyles()}
	l.SetSize(60, 40)
	l.SetSessions(groupedSessions())
	if strings.Contains(l.View(), "▸ ") {
		t.Error("flat view should have no group headers")
	}
	l.ToggleGroup()
	if !strings.Contains(l.View(), "▸ ") {
		t.Error("grouped view should show headers after ToggleGroup")
	}
	l.ToggleGroup()
	if strings.Contains(l.View(), "▸ ") {
		t.Error("headers should disappear after toggling group off")
	}
}

func TestGroupedFilterFallsBackToFlat(t *testing.T) {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(60, 40)
	l.SetSessions(groupedSessions())
	l.SetFilter("alpha")
	if strings.Contains(l.View(), "▸ ") {
		t.Errorf("filtered view should be flat (no headers):\n%s", l.View())
	}
}

func TestListViewEmptyStates(t *testing.T) {
	var empty listPane
	empty.styles = defaultStyles()
	empty.SetSize(50, 30)
	empty.SetSessions(nil)
	if v := empty.View(); !strings.Contains(v, "no sessions") {
		t.Errorf("empty pane view = %q, want it to contain %q", v, "no sessions")
	}

	l := newTestPane()
	l.SetFilter("zzzznomatch")
	if v := l.View(); !strings.Contains(v, "no matches") {
		t.Errorf("filtered-out view = %q, want it to contain %q", v, "no matches")
	}
}

func TestHumanTime(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		t    time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-48 * time.Hour), "2d ago"},
		{now.Add(-90 * 24 * time.Hour), "Apr 11 2026"},
	}
	for _, c := range cases {
		if got := humanTime(c.t, now); got != c.want {
			t.Errorf("humanTime(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}
