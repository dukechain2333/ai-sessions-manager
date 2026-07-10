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
