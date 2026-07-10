package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func wheel(x, y int, up bool) tea.MouseMsg {
	b := tea.MouseButtonWheelDown
	if up {
		b = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b}
}

func TestZoneAt(t *testing.T) {
	m := newTestModel() // 100x30: listW=40, bodyH=25
	cases := []struct {
		name string
		x, y int
		z    zone
		line int
	}{
		{"filter row", 5, 1, zoneFilter, 0},
		{"help row", 50, 29, zoneHelp, 0},
		{"title row", 5, 0, zoneNone, 0},
		{"body top border", 5, 2, zoneNone, 0},
		{"body bottom border", 5, 28, zoneNone, 0},
		{"list first line", 5, 3, zoneList, 0},
		{"list last line", 38, 27, zoneList, 24},
		{"list left border", 0, 5, zoneNone, 0},
		{"list right border", 39, 5, zoneNone, 0},
		{"preview left border", 40, 5, zoneNone, 0},
		{"preview first col", 41, 3, zonePreview, 0},
		{"preview last col", 98, 10, zonePreview, 7},
		{"preview right border", 99, 10, zoneNone, 0},
	}
	for _, c := range cases {
		z, line := m.zoneAt(c.x, c.y)
		if z != c.z || line != c.line {
			t.Errorf("%s: zoneAt(%d,%d) = (%v,%d), want (%v,%d)", c.name, c.x, c.y, z, line, c.z, c.line)
		}
	}
}

func TestZoneAtNarrow(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 30}) // narrow: listW=68
	m = m2.(Model)
	if z, _ := m.zoneAt(50, 5); z != zoneList {
		t.Errorf("narrow: (50,5) = %v, want zoneList", z)
	}
	if z, _ := m.zoneAt(66, 5); z != zoneList {
		t.Errorf("narrow: (66,5) = %v, want zoneList (last content col)", z)
	}
	if z, _ := m.zoneAt(67, 5); z != zoneNone {
		t.Errorf("narrow: (67,5) = %v, want zoneNone (border)", z)
	}
	if z, _ := m.zoneAt(69, 5); z != zoneNone {
		t.Errorf("narrow: (69,5) = %v, want zoneNone (no preview pane)", z)
	}
}

func TestClickSelectsSession(t *testing.T) {
	m := newTestModel()
	m2, cmd := m.Update(click(5, 8)) // s2's title line
	m = m2.(Model)
	if s, _, ok := m.list.Selected(); !ok || s.ID != "s2" {
		t.Fatalf("selected %v, want s2", s.ID)
	}
	if cmd == nil {
		t.Error("selecting must reload the preview")
	}
}

func TestClickHeaderFolds(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(5, 3)) // alpha header
	m = m2.(Model)
	if !m.list.folded["alpha"] {
		t.Fatal("clicking a header must fold its project")
	}
	if !m.list.OnHeader() {
		t.Error("cursor should park on the folded header")
	}
	m2, _ = m.Update(click(5, 3)) // click again: unfold
	m = m2.(Model)
	if m.list.folded["alpha"] {
		t.Error("second click must unfold")
	}
}

func TestClickBlankBelowListIsNoop(t *testing.T) {
	m := newTestModel()
	before, _, _ := m.list.Selected()
	m2, _ := m.Update(click(5, 20)) // inside the pane, below the last row
	m = m2.(Model)
	after, _, ok := m.list.Selected()
	if !ok || after.ID != before.ID {
		t.Errorf("selection changed from %v to %v on a blank-area click", before.ID, after.ID)
	}
}

