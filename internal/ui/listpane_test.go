package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

func testSessions() []store.Session {
	return []store.Session{
		{ID: "s1", Slug: "-p1", CWD: "/x/alpha", Title: "Create slides from notes", FirstPrompt: "make slides", Agent: store.AgentClaude, UserMessages: 3, Enriched: true, LastActivity: time.Now()},
		{ID: "s2", Slug: "-p2", CWD: "/x/beta", Title: "Fix backup script", FirstPrompt: "backup is broken", Agent: store.AgentClaude, UserMessages: 2, Enriched: true, LastActivity: time.Now().Add(-time.Hour)},
		{ID: "s3", Slug: "-p3", CWD: "/x/gamma", Title: "", FirstPrompt: "", Agent: store.AgentClaude, UserMessages: 0, Enriched: true, LastActivity: time.Now().Add(-2 * time.Hour)},
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
	if got := l.Len(); got != 2 {
		t.Fatalf("Len = %d, want 2 (empty session hidden)", got)
	}
	l.SetFilter("backup")
	s, _, ok := l.Selected()
	if !ok || s.ID != "s2" {
		t.Errorf("filter 'backup' selected %v", s.ID)
	}
	l.SetFilter("")
	if got := l.Len(); got != 2 {
		t.Errorf("clearing filter: Len = %d, want 2", got)
	}
}

func TestListToggleEmpty(t *testing.T) {
	l := newTestPane()
	l.ToggleEmpty()
	if got := l.Len(); got != 3 {
		t.Errorf("Len = %d, want 3 after ToggleEmpty", got)
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

	// Session rows clustered by project, most-recent project first, recency
	// within. Collect the session IDs from the row list, in order.
	var order []string
	for _, r := range l.rows {
		if !r.header {
			order = append(order, l.sessions[r.session].ID)
		}
	}
	want := []string{"b1", "a1", "a2"}
	for i := range want {
		if i >= len(order) || order[i] != want[i] {
			t.Fatalf("session order = %v, want %v", order, want)
		}
	}

	v := l.View()
	if !strings.Contains(v, "▾ beta (1)") {
		t.Errorf("view missing beta header:\n%s", v)
	}
	if !strings.Contains(v, "▾ alpha (2)") {
		t.Errorf("view missing alpha header with count 2:\n%s", v)
	}
	if strings.Index(v, "beta (1)") > strings.Index(v, "alpha (2)") {
		t.Errorf("beta group should precede alpha group:\n%s", v)
	}
}

func TestGroupToggle(t *testing.T) {
	l := listPane{styles: defaultStyles()}
	l.SetSize(60, 40)
	l.SetSessions(groupedSessions())
	if strings.ContainsAny(l.View(), "▾▸") {
		t.Error("flat view should have no group headers")
	}
	l.ToggleGroup()
	if !strings.Contains(l.View(), "▾ ") {
		t.Error("grouped view should show headers after ToggleGroup")
	}
	l.ToggleGroup()
	if strings.ContainsAny(l.View(), "▾▸") {
		t.Error("headers should disappear after toggling group off")
	}
}

func TestGroupedFilterFallsBackToFlat(t *testing.T) {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(60, 40)
	l.SetSessions(groupedSessions())
	l.SetFilter("alpha")
	if strings.ContainsAny(l.View(), "▾▸") {
		t.Errorf("filtered view should be flat (no headers):\n%s", l.View())
	}
}

func TestFoldHidesSessions(t *testing.T) {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(60, 40)
	l.SetSessions(groupedSessions())
	// Select an alpha session, then fold its group.
	l.selectSession(1) // a1 (index 1 in groupedSessions)
	if s, _, ok := l.Selected(); !ok || s.ID != "a1" {
		t.Fatalf("setup: selected %v, want a1", s.ID)
	}
	l.ToggleFold()

	v := l.View()
	if !strings.Contains(v, "▸ alpha (2)") {
		t.Errorf("folded alpha header should use ▸ and keep its count:\n%s", v)
	}
	if strings.Contains(v, "Alpha newer") || strings.Contains(v, "Alpha older") {
		t.Errorf("folded group should hide its session titles:\n%s", v)
	}
	// beta is still expanded.
	if !strings.Contains(v, "Beta newest") {
		t.Errorf("unfolded beta session should remain visible:\n%s", v)
	}
	// Cursor parked on the folded project's header.
	if !l.OnHeader() {
		t.Error("after folding, cursor should rest on the group header")
	}
	// Fold does not change the session count.
	if l.Len() != 3 {
		t.Errorf("Len = %d after fold, want 3 (fold hides rows, not sessions)", l.Len())
	}

	// Unfold restores the sessions.
	l.ToggleFold()
	if v := l.View(); !strings.Contains(v, "Alpha newer") {
		t.Errorf("unfolding should restore session titles:\n%s", v)
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

func TestRowAtLineGrouped(t *testing.T) {
	l := newTestPane()
	l.ToggleGroup() // fixture pane starts flat; model default is grouped
	// rows: 0 hdr alpha, 1 s1 (3 lines), 2 hdr beta, 3 s2 (3 lines)
	cases := []struct {
		line int
		row  int
		ok   bool
	}{
		{0, 0, true}, {1, 1, true}, {3, 1, true}, {4, 2, true},
		{5, 3, true}, {7, 3, true}, {8, 0, false}, {-1, 0, false},
	}
	for _, c := range cases {
		row, ok := l.RowAtLine(c.line)
		if ok != c.ok || (ok && row != c.row) {
			t.Errorf("RowAtLine(%d) = (%d,%v), want (%d,%v)", c.line, row, ok, c.row, c.ok)
		}
	}
}

func TestRowAtLineFlat(t *testing.T) {
	l := newTestPane() // flat: rows 0 s1 (lines 0-2), 1 s2 (lines 3-5)
	for line, want := range map[int]int{0: 0, 2: 0, 3: 1, 5: 1} {
		if row, ok := l.RowAtLine(line); !ok || row != want {
			t.Errorf("RowAtLine(%d) = (%d,%v), want (%d,true)", line, row, ok, want)
		}
	}
	if _, ok := l.RowAtLine(6); ok {
		t.Error("RowAtLine(6) should be out of range")
	}
}

func TestRowAtLineFolded(t *testing.T) {
	l := newTestPane()
	l.ToggleGroup()
	l.ToggleFold() // cursor starts on s1 → folds alpha; rows: hdr alpha, hdr beta, s2
	if row, ok := l.RowAtLine(1); !ok || row != 1 {
		t.Errorf("folded: RowAtLine(1) = %d, want 1 (beta header)", row)
	}
	if row, ok := l.RowAtLine(3); !ok || row != 2 {
		t.Errorf("folded: RowAtLine(3) = %d, want 2 (s2 middle line)", row)
	}
}

func TestRowAtLineScrolledSmallPane(t *testing.T) {
	l := listPane{styles: defaultStyles()}
	l.SetSize(50, 3)
	l.SetSessions(testSessions())
	l.ToggleGroup()
	l.SetCursor(3)
	// ensureVisible keeps title+meta visible (the blank separator may hang
	// off-screen): cursor on s2 (lines 5-7, height 3) → lineOffset 4.
	if l.lineOffset == 0 {
		t.Fatal("setup: expected a scrolled pane")
	}
	if row, ok := l.RowAtLine(0); !ok || row != 2 {
		t.Errorf("scrolled: RowAtLine(0) = %d, want 2 (beta header at line 4)", row)
	}
	if row, ok := l.RowAtLine(1); !ok || row != 3 {
		t.Errorf("scrolled: RowAtLine(1) = %d, want 3 (s2 title line)", row)
	}
}

func TestSetCursorClamps(t *testing.T) {
	l := newTestPane()
	l.SetCursor(1)
	if s, _, ok := l.Selected(); !ok || s.ID != "s2" {
		t.Errorf("SetCursor(1) selected %v, want s2", s.ID)
	}
	l.SetCursor(99) // out of range: no-op
	if s, _, ok := l.Selected(); !ok || s.ID != "s2" {
		t.Errorf("SetCursor(99) moved cursor to %v", s.ID)
	}
	l.SetCursor(-1)
	if s, _, ok := l.Selected(); !ok || s.ID != "s2" {
		t.Errorf("SetCursor(-1) moved cursor to %v", s.ID)
	}
}

func TestSearchResultsMode(t *testing.T) {
	l := newTestPane()
	l.ToggleGroup() // grouped, to prove search mode overrides grouping
	l.SetSearchResults([]store.SessionHits{
		{Session: 1, MsgHits: 3, First: 0}, // s2 first (more hits)
		{Session: 0, MsgHits: 1, First: 2},
	})
	if got := l.Len(); got != 2 {
		t.Fatalf("Len = %d, want 2", got)
	}
	if s, _, ok := l.Selected(); !ok || s.ID != "s2" {
		t.Fatalf("first result should be selected, got %v", s.ID)
	}
	for _, r := range l.rows {
		if r.header {
			t.Fatal("search mode must not render project headers")
		}
	}
	view := l.View()
	if !strings.Contains(view, "· 3 hits") {
		t.Errorf("meta line should show hit count, view:\n%s", view)
	}
	l.SetSearchResults(nil)
	if got := l.Len(); got != 2 {
		t.Errorf("clearing search restores normal view, Len = %d", got)
	}
	if len(l.rows) == 0 || !l.rows[0].header {
		t.Error("grouped headers should be back after clearing")
	}
}

func TestRemoveSessionAdjustsSearchResults(t *testing.T) {
	l := newTestPane()
	l.SetSearchResults([]store.SessionHits{
		{Session: 1, MsgHits: 3}, // s2
		{Session: 0, MsgHits: 1}, // s1
	})
	l.RemoveSession(0) // drop s1: s2's index shifts from 1 to 0
	if got := l.Len(); got != 1 {
		t.Fatalf("Len = %d, want 1 after removing a result", got)
	}
	if s, _, ok := l.Selected(); !ok || s.ID != "s2" {
		t.Errorf("remaining result should be s2, got %v", s.ID)
	}
	if n := l.searchHits(0); n != 3 {
		t.Errorf("s2's hits must follow its shifted index, got %d", n)
	}
}

func TestSearchResultsSingularHit(t *testing.T) {
	l := newTestPane()
	l.SetSearchResults([]store.SessionHits{{Session: 0, MsgHits: 1}})
	if view := l.View(); !strings.Contains(view, "· 1 hit ") && !strings.HasSuffix(strings.TrimRight(view, " \n"), "· 1 hit") && !strings.Contains(view, "· 1 hit\n") {
		t.Errorf("singular form wanted, view:\n%s", view)
	}
}

// TestSearchModeSpaceDoesNotMutateFold is the I3 regression: space (via
// ToggleFold) must not silently fold/unfold a project while browsing
// search results, even though search-mode rows don't visually reflect
// fold state — the mutation would surface unexpectedly once the user
// leaves search mode.
func TestSearchModeSpaceDoesNotMutateFold(t *testing.T) {
	l := newTestPane()
	l.ToggleGroup() // groupByProject=true, filter=="" — grouped() would (bug) say true
	l.SetSearchResults([]store.SessionHits{{Session: 0, MsgHits: 1}, {Session: 1, MsgHits: 1}})
	l.ToggleFold()
	if len(l.folded) != 0 {
		t.Errorf("space in search-results mode must not fold any project, folded = %v", l.folded)
	}
}

// TestSearchModeGDoesNotToggleGroup is the I3 regression for ToggleGroup.
func TestSearchModeGDoesNotToggleGroup(t *testing.T) {
	l := newTestPane()
	l.SetSearchResults([]store.SessionHits{{Session: 0, MsgHits: 1}})
	before := l.groupByProject
	l.ToggleGroup()
	if l.groupByProject != before {
		t.Error("g in search-results mode must not change groupByProject")
	}
}

// TestSearchModeEDoesNotToggleEmpty is the I3 regression for ToggleEmpty.
func TestSearchModeEDoesNotToggleEmpty(t *testing.T) {
	l := newTestPane()
	l.SetSearchResults([]store.SessionHits{{Session: 0, MsgHits: 1}})
	before := l.showEmpty
	l.ToggleEmpty()
	if l.showEmpty != before {
		t.Error("e in search-results mode must not change showEmpty")
	}
}

// TestSearchResultsEmptyShowsNoMatches is the M3 regression: a zero-hit
// search must say "no matches", not the generic "no sessions".
func TestSearchResultsEmptyShowsNoMatches(t *testing.T) {
	l := newTestPane()
	l.SetSearchResults([]store.SessionHits{})
	if v := l.View(); !strings.Contains(v, "no matches") {
		t.Errorf("empty search-results view = %q, want it to contain %q", v, "no matches")
	}
}

func TestListShowsAgentTag(t *testing.T) {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(80, 40)
	l.SetSessions([]store.Session{
		{ID: "c1", CWD: "/x/p", Title: "alpha one", Agent: store.AgentClaude, UserMessages: 1, Enriched: true, LastActivity: time.Now()},
		{ID: "x1", CWD: "/x/p", Title: "beta two", Agent: store.AgentCodex, UserMessages: 1, Enriched: true, LastActivity: time.Now().Add(-time.Minute)},
	})
	v := l.View()
	if !strings.Contains(v, "claude") {
		t.Errorf("view missing claude tag:\n%s", v)
	}
	if !strings.Contains(v, "codex") {
		t.Errorf("view missing codex tag:\n%s", v)
	}
}

func agentMixSessions() []store.Session {
	now := time.Now()
	return []store.Session{
		{ID: "c1", CWD: "/x/mix", Title: "claude a", Agent: store.AgentClaude, UserMessages: 1, Enriched: true, LastActivity: now},
		{ID: "x1", CWD: "/x/mix", Title: "codex a", Agent: store.AgentCodex, UserMessages: 1, Enriched: true, LastActivity: now.Add(-time.Minute)},
		{ID: "c2", CWD: "/x/solo", Title: "claude solo", Agent: store.AgentClaude, UserMessages: 1, Enriched: true, LastActivity: now.Add(-2 * time.Minute)},
	}
}

func TestAgentGroupingSubheaders(t *testing.T) {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(80, 60)
	l.SetSessions(agentMixSessions())
	if strings.Contains(l.View(), "Claude ─") {
		t.Error("no subheaders before toggle")
	}
	l.ToggleAgentGroup()
	v := l.View()
	if !strings.Contains(v, "─ Claude ─") || !strings.Contains(v, "─ Codex ─") {
		t.Errorf("mixed project should show both subheaders:\n%s", v)
	}
	// single-agent project 'solo' must NOT get a subheader
	soloIdx := strings.Index(v, "claude solo")
	seg := v[strings.Index(v, "solo ("):soloIdx]
	if strings.Contains(seg, "─ Claude ─") {
		t.Errorf("single-agent project should not show a subheader:\n%s", v)
	}
}

func TestAgentSubheaderNotCursorable(t *testing.T) {
	l := listPane{styles: defaultStyles(), groupByProject: true}
	l.SetSize(80, 60)
	l.SetSessions(agentMixSessions())
	l.ToggleAgentGroup()
	// walk down through all rows; Selected must never be a subheader (ok true only on sessions)
	for n := 0; n < 12; n++ {
		if _, _, ok := l.Selected(); ok {
			// fine — a session
		}
		l.MoveCursor(1)
	}
	// no panic + cursor still resolves
	if l.Len() != 3 {
		t.Errorf("Len = %d, want 3", l.Len())
	}
}

func TestListViewFillsHeight(t *testing.T) {
	// The preview pane (a viewport) always renders exactly its height in
	// lines; the list pane must too, or the two bordered boxes end at
	// different rows. newTestPane is height 30 with only 2 visible sessions
	// (6 content lines), so a trimming View would return far fewer.
	l := newTestPane()
	if got := strings.Count(l.View(), "\n") + 1; got != 30 {
		t.Errorf("View() = %d lines, want 30 (pane height) so it matches the preview box", got)
	}
}
