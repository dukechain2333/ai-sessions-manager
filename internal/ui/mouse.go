package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// zone is an interactive region of the screen. zoneDialog is never returned
// by zoneAt; it tags dialog rows in the double-click tracker.
type zone int

const (
	zoneNone zone = iota
	zoneFilter
	zoneList
	zonePreview
	zoneHelp
	zoneDialog
)

// zoneAt resolves screen coordinates to the region under them. Geometry
// mirrors layout()/View(): row 0 title, row 1 filter bar, row 2 body top
// border, pane content rows [3, 2+bodyH], help bar on the last row. The
// second return is the 0-based content line inside the pane (zoneList /
// zonePreview only).
func (m *Model) zoneAt(x, y int) (zone, int) {
	switch {
	case y == 1:
		return zoneFilter, 0
	case y == m.height-1:
		return zoneHelp, 0
	}
	bodyH := m.bodyHeight()
	if y < 3 || y > 2+bodyH {
		return zoneNone, 0
	}
	line := y - 3
	listW, previewW := m.paneWidths()
	if x >= 1 && x <= listW-2 {
		return zoneList, line
	}
	if !m.narrow() && x >= listW+1 && x <= listW+previewW {
		return zonePreview, line
	}
	return zoneNone, 0
}

// handleMouse dispatches mouse events. Built out task by task; keyboard
// paths are never touched.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	return m, nil
}
