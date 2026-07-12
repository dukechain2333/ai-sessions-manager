package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
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

func TestClickSubheaderIsInert(t *testing.T) {
	m := newTestModel()
	// A project with both a Claude and a Codex session so a subheader renders.
	m.list.SetSessions(agentMixSessions())
	m.list.ToggleAgentGroup()

	// Locate the "─ Claude ─" subheader's screen line: list content starts at
	// terminal row 3, adjusted for any scroll offset.
	start, _ := m.list.layout()
	subRow := -1
	for i, r := range m.list.rows {
		if r.subheader {
			subRow = i
			break
		}
	}
	if subRow < 0 {
		t.Fatal("expected a subheader row with groupByAgent on a mixed project")
	}
	y := 3 + start[subRow] - m.list.lineOffset

	before, _, okBefore := m.list.Selected()
	if !okBefore {
		t.Fatal("precondition: a session should be selected before the click")
	}

	m2, _ := m.Update(click(5, y)) // click squarely on the "─ Claude ─" divider
	m = m2.(Model)

	// (a) the project must NOT have folded — its sessions stay visible.
	if m.list.folded["/x/mix"] {
		t.Error("clicking a subheader must not fold the project")
	}
	if !strings.Contains(m.list.View(), "claude a") {
		t.Errorf("session under the subheader should still be visible:\n%s", m.list.View())
	}
	// (b) the selection must not move onto a non-session; it stays put.
	after, _, okAfter := m.list.Selected()
	if !okAfter {
		t.Error("selection must still resolve to a session after a subheader click")
	}
	if after.ID != before.ID {
		t.Errorf("subheader click moved selection %s -> %s; should be inert", before.ID, after.ID)
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
	m.runCmd = rec.cmd
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
	m.runCmd = rec.cmd
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
	m.runCmd = rec.cmd
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
	m.runCmd = rec.cmd
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
	want := " ↵ resume  tab focus  n new  d delete  / filter  s search  g group  a agent  space fold  e empty  r rescan  q quit"
	if helpLine() != want {
		t.Fatalf("help bar text changed — it must stay byte-identical\n got: %q\nwant: %q", helpLine(), want)
	}
}

func TestClickHelpBarTriggersAction(t *testing.T) {
	m := newTestModel()
	if !m.list.groupByProject {
		t.Fatal("setup: expected grouped mode")
	}
	labelW := lipgloss.Width(m.projectLabelText())
	m2, _ := m.Update(click(labelW+60, 29)) // inside "g group" [59,65]
	m = m2.(Model)
	if m.list.groupByProject {
		t.Error("clicking 'g group' must toggle grouping")
	}
	m2, _ = m.Update(click(labelW+80, 29)) // inside "space fold" [77,86]
	m = m2.(Model)
	// flat mode: fold is a no-op; regroup and fold via clicks
	m2, _ = m.Update(click(labelW+60, 29))
	m = m2.(Model)
	m2, _ = m.Update(click(labelW+80, 29))
	m = m2.(Model)
	if len(m.list.folded) == 0 {
		t.Error("clicking 'space fold' must fold the current project")
	}
}

func TestClickHelpBarGapIsNoop(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(lipgloss.Width(m.projectLabelText())+47, 29)) // gap between "/ filter" and "s search"
	m = m2.(Model)
	if !m.list.groupByProject {
		t.Error("a click on a help-bar gap must do nothing")
	}
}

func TestClickHelpQuitReturnsQuit(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(click(lipgloss.Width(m.projectLabelText())+110, 29)) // inside "q quit" [108,113]
	if cmd == nil {
		t.Fatal("clicking 'q quit' must return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("the command must be tea.Quit")
	}
}

func TestClickHelpWhilePreviewFocused(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(50, 10)) // focus the preview
	m = m2.(Model)
	if m.focus != focusPreview {
		t.Fatal("setup: preview should be focused")
	}
	m2, _ = m.Update(click(lipgloss.Width(m.projectLabelText())+30, 29)) // "d delete" [29,36]
	m = m2.(Model)
	if m.dialog != dialogDelete {
		t.Errorf("dialog = %v, want dialogDelete (button must act on the list, not scroll the preview)", m.dialog)
	}
}

