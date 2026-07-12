package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

// resumeRecorder stands in for runCmd and records invocations.
type resumeRecorder struct {
	name string
	dir  string
	args []string
}

func (r *resumeRecorder) cmd(name, dir string, args ...string) tea.Cmd {
	r.name, r.dir, r.args = name, dir, args
	return func() tea.Msg { return nil }
}

func modelWithRealCWD(t *testing.T) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	m := newTestModel()
	// point first session at a directory that exists
	m.list.sessions[0].CWD = dir
	return m, dir
}

func TestEnterResumesInSessionCWD(t *testing.T) {
	m, dir := modelWithRealCWD(t)
	rec := &resumeRecorder{}
	m.runCmd = rec.cmd
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("enter should return a cmd")
	}
	if rec.name != "claude" {
		t.Errorf("name = %q, want claude", rec.name)
	}
	if rec.dir != dir {
		t.Errorf("resume dir = %q, want %q", rec.dir, dir)
	}
	if len(rec.args) != 2 || rec.args[0] != "--resume" || rec.args[1] != "s1" {
		t.Errorf("args = %v, want [--resume s1]", rec.args)
	}
}

func TestResumeCodexUsesCodexCommand(t *testing.T) {
	m := newTestModel()
	dir := t.TempDir()
	// make the selected session a codex session in an existing dir
	m.list.sessions[0].Agent = store.AgentCodex
	m.list.sessions[0].CWD = dir
	// ensure a codex provider is registered
	m.providers = append(m.providers, store.NewCodexProvider(t.TempDir()))
	rec := &resumeRecorder{}
	m.runCmd = rec.cmd
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if rec.name != "codex" || len(rec.args) != 2 || rec.args[0] != "resume" {
		t.Errorf("codex resume = %s %v", rec.name, rec.args)
	}
	if rec.dir != dir {
		t.Errorf("dir = %q, want %q", rec.dir, dir)
	}
}

func TestEnterMissingCWDOpensPicker(t *testing.T) {
	m := newTestModel()
	m.list.sessions[0].CWD = "/definitely/not/a/real/dir"
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.dialog != dialogPickDir {
		t.Fatalf("dialog = %v, want dialogPickDir", m.dialog)
	}
	if m.pendingResume == nil || m.pendingResume.ID != "s1" {
		t.Error("pendingResume not set")
	}
}

func TestDeleteFlow(t *testing.T) {
	m := newTestModel()
	trashed := ""
	m.trashFn = func(s store.Session) (string, error) {
		trashed = s.ID
		return "/trash/" + s.ID, nil
	}
	before := m.list.Len()
	m2, _ := m.Update(key("d"))
	m = m2.(Model)
	if m.dialog != dialogDelete {
		t.Fatalf("dialog = %v, want dialogDelete", m.dialog)
	}
	m2, _ = m.Update(key("y"))
	m = m2.(Model)
	if trashed != "s1" {
		t.Errorf("trashed %q, want s1", trashed)
	}
	if m.dialog != dialogNone || m.list.Len() != before-1 {
		t.Errorf("after delete: dialog=%v len=%d", m.dialog, m.list.Len())
	}
}

func TestDeleteCancel(t *testing.T) {
	m := newTestModel()
	m.trashFn = func(store.Session) (string, error) {
		t.Error("trashFn must not be called on cancel")
		return "", nil
	}
	m2, _ := m.Update(key("d"))
	m = m2.(Model)
	m2, _ = m.Update(key("n"))
	m = m2.(Model)
	if m.dialog != dialogNone || m.list.Len() != 2 {
		t.Errorf("cancel failed: dialog=%v len=%d", m.dialog, m.list.Len())
	}
}

func TestNewSessionPicker(t *testing.T) {
	dir := t.TempDir()
	m := newTestModel()
	// s1 (the initially selected session) keeps its non-existent CWD, so "n"
	// falls back to the dir picker; s2 supplies a known, existing directory.
	m.list.sessions[1].CWD = dir
	rec := &resumeRecorder{}
	m.runCmd = rec.cmd
	m2, _ := m.Update(key("n"))
	m = m2.(Model)
	if m.dialog != dialogPickDir {
		t.Fatalf("dialog = %v, want dialogPickDir", m.dialog)
	}
	if len(m.dirs) == 0 || m.dirs[0] != dir {
		t.Fatalf("dirs = %v, want [%s]", m.dirs, dir)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.dialog != dialogPickAgent || m.pendingNewDir != dir {
		t.Fatalf("after picking a dir: dialog=%v pendingNewDir=%q, want dialogPickAgent %q", m.dialog, m.pendingNewDir, dir)
	}
	m2, _ = m.Update(key("1"))
	m = m2.(Model)
	if rec.name != "claude" || rec.dir != dir || len(rec.args) != 0 {
		t.Errorf("new session: name=%q dir=%q args=%v, want claude dir=%q args=[]", rec.name, rec.dir, rec.args, dir)
	}
	if m.dialog != dialogNone {
		t.Error("dialog should close after picking the agent")
	}
}

func TestNewSessionTypedPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	sub := filepath.Join(home, ".cache")
	if _, err := os.Stat(sub); err != nil {
		t.Skip("~/.cache missing")
	}
	m := newTestModel()
	rec := &resumeRecorder{}
	m.runCmd = rec.cmd
	m2, _ := m.Update(key("n"))
	m = m2.(Model)
	for _, r := range "~/.cache" {
		m2, _ = m.Update(key(string(r)))
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.dialog != dialogPickAgent || m.pendingNewDir != sub {
		t.Fatalf("after typing a path: dialog=%v pendingNewDir=%q, want dialogPickAgent %q", m.dialog, m.pendingNewDir, sub)
	}
	m2, _ = m.Update(key("c"))
	m = m2.(Model)
	if rec.name != "claude" || rec.dir != sub {
		t.Errorf("typed path resumed in name=%q dir=%q, want claude %q", rec.name, rec.dir, sub)
	}
}

func TestNewSessionOpensAgentPicker(t *testing.T) {
	m := newTestModel()
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	m2, _ := m.Update(key("n"))
	m = m2.(Model)
	if m.dialog != dialogPickAgent {
		t.Fatalf("dialog = %v, want dialogPickAgent", m.dialog)
	}
	// pick Claude (key "1") launches claude in the selected project dir
	rec := &resumeRecorder{}
	m.runCmd = rec.cmd
	m2, _ = m.Update(key("1"))
	m = m2.(Model)
	if rec.name != "claude" || rec.dir != dir || len(rec.args) != 0 {
		t.Errorf("new claude = %s %q %v", rec.name, rec.dir, rec.args)
	}
}

func TestAgentGroupKeyToggles(t *testing.T) {
	m := newTestModel()
	before := m.list.groupByAgent
	m2, _ := m.Update(key("a"))
	m = m2.(Model)
	if m.list.groupByAgent == before {
		t.Error("`a` should toggle agent grouping")
	}
}

func TestErrorDialogDismiss(t *testing.T) {
	m := newTestModel()
	m.dialog = dialogError
	m.errText = "boom"
	m2, _ := m.Update(key("x"))
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Error("any key should dismiss error dialog")
	}
}
