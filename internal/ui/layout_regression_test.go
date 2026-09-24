package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

func TestListPaneWidthMatchesHitZones(t *testing.T) {
	m := newTestModel()
	m.list.SetSessions([]store.Session{{ID: "a", Title: "Hi", CWD: "/p", Agent: store.AgentClaude, LastActivity: time.Now()}})
	expected, _ := m.paneWidths()
	actual := lipgloss.Width(m.list.View()) + 2
	z, _ := m.zoneAt(actual+1, 4)
	if actual != expected || z != zonePreview {
		t.Fatalf("list actual outer width=%d, layout/hit-test width=%d; click inside visible preview at x=%d routes zone=%d (preview=%d)", actual, expected, actual+1, z, zonePreview)
	}
}

func TestCJKListFitsTerminal(t *testing.T) {
	m := newTestModel()
	m.list.SetSessions([]store.Session{{ID: "a", Title: strings.Repeat("中文", 30), CWD: "/p", Agent: store.AgentClaude}})
	for i, line := range strings.Split(m.View(), "\n") {
		if width := lipgloss.Width(line); width > m.width {
			t.Fatalf("rendered row %d is %d cells, terminal is %d", i, width, m.width)
		}
	}
}

func TestResizeReflowsLoadedTranscript(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
	m = next.(Model)
	m.previewFor = "a"
	tr := store.Transcript{Messages: []store.Message{{Kind: store.KindAssistant, Text: strings.Repeat("a", 65) + " TAILVISIBLE"}}}
	next, _ = m.Update(transcriptMsg{id: "a", t: tr})
	m = next.(Model)
	if !strings.Contains(m.preview.View(), "TAILVISIBLE") {
		t.Fatal("fixture is not visible before resize")
	}
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	if !strings.Contains(m.preview.View(), "TAILVISIBLE") && cmd == nil {
		t.Fatalf("resize to preview width %d hid the tail without scheduling reflow; view=%q", m.preview.Width, m.preview.View())
	}
}

func TestFilterInputFitsTerminal(t *testing.T) {
	m := newTestModel()
	m.filterInput.SetValue(strings.Repeat("query", 25))
	next, _ := m.Update(key("/"))
	m = next.(Model)
	line := strings.Split(m.View(), "\n")[1]
	if w := lipgloss.Width(line); w > m.width {
		t.Fatalf("filter width=%d exceeds terminal width=%d", w, m.width)
	}
}

func TestHeightResizeKeepsTranscriptScrollPosition(t *testing.T) {
	m := newTestModel()
	m.previewFor = "a"
	tr := store.Transcript{Messages: []store.Message{{Kind: store.KindAssistant, Text: strings.Repeat("a line of text\n", 100)}}}
	next, _ := m.Update(transcriptMsg{id: "a", t: tr})
	m = next.(Model)
	m.preview.SetYOffset(40)
	next, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 35})
	m = next.(Model)
	if m.preview.YOffset != 40 {
		t.Fatalf("height-only resize lost scroll position: %d", m.preview.YOffset)
	}
}