func TestClickHelpGapKeepsFocus(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(click(50, 10)) // focus the preview
	m = m2.(Model)
	m2, _ = m.Update(click(lipgloss.Width(m.projectLabelText())+47, 29)) // gap between "/ filter" and "s search"
	m = m2.(Model)
	if m.focus != focusPreview {
		t.Errorf("focus = %v, want focusPreview (a gap click must be a pure no-op)", m.focus)
	}
}

// dialogContentOrigin returns the screen cell of the dialog's content (0,0),
// i.e. the box origin shifted past border and padding.
func dialogContentOrigin(m Model) (int, int) {
	x0, y0 := m.dialogOrigin(m.dialogView())
	return x0 + m.st.DialogBox.GetBorderLeftSize() + m.st.DialogBox.GetPaddingLeft(),
		y0 + m.st.DialogBox.GetBorderTopSize() + m.st.DialogBox.GetPaddingTop()
}

func TestDialogOriginMatchesRender(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(key("d")) // delete dialog for s1
	m = m2.(Model)
	x0, y0 := m.dialogOrigin(m.dialogView())
	lines := strings.Split(m.View(), "\n")
	for y, ln := range lines {
		if x := strings.Index(ln, "╭"); x >= 0 {
			// bytes == cells here: everything left of the corner is spaces
			if x != x0 || y != y0 {
				t.Fatalf("corner rendered at (%d,%d), dialogOrigin says (%d,%d)", x, y, x0, y0)
			}
			return
		}
	}
	t.Fatal("no dialog border corner found in the rendered view")
}

func TestDeleteDialogButtons(t *testing.T) {
	m := newTestModel()
	trashed := ""
	m.trashFn = func(s store.Session) (string, error) {
		trashed = s.ID
		return "/trash/" + s.ID, nil
	}
	m2, _ := m.Update(key("d"))
	m = m2.(Model)
	cx, cy := dialogContentOrigin(m)
	m2, _ = m.Update(click(cx+4, cy+4)) // inside "y confirm" [0,8]
	m = m2.(Model)
	if trashed != "s1" || m.dialog != dialogNone {
		t.Fatalf("y-button: trashed=%q dialog=%v, want s1 trashed and dialog closed", trashed, m.dialog)
	}

	// n cancel
	m2, _ = m.Update(key("d"))
	m = m2.(Model)
	trashed = ""
	cx, cy = dialogContentOrigin(m)
	m2, _ = m.Update(click(cx+14, cy+4)) // inside "n cancel" [12,19]
	m = m2.(Model)
	if trashed != "" || m.dialog != dialogNone {
		t.Errorf("n-button: trashed=%q dialog=%v, want nothing trashed and dialog closed", trashed, m.dialog)
	}
}

func TestDeleteDialogClickOutsideCancels(t *testing.T) {
	m := newTestModel()
	m.trashFn = func(store.Session) (string, error) {
		t.Error("trashFn must not run on an outside click")
		return "", nil
	}
	m2, _ := m.Update(key("d"))
	m = m2.(Model)
	x0, y0 := m.dialogOrigin(m.dialogView())
	m2, _ = m.Update(click(x0-2, y0))
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Error("clicking outside the delete dialog must cancel it")
	}
}

func TestDeleteDialogDeadZoneClickKeepsDialog(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(key("d"))
	m = m2.(Model)
	cx, cy := dialogContentOrigin(m)
	m2, _ = m.Update(click(cx+10, cy+4)) // the " · " separator between buttons
	m = m2.(Model)
	if m.dialog != dialogDelete {
		t.Error("clicking between the buttons must keep the dialog open")
	}
}

func TestErrorDialogClickDismisses(t *testing.T) {
	m := newTestModel()
	m.dialog = dialogError
	m.errText = "boom"
	m2, _ := m.Update(click(3, 3)) // anywhere
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Error("any click must dismiss the error dialog")
	}
}

