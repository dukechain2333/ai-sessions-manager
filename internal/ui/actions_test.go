package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

// resumeRecorder stands in for runClaude and records invocations.
type resumeRecorder struct {
	dir  string
	args []string
}

func (r *resumeRecorder) cmd(dir string, args ...string) tea.Cmd {
	r.dir = dir
	r.args = args
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
	m.runClaude = rec.cmd
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("enter should return a cmd")
	}
	if rec.dir != dir {
		t.Errorf("resume dir = %q, want %q", rec.dir, dir)
	}
	if len(rec.args) != 2 || rec.args[0] != "--resume" || rec.args[1] != "s1" {
		t.Errorf("args = %v, want [--resume s1]", rec.args)
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
	m.trashFn = func(projectsDir string, s store.Session) (string, error) {
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
	m.trashFn = func(string, store.Session) (string, error) {
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
	m.list.sessions[0].CWD = dir // KnownDirs needs an existing dir
	rec := &resumeRecorder{}
	m.runClaude = rec.cmd
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
	if rec.dir != dir || len(rec.args) != 0 {
		t.Errorf("new session: dir=%q args=%v, want dir=%q args=[]", rec.dir, rec.args, dir)
	}
	if m.dialog != dialogNone {
		t.Error("dialog should close after picking")
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
	m.runClaude = rec.cmd
	m2, _ := m.Update(key("n"))
	m = m2.(Model)
	for _, r := range "~/.cache" {
		m2, _ = m.Update(key(string(r)))
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if rec.dir != sub {
		t.Errorf("typed path resumed in %q, want %q", rec.dir, sub)
	}
}

func TestErrorDialogDismiss(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(claudeMissingMsg{})
	m = m2.(Model)
	if m.dialog != dialogError {
		t.Fatal("claudeMissingMsg should open error dialog")
	}
	m2, _ = m.Update(key("x"))
	m = m2.(Model)
	if m.dialog != dialogNone {
		t.Error("any key should dismiss error dialog")
	}
}
