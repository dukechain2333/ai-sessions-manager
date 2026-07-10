package ui

import (
	"testing"

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