func TestWheelOverListMovesSelection(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(wheel(5, 5, false)) // down: s1 → beta header
	m = m2.(Model)
	if !m.list.OnHeader() {
		t.Fatal("wheel down should move cursor to the beta header row")
	}
	m2, _ = m.Update(wheel(5, 5, false)) // down again: → s2
	m = m2.(Model)
	if s, _, ok := m.list.Selected(); !ok || s.ID != "s2" {
		t.Fatalf("second wheel down landed on %v, want s2", s.ID)
	}
	m2, _ = m.Update(wheel(5, 5, true)) // up: → header
	m = m2.(Model)
	m2, cmd := m.Update(wheel(5, 5, true)) // up again: → s1
	m = m2.(Model)
	if s, _, ok := m.list.Selected(); !ok || s.ID != "s1" {
		t.Errorf("wheel up landed on %v, want s1", s.ID)
	}
	// landing on a session (not a header) must reload the preview
	if cmd == nil {
		t.Error("wheeling onto a session must reload the preview")
	}
}

func TestClickListReturnsFocus(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus preview
	m = m2.(Model)
	m2, _ = m.Update(click(5, 8))
	m = m2.(Model)
	if m.focus != focusList {
		t.Errorf("focus = %v, want focusList after clicking the list", m.focus)
	}

	m2, _ = m.Update(key("/")) // focus filter
	m = m2.(Model)
	m2, _ = m.Update(click(5, 8))
	m = m2.(Model)
	if m.focus != focusList || m.filterInput.Focused() {
		t.Error("clicking the list while filtering must blur the filter and refocus the list")
	}
}

func TestDoubleClickResumes(t *testing.T) {
	m, dir := modelWithRealCWD(t)
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
	t0 := time.Now()
	m.now = func() time.Time { return t0 }
	m2, _ := m.Update(click(5, 4)) // s1 title line
	m = m2.(Model)
	m.now = func() time.Time { return t0.Add(200 * time.Millisecond) }
	m2, cmd := m.Update(click(5, 4))
	m = m2.(Model)
	if cmd == nil || rec.dir != dir {
		t.Fatalf("double-click: resume dir = %q, want %q (cmd nil: %v)", rec.dir, dir, cmd == nil)
	}
	if len(rec.args) != 2 || rec.args[0] != "--resume" || rec.args[1] != "s1" {
		t.Errorf("args = %v, want [--resume s1]", rec.args)
	}
}

func TestSlowSecondClickDoesNotResume(t *testing.T) {
	m, _ := modelWithRealCWD(t)
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
	t0 := time.Now()
	m.now = func() time.Time { return t0 }
	m2, _ := m.Update(click(5, 4))
	m = m2.(Model)
	m.now = func() time.Time { return t0.Add(time.Second) }
	m2, _ = m.Update(click(5, 4))
	m = m2.(Model)
	if rec.dir != "" {
		t.Error("a slow second click must not resume")
	}
}

func TestDoubleClickDifferentRowsDoesNotResume(t *testing.T) {
	m, _ := modelWithRealCWD(t)
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
	t0 := time.Now()
	m.now = func() time.Time { return t0 }
	m2, _ := m.Update(click(5, 4)) // s1
	m = m2.(Model)
	m.now = func() time.Time { return t0.Add(100 * time.Millisecond) }
	m2, _ = m.Update(click(5, 8)) // s2 — fast, but a different row
	m = m2.(Model)
	if rec.dir != "" {
		t.Error("fast clicks on different rows must not resume")
	}
	if s, _, _ := m.list.Selected(); s.ID != "s2" {
		t.Errorf("second click should still select s2, got %v", s.ID)
	}
}

func TestHeaderClickResetsDoubleClick(t *testing.T) {
	m, _ := modelWithRealCWD(t)
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
	t0 := time.Now()
	m.now = func() time.Time { return t0 }
	m2, _ := m.Update(click(5, 4)) // s1
	m = m2.(Model)
	m2, _ = m.Update(click(5, 3)) // alpha header (folds; s1 row index shifts)
	m = m2.(Model)
	m2, _ = m.Update(click(5, 3)) // unfold
	m = m2.(Model)
	m.now = func() time.Time { return t0.Add(300 * time.Millisecond) }
	m2, _ = m.Update(click(5, 4)) // s1 again, still inside the window
	m = m2.(Model)
	if rec.dir != "" {
		t.Error("a header click in between must reset double-click tracking")
	}
}

