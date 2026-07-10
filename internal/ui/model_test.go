package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func newTestModel() Model {
	m := New("/nonexistent-projects-dir")
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(Model)
	m2, _ = m.Update(scanDoneMsg{sessions: testSessions()})
	m = m2.(Model)
	for i, s := range testSessions() {
		m2, _ = m.Update(enrichMsg{Index: i, Meta: store.Meta{
			Title: s.Title, FirstPrompt: s.FirstPrompt, CWD: s.CWD,
			UserMessages: s.UserMessages, LastActivity: s.LastActivity,
		}})
		m = m2.(Model)
	}
	return m
}

func TestSlashEntersFilterMode(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	if m.focus != focusFilter {
		t.Fatalf("focus = %v, want focusFilter", m.focus)
	}
	for _, r := range "backup" {
		m2, _ = m.Update(key(string(r)))
		m = m2.(Model)
	}
	s, _, ok := m.list.Selected()
	if !ok || s.ID != "s2" {
		t.Errorf("filtered selection = %v, want s2", s.ID)
	}
	// enter keeps filter, returns focus to list
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.focus != focusList || m.list.filter != "backup" {
		t.Errorf("after enter: focus=%v filter=%q", m.focus, m.list.filter)
	}
	// esc clears filter
	m2, _ = m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	if m.list.filter != "" {
		t.Errorf("esc should clear filter, got %q", m.list.filter)
	}
}

func TestTabTogglesFocus(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	if m.focus != focusPreview {
		t.Fatalf("focus = %v, want focusPreview", m.focus)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	if m.focus != focusList {
		t.Fatalf("focus = %v, want focusList", m.focus)
	}
}

func TestJKMoveCursor(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(key("j"))
	m = m2.(Model)
	s, _, _ := m.list.Selected()
	if s.ID != "s2" {
		t.Errorf("after j: selected %s, want s2", s.ID)
	}
	m2, _ = m.Update(key("k"))
	m = m2.(Model)
	s, _, _ = m.list.Selected()
	if s.ID != "s1" {
		t.Errorf("after k: selected %s, want s1", s.ID)
	}
}

func TestEmptyToggleKey(t *testing.T) {
	m := newTestModel()
	before := m.list.Len()
	m2, _ := m.Update(key("e"))
	m = m2.(Model)
	if m.list.Len() != before+1 {
		t.Errorf("Len = %d, want %d", m.list.Len(), before+1)
	}
}

func TestQuitKey(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(key("q"))
	if cmd == nil {
		t.Fatal("q should return a quit cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q cmd should produce tea.QuitMsg")
	}
}

func TestViewRenders(t *testing.T) {
	m := newTestModel()
	v := m.View()
	if v == "" {
		t.Fatal("empty view")
	}
}