// shortTempDir returns a real, empty directory with a short, fixed-length
// name. Unlike t.TempDir(), whose path embeds the full (sub)test name, this
// keeps the rendered picker dialog's width independent of the caller's test
// name — a long test name alone was enough to push the box past the 100-col
// test terminal and (correctly) trip the oversize-dialog mouse guard.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func pickerModel(t *testing.T) (Model, string, string, *resumeRecorder) {
	t.Helper()
	dirA, dirB := shortTempDir(t), shortTempDir(t)
	m := newTestModel()
	// A second provider is registered so confirming a dir opens the
	// agent-pick dialog these tests exercise (the single-provider fast path
	// is covered by TestNewSessionSingleProviderLaunchesDirectly).
	m.providers = append(m.providers, store.NewCodexProvider(t.TempDir()))
	// s1 (the initially selected session) keeps its non-existent CWD, so "n"
	// falls back to the dir picker instead of jumping straight to the agent
	// picker; s2/s3 supply the known, existing directories.
	m.list.sessions[1].CWD = dirA // s2 → dirs[0]
	m.list.sessions[2].CWD = dirB // s3 → dirs[1]
	rec := &resumeRecorder{}
	m.runCmd = rec.cmd
	m2, _ := m.Update(key("n"))
	m = m2.(Model)
	if m.dialog != dialogPickDir || len(m.dirs) != 2 || m.dirs[1] != dirB {
		t.Fatalf("setup: dialog=%v dirs=%v", m.dialog, m.dirs)
	}
	return m, dirA, dirB, rec
}

func TestPickDirClickSelectsRow(t *testing.T) {
	m, _, _, _ := pickerModel(t)
	cx, cy := dialogContentOrigin(m)
	m2, _ := m.Update(click(cx+1, cy+3)) // content line 3 = dir row 1
	m = m2.(Model)
	if m.dirCursor != 1 {
		t.Errorf("dirCursor = %d, want 1", m.dirCursor)
	}
	if m.dialog != dialogPickDir {
		t.Error("a single click must not confirm")
	}
}

func TestPickDirDoubleClickConfirms(t *testing.T) {
	m, _, dirB, rec := pickerModel(t)
	t0 := time.Now()
	m.now = func() time.Time { return t0 }
	cx, cy := dialogContentOrigin(m)
	m2, _ := m.Update(click(cx+1, cy+3))
	m = m2.(Model)
	m.now = func() time.Time { return t0.Add(150 * time.Millisecond) }
	m2, _ = m.Update(click(cx+1, cy+3))
	m = m2.(Model)
	if m.dialog != dialogPickAgent || m.pendingNewDir != dirB {
		t.Fatalf("double-click confirm: dialog=%v pendingNewDir=%q, want dialogPickAgent %q", m.dialog, m.pendingNewDir, dirB)
	}
	m2, _ = m.Update(key("1"))
	m = m2.(Model)
	if rec.name != "claude" || rec.dir != dirB || len(rec.args) != 0 {
		t.Errorf("agent pick: name=%q dir=%q args=%v, want claude %q []", rec.name, rec.dir, rec.args, dirB)
	}
	if m.dialog != dialogNone {
		t.Error("confirming the agent must close the dialog")
	}
}

func TestPickDirWheelMovesCursor(t *testing.T) {
	m, _, _, _ := pickerModel(t)
	x0, y0 := m.dialogOrigin(m.dialogView())
	m2, _ := m.Update(wheel(x0+2, y0+2, false))
	m = m2.(Model)
	if m.dirCursor != 1 {
		t.Errorf("wheel down: dirCursor = %d, want 1", m.dirCursor)
	}
	m2, _ = m.Update(wheel(x0+2, y0+2, true))
	m = m2.(Model)
	if m.dirCursor != 0 {
		t.Errorf("wheel up: dirCursor = %d, want 0", m.dirCursor)
	}
}

func TestPickDirClickOutsideCancels(t *testing.T) {
	m, _, _, rec := pickerModel(t)
	m2, _ := m.Update(click(1, 3)) // far outside the centered box
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Error("clicking outside the picker must cancel it")
	}
	if rec.dir != "" {
		t.Error("cancel must not launch claude")
	}
}

func TestPickDirClickNonRowIsNoop(t *testing.T) {
	m, _, _, _ := pickerModel(t)
	cx, cy := dialogContentOrigin(m)
	m2, _ := m.Update(click(cx+1, cy)) // header line
	m = m2.(Model)
	if m.dialog != dialogPickDir || m.dirCursor != 0 {
		t.Errorf("header click: dialog=%v dirCursor=%d, want open dialog, cursor 0", m.dialog, m.dirCursor)
	}
}

