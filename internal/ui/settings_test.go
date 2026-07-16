package ui

import (
	"errors"
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

func TestSettingsTextEditCommits(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.setCursor = 2 // iterm2 ssh
	m2, _ := sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	if !sm.setEditing {
		t.Fatal("enter on a text row should start editing")
	}
	sm.setInput.SetValue("myhost")
	m2, _ = sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	if sm.setEditing {
		t.Error("enter should commit and leave edit mode")
	}
	if sm.setForm.ITerm2SSH != "myhost" {
		t.Errorf("committed value = %q, want myhost", sm.setForm.ITerm2SSH)
	}
	if sm.dialog != dialogSettings {
		t.Errorf("dialog must stay open after a commit, got %v", sm.dialog)
	}
}

func TestSettingsInvalidHexRejected(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.setCursor = 4 // claude light
	m2, _ := sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	sm.setInput.SetValue("nothex")
	m2, _ = sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	if !sm.setEditing {
		t.Error("invalid hex must keep the row in edit mode")
	}
	if sm.setErr == "" {
		t.Error("invalid hex must set an inline error")
	}
	if sm.setForm.Claude.Light != "#C15F3C" {
		t.Errorf("invalid hex must not be committed, got %q", sm.setForm.Claude.Light)
	}
}

func TestSettingsEscCancelsRowEditNotDialog(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.setCursor = 2
	m2, _ := sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m2.(Model)
	sm.setInput.SetValue("abandoned")
	m2, _ = sm.handleDialogKey(escKey())
	sm = m2.(Model)
	if sm.setEditing {
		t.Error("esc should leave edit mode")
	}
	if sm.dialog != dialogSettings {
		t.Errorf("esc while editing must not close the dialog, got %v", sm.dialog)
	}
	if sm.setForm.ITerm2SSH != "" {
		t.Errorf("abandoned edit must not be committed, got %q", sm.setForm.ITerm2SSH)
	}
}

func TestSettingsEditSeedsCurrentValue(t *testing.T) {
	cfg := config.Default()
	cfg.ITerm2SSH = "generalserver"
	m := New("/nope", "/nope", "", cfg)
	m2, _ := m.Update(scanDoneMsg{sessions: testSessions()})
	sm := openSettingsDialog(t, m2.(Model))
	sm.setCursor = 2
	m3, _ := sm.handleDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
	sm = m3.(Model)
	if got := sm.setInput.Value(); got != "generalserver" {
		t.Errorf("editor must seed with the current value, got %q", got)
	}
}

func TestSettingsSaveWritesAndConfirms(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.configPath = "/cfg/config.json"
	var gotPath string
	var gotCfg config.Config
	sm.saveConfig = func(p string, c config.Config) error { gotPath, gotCfg = p, c; return nil }
	sm.setForm.View = "tabs"
	m2, _ := sm.handleDialogKey(runeKey("s"))
	sm = m2.(Model)
	if gotPath != "/cfg/config.json" {
		t.Errorf("saved to %q, want /cfg/config.json", gotPath)
	}
	if gotCfg.View != "tabs" {
		t.Errorf("saved config = %+v, want the edited form", gotCfg)
	}
	if sm.dialog != dialogInfo {
		t.Errorf("save should show the info dialog, got %v", sm.dialog)
	}
	if !strings.Contains(sm.errText, "restart sm") {
		t.Errorf("confirmation should mention restarting, got %q", sm.errText)
	}
	if sm.cfg.View != "tabs" {
		t.Error("m.cfg must track the saved form so reopening shows saved values")
	}
}

func TestSettingsSaveFailureKeepsForm(t *testing.T) {
	sm := openSettingsDialog(t, newTestModel())
	sm.saveConfig = func(string, config.Config) error { return errors.New("disk full") }
	sm.setForm.View = "tabs"
	m2, _ := sm.handleDialogKey(runeKey("s"))
	sm = m2.(Model)
	if sm.dialog != dialogSettings {
		t.Errorf("failed save must keep the dialog open, got %v", sm.dialog)
	}
	if !strings.Contains(sm.setErr, "disk full") {
		t.Errorf("save error must surface inline, got %q", sm.setErr)
	}
	if sm.setForm.View != "tabs" {
		t.Error("failed save must preserve the form for a retry")
	}
}
