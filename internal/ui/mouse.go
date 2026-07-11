package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// runeKey builds the KeyMsg for a printable key, so help-bar buttons reuse
// the exact key-handling paths.
func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// helpItem is one clickable segment of the help bar.
type helpItem struct {
	label string
	key   tea.KeyMsg
}

// helpBar drives both the rendered help line and its hit-testing; the two
// can't drift because they read the same table.
var helpBar = []helpItem{
	{"↵ resume", tea.KeyMsg{Type: tea.KeyEnter}},
	{"tab focus", tea.KeyMsg{Type: tea.KeyTab}},
	{"n new", runeKey("n")},
	{"d delete", runeKey("d")},
	{"/ filter", runeKey("/")},
	{"g group", runeKey("g")},
	{"space fold", runeKey(" ")},
	{"e empty", runeKey("e")},
	{"r rescan", runeKey("r")},
	{"q quit", runeKey("q")},
}

// helpLine renders the help bar's text (unstyled).
func helpLine() string {
	parts := make([]string, len(helpBar))
	for i, it := range helpBar {
		parts[i] = it.label
	}
	return " " + strings.Join(parts, "  ")
}

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
		return m.clickHelp(msg.X)
	}
	return m, nil
}

// doubleClickWindow is how close two presses on the same row must be to
// count as a double-click.
const doubleClickWindow = 400 * time.Millisecond

func (m *Model) isDoubleClick(z zone, row int) bool {
	return z == m.lastClickZone && row == m.lastClickRow &&
		m.now().Sub(m.lastClickAt) <= doubleClickWindow
}

func (m *Model) recordClick(z zone, row int) {
	m.lastClickZone, m.lastClickRow, m.lastClickAt = z, row, m.now()
}

// clickList selects the row under a click; header rows fold instead, and a
// second click on the same session within doubleClickWindow resumes it.
func (m Model) clickList(line int) (tea.Model, tea.Cmd) {
	row, ok := m.list.RowAtLine(line)
	if !ok {
		return m, nil
	}
	m.list.SetCursor(row)
	if m.list.OnHeader() {
		m.lastClickRow = -1 // folding renumbers rows; stale indexes must not pair
		m.list.ToggleFold()
		return m, m.loadTranscriptCmd()
	}
	if m.isDoubleClick(zoneList, row) {
		m.lastClickRow = -1
		return m.startResume()
	}
	m.recordClick(zoneList, row)
	return m, m.loadTranscriptCmd()
}

// clickHelp maps an x coordinate on the help bar to its segment and feeds
// that segment's key through the normal key path.
func (m Model) clickHelp(x int) (tea.Model, tea.Cmd) {
	pos := 1 // leading space
	for _, it := range helpBar {
		w := lipgloss.Width(it.label)
		if x >= pos && x < pos+w {
			// Buttons act from list focus: without this, a synthesized key
			// would be eaten by whichever pane holds focus (e.g. "d" scrolls
			// a focused preview half a page instead of opening the delete
			// dialog). Gap clicks fall through and change nothing.
			m.focus = focusList
			return m.handleKey(it.key)
		}
		pos += w + 2
	}
	return m, nil
}