func TestPickDirDoubleClickOverridesTypedPath(t *testing.T) {
	m, _, dirB, rec := pickerModel(t)
	m.dirInput.SetValue("~/definitely/not/here")
	t0 := time.Now()
	m.now = func() time.Time { return t0 }
	cx, cy := dialogContentOrigin(m)
	m2, _ := m.Update(click(cx+1, cy+3)) // dir row 1
	m = m2.(Model)
	m.now = func() time.Time { return t0.Add(150 * time.Millisecond) }
	m2, _ = m.Update(click(cx+1, cy+3))
	m = m2.(Model)
	if m.dialog != dialogPickAgent || m.pendingNewDir != dirB {
		t.Fatalf("double-click set pendingNewDir=%q dialog=%v, want the clicked row %q and dialogPickAgent", m.pendingNewDir, m.dialog, dirB)
	}
	m2, _ = m.Update(key("1"))
	m = m2.(Model)
	if rec.dir != dirB {
		t.Errorf("double-click launched %q, want the clicked row %q", rec.dir, dirB)
	}
	if m.dialog != dialogNone {
		t.Error("confirm must close the dialog")
	}
}

func TestOversizeDialogIgnoresMouse(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 11}) // delete box (9 rows) > body area (8)
	m = m2.(Model)
	trashed := ""
	m.trashFn = func(s store.Session) (string, error) {
		trashed = s.ID
		return "/trash/" + s.ID, nil
	}
	m2, _ = m.Update(key("d"))
	m = m2.(Model)
	x0, y0 := m.dialogOrigin(m.dialogView())
	m2, _ = m.Update(click(x0+3+4, y0+2+4)) // where the naive math puts "y confirm"
	m = m2.(Model)
	if trashed != "" || m.dialog != dialogDelete {
		t.Fatalf("oversize dialog must ignore mouse: trashed=%q dialog=%v", trashed, m.dialog)
	}
	m2, _ = m.Update(key("y")) // keyboard still works
	m = m2.(Model)
	if trashed != "s1" || m.dialog != dialogNone {
		t.Errorf("keyboard confirm broken: trashed=%q dialog=%v", trashed, m.dialog)
	}
}

func TestHelpBarTruncatesToWidth(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m = m2.(Model)
	lines := strings.Split(m.View(), "\n")
	last := lines[len(lines)-1]
	if w := lipgloss.Width(last); w > 60 {
		t.Errorf("help line width %d exceeds terminal width 60 — it must truncate, not wrap", w)
	}
}

func TestEnrichResetsClickTracker(t *testing.T) {
	m := newTestModel()
	m.lastClickRow = 1 // a prior click recorded this row
	m2, _ := m.Update(enrichMsg{
		EnrichResult: store.EnrichResult{Index: 0, Meta: store.Meta{
			Title: "x", CWD: "/x/alpha", UserMessages: 1, LastActivity: time.Now(),
		}},
		ch: m.enrichCh,
	})
	m = m2.(Model)
	if m.lastClickRow != -1 {
		t.Errorf("enrichMsg must reset lastClickRow (rows can renumber), got %d", m.lastClickRow)
	}
}

func TestScanResetsClickTracker(t *testing.T) {
	m := newTestModel()
	m.lastClickRow = 2
	m2, _ := m.Update(scanDoneMsg{sessions: testSessions()})
	m = m2.(Model)
	if m.lastClickRow != -1 {
		t.Errorf("scanDoneMsg must reset lastClickRow (rows renumber), got %d", m.lastClickRow)
	}
}

func TestClickHelpWithProjectLabelStillWorks(t *testing.T) {
	m := newTestModel() // a session is selected → non-empty project label
	// Compute the "q quit" button's screen x AFTER the label offset.
	base := lipgloss.Width(m.projectLabelText()) + 1 // +1 = helpLine leading space
	pos := base
	var qx int = -1
	for _, it := range helpBar {
		w := lipgloss.Width(it.label)
		if it.label == "q quit" {
			qx = pos + 1 // a cell inside the button
			break
		}
		pos += w + 2
	}
	if qx < 0 {
		t.Fatal("no 'q quit' item in helpBar")
	}
	m2, cmd := m.Update(click(qx, m.height-1))
	_ = m2
	if cmd == nil {
		t.Fatalf("click on 'q quit' at x=%d produced no cmd", qx)
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("clicking 'q quit' (label-shifted position) should quit")
	}
}