func TestClickPreviewFocusesIt(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(50, 10))
	m = m2.(Model)
	if m.focus != focusPreview {
		t.Errorf("focus = %v, want focusPreview", m.focus)
	}
}

func TestWheelOverPreviewScrollsWithoutFocus(t *testing.T) {
	m := newTestModel()
	m.preview.SetContent(strings.Repeat("x\n", 100))
	m.preview.GotoTop()
	m2, _ := m.Update(wheel(50, 10, false))
	m = m2.(Model)
	if m.preview.YOffset != 3 {
		t.Errorf("YOffset = %d, want 3 (one wheel tick)", m.preview.YOffset)
	}
	if m.focus != focusList {
		t.Error("wheel over the preview must not steal focus")
	}
	m2, _ = m.Update(wheel(50, 10, true))
	m = m2.(Model)
	if m.preview.YOffset != 0 {
		t.Errorf("YOffset = %d, want 0 after wheeling back up", m.preview.YOffset)
	}
}

func TestClickFilterBarFocusesFilter(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(5, 1))
	m = m2.(Model)
	if m.focus != focusFilter || !m.filterInput.Focused() {
		t.Fatalf("focus = %v focused=%v, want filter focused", m.focus, m.filterInput.Focused())
	}
	// typed keys must now go to the filter
	for _, r := range "backup" {
		m2, _ = m.Update(key(string(r)))
		m = m2.(Model)
	}
	if s, _, ok := m.list.Selected(); !ok || s.ID != "s2" {
		t.Errorf("typing after a filter-bar click selected %v, want s2", s.ID)
	}
}

func TestNonLeftPressesIgnored(t *testing.T) {
	m := newTestModel()
	before, _, _ := m.list.Selected()
	for _, msg := range []tea.MouseMsg{
		{X: 5, Y: 8, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft},
		{X: 5, Y: 8, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone},
		{X: 5, Y: 8, Action: tea.MouseActionPress, Button: tea.MouseButtonRight},
	} {
		m2, _ := m.Update(msg)
		m = m2.(Model)
	}
	if s, _, ok := m.list.Selected(); !ok || s.ID != before.ID {
		t.Error("release/motion/right-click must not change the selection")
	}
}

func TestHelpBarTextUnchanged(t *testing.T) {
	m := newTestModel()
	want := " ↵ resume  tab focus  n new  d delete  / filter  g group  space fold  e empty  r rescan  q quit"
	if !strings.Contains(m.View(), want) {
		t.Fatal("help bar text changed — it must stay byte-identical")
	}
}

func TestClickHelpBarTriggersAction(t *testing.T) {
	m := newTestModel()
	if !m.list.groupByProject {
		t.Fatal("setup: expected grouped mode")
	}
	m2, _ := m.Update(click(50, 29)) // inside "g group" [49,55]
	m = m2.(Model)
	if m.list.groupByProject {
		t.Error("clicking 'g group' must toggle grouping")
	}
	m2, _ = m.Update(click(60, 29)) // inside "space fold" [58,67]
	m = m2.(Model)
	// flat mode: fold is a no-op; regroup and fold via clicks
	m2, _ = m.Update(click(50, 29))
	m = m2.(Model)
	m2, _ = m.Update(click(60, 29))
	m = m2.(Model)
	if len(m.list.folded) == 0 {
		t.Error("clicking 'space fold' must fold the current project")
	}
}

func TestClickHelpBarGapIsNoop(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(47, 29)) // gap between "/ filter" and "g group"
	m = m2.(Model)
	if !m.list.groupByProject {
		t.Error("a click on a help-bar gap must do nothing")
	}
}

func TestClickHelpQuitReturnsQuit(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(click(90, 29)) // inside "q quit" [89,94]
	if cmd == nil {
		t.Fatal("clicking 'q quit' must return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("the command must be tea.Quit")
	}
}
