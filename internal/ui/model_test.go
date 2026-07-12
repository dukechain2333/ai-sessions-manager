package ui

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

func TestClaudeNonZeroExitNotShownAsError(t *testing.T) {
	m := newTestModel()
	// A real *exec.ExitError: claude ran and exited non-zero (declined trust
	// prompt, Ctrl-C, /exit, etc.). This is normal — no error dialog.
	exitErr := exec.Command("sh", "-c", "exit 1").Run()
	if exitErr == nil {
		t.Fatal("setup: expected a non-nil exit error")
	}
	m2, cmd := m.Update(claudeExitMsg{err: exitErr})
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Errorf("non-zero claude exit should return to the list, got dialog=%v err=%q", m.dialog, m.errText)
	}
	if cmd == nil {
		t.Error("expected a rescan cmd after claude exits")
	}
}

func TestClaudeLaunchFailureShownAsError(t *testing.T) {
	m := newTestModel()
	// Not an *exec.ExitError: claude could not be launched at all.
	m2, _ := m.Update(claudeExitMsg{err: errors.New(`exec: "claude": executable file not found in $PATH`)})
	m = m2.(Model)
	if m.dialog != dialogError {
		t.Error("a genuine launch failure should show the error dialog")
	}
}

func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestNewBuildsProviders(t *testing.T) {
	m := New("/nope/claude", "/nope/codex")
	if len(m.providers) == 0 {
		t.Error("expected at least the claude provider")
	}
}

func newTestModel() Model {
	m := New("/nonexistent-projects-dir", "/nonexistent-codex-dir")
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(Model)
	m2, _ = m.Update(scanDoneMsg{sessions: testSessions()})
	m = m2.(Model)
	for i, s := range testSessions() {
		m2, _ = m.Update(enrichMsg{EnrichResult: store.EnrichResult{Index: i, Meta: store.Meta{
			Title: s.Title, FirstPrompt: s.FirstPrompt, CWD: s.CWD,
			UserMessages: s.UserMessages, LastActivity: s.LastActivity,
		}}, ch: m.enrichCh})
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
	// Flat recency order so navigation is session-to-session (no headers).
	m2, _ := m.Update(key("g"))
	m = m2.(Model)
	m2, _ = m.Update(key("j"))
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

func TestSpaceFoldsGroup(t *testing.T) {
	m := newTestModel() // grouped by default; cursor on first session (s1, project alpha)
	if m.list.OnHeader() {
		t.Fatal("cursor should start on a session, not a header")
	}
	m2, _ := m.Update(key(" "))
	m = m2.(Model)
	if !m.list.OnHeader() {
		t.Error("space should fold the group and park the cursor on its header")
	}
	if strings.Contains(m.list.View(), "Create slides from notes") {
		t.Error("folded group should hide its session titles")
	}
}

func TestEnterOnHeaderFoldsNotResume(t *testing.T) {
	m := newTestModel()
	// Move up onto the leading project header.
	m2, _ := m.Update(key("k"))
	m = m2.(Model)
	if !m.list.OnHeader() {
		t.Fatal("k from the first session should land on the group header")
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	// Enter on a header folds; it must not open a dialog or try to resume.
	if m.dialog != dialogNone {
		t.Errorf("enter on header should not open a dialog, got %v", m.dialog)
	}
	if strings.Contains(m.list.View(), "Create slides from notes") {
		t.Error("enter on header should have folded the group")
	}
	_ = cmd
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

func TestStaleEnrichDropped(t *testing.T) {
	m := newTestModel()
	before := m.list.sessions[0].Title
	stale := make(chan store.EnrichResult)
	m2, cmd := m.Update(enrichMsg{
		EnrichResult: store.EnrichResult{Index: 0, Meta: store.Meta{Title: "STALE"}},
		ch:           stale,
	})
	m = m2.(Model)
	if got := m.list.sessions[0].Title; got != before {
		t.Errorf("stale enrich applied: title = %q, want %q", got, before)
	}
	if cmd != nil {
		t.Error("stale enrich should not re-arm waitEnrich (cmd must be nil)")
	}
}

func TestViewRenders(t *testing.T) {
	m := newTestModel()
	v := m.View()
	if v == "" {
		t.Fatal("empty view")
	}
}
