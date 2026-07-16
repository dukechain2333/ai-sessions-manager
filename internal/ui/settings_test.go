package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/ai-sessions-manager/internal/config"
)

func escKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEsc} }

// openSettingsDialog drives the ',' key from the list and asserts the
// dialog opened. (Named to avoid shadowing confusion with the Model method
// openSettings.)
func openSettingsDialog(t *testing.T, m Model) Model {
	t.Helper()
	m2, _ := m.handleKey(runeKey(","))
	sm := m2.(Model)
	if sm.dialog != dialogSettings {
		t.Fatalf("',' should open the settings dialog, got dialog=%v", sm.dialog)
	}
	return sm
}

func TestSettingsOpenSeedsFormFromStartupConfig(t *testing.T) {
	cfg := config.Default()
	cfg.ITerm2SSH = "generalserver"
	cfg.View = "tabs"
	m := New("/nope", "/nope", "/tmp/config.json", cfg)
	m2, _ := m.Update(scanDoneMsg{sessions: testSessions()})
	sm := openSettingsDialog(t, m2.(Model))
	if sm.setForm != cfg {
		t.Errorf("form must seed from the startup config:\n got %+v\nwant %+v", sm.setForm, cfg)
	}
	if sm.setCursor != 0 || sm.setEditing || sm.setErr != "" {
		t.Errorf("form state not reset: cursor=%d editing=%v err=%q", sm.setCursor, sm.setEditing, sm.setErr)
	}
}

func TestSettingsCursorMovesAndClamps(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	last := len(settingsRows()) - 1
	for i := 0; i < last+3; i++ { // overshoot: must clamp at the last row
		m2, _ := sm.handleDialogKey(runeKey("j"))
		sm = m2.(Model)
	}
	if sm.setCursor != last {
		t.Errorf("cursor = %d after overshooting down, want %d", sm.setCursor, last)
	}
	for i := 0; i < last+3; i++ {
		m2, _ := sm.handleDialogKey(runeKey("k"))
		sm = m2.(Model)
	}
	if sm.setCursor != 0 {
		t.Errorf("cursor = %d after overshooting up, want 0", sm.setCursor)
	}
}

func TestSettingsEscClosesWithoutSaving(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	saved := false
	sm.saveConfig = func(string, config.Config) error { saved = true; return nil }
	sm.setForm.View = "tabs" // dirty the form, then abandon it
	m2, _ := sm.handleDialogKey(escKey())
	sm = m2.(Model)
	if sm.dialog != dialogNone {
		t.Errorf("esc should close the dialog, got %v", sm.dialog)
	}
	if saved {
		t.Error("esc must not write the config")
	}
	if sm.cfg.View == "tabs" {
		t.Error("abandoned edits must not leak into m.cfg")
	}
}

func TestSettingsViewRendersEveryRow(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	out := sm.settingsView()
	for _, want := range []string{
		"view", "open_in mode", "iterm2 ssh", "tmux enabled",
		"claude light", "claude dark", "codex light", "codex dark",
		"#C15F3C", "#D97757", "#0A7C66", "#10A37F",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings view missing %q:\n%s", want, out)
		}
	}
}

func TestSettingsEnumCycles(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.setCursor = 0 // view: list ⇄ tabs
	press := func(k string) {
		m2, _ := sm.handleDialogKey(runeKey(k))
		sm = m2.(Model)
	}
	press("l")
	if sm.setForm.View != "tabs" {
		t.Errorf("l should cycle view to tabs, got %q", sm.setForm.View)
	}
	press("l") // wraps
	if sm.setForm.View != "list" {
		t.Errorf("cycling past the end should wrap to list, got %q", sm.setForm.View)
	}
	press("h") // backward wraps too
	if sm.setForm.View != "tabs" {
		t.Errorf("h should cycle backward (wrapping), got %q", sm.setForm.View)
	}
	m2, _ := sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	if sm.setForm.View != "list" {
		t.Errorf("enter should cycle forward, got %q", sm.setForm.View)
	}
}

func TestSettingsBoolToggles(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.setCursor = 3 // tmux enabled
	m2, _ := sm.handleDialogKey(runeKey(" "))
	sm = m2.(Model)
	if !sm.setForm.TmuxEnabled {
		t.Error("space should toggle tmux on")
	}
	m2, _ = sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	if sm.setForm.TmuxEnabled {
		t.Error("enter should toggle tmux back off")
	}
}
