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

// handleMouse dispatches mouse events. Only left presses and wheel ticks
// act; motion, release, and other buttons are ignored. Keyboard paths are
// never touched.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.dialog != dialogNone {
		return m, nil // dialog mouse support lands in a later task
	}
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	z, line := m.zoneAt(msg.X, msg.Y)

	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		switch z {
		case zoneList:
			delta := 1
			if msg.Button == tea.MouseButtonWheelUp {
				delta = -1
			}
			m.list.MoveCursor(delta)
			return m, m.loadTranscriptCmd()
		case zonePreview:
			var cmd tea.Cmd
			m.preview, cmd = m.preview.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	// A click outside the filter bar while typing in it puts the keyboard
	// back on the list; the filter text stays (same as pressing enter).
	if m.focus == focusFilter && z != zoneFilter {
		m.filterInput.Blur()
		m.focus = focusList
	}

	switch z {
	case zoneList:
		m.focus = focusList
		return m.clickList(line)
	case zonePreview:
		m.focus = focusPreview
		return m, nil
	case zoneFilter:
		m.focus = focusFilter
		m.filterInput.Focus()
		return m, nil
	case zoneHelp:
		return m, nil // clickable help bar lands in a later task
	}
	return m, nil
}

// clickList selects the row under a click; header rows fold instead.
func (m Model) clickList(line int) (tea.Model, tea.Cmd) {
	row, ok := m.list.RowAtLine(line)
	if !ok {
		return m, nil
	}
	m.list.SetCursor(row)
	if m.list.OnHeader() {
		m.list.ToggleFold()
		return m, m.loadTranscriptCmd()
	}
	return m, m.loadTranscriptCmd()
}
