package ui

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/ai-sessions-manager/internal/config"
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
	m2, cmd := m.Update(agentExitMsg{err: exitErr})
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
	m2, _ := m.Update(agentExitMsg{err: errors.New(`exec: "claude": executable file not found in $PATH`)})
	m = m2.(Model)
	if m.dialog != dialogError {
		t.Error("a genuine launch failure should show the error dialog")
	}
}

func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestNewBuildsProviders(t *testing.T) {
	m := New("/nope/claude", "/nope/codex", config.Default())
	if len(m.providers) == 0 {
		t.Error("expected at least the claude provider")
	}
}

func newTestModel() Model {
	m := New("/nonexistent-projects-dir", "/nonexistent-codex-dir", config.Default())
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

func newTmuxModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	m := newTestModel()
	m.tmuxEnabled = true
	captured := &[]string{}
	m.runCmd = func(name, dir string, args ...string) tea.Cmd {
		*captured = append([]string{name, dir}, args...)
		return nil
	}
	m.now = func() time.Time { return time.Unix(0, 1234) }
	return m, captured
}

func TestResumeWrapsInTmuxWhenEnabled(t *testing.T) {
	m, cap := newTmuxModel(t)
	dir := t.TempDir() // startResume stats CWD; it must exist or it opens the picker
	m.list.sessions[0].CWD = dir
	m.list.selectSession(0) // s1, claude
	m.startResume()
	got := *cap
	if len(got) < 3 || got[0] != "tmux" {
		t.Fatalf("resume should run tmux, got %v", got)
	}
	// tmux new-session -A -s sm-claude-s1 -c <dir> claude --resume s1
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "new-session -A -s sm-claude-s1 -c "+dir+" claude --resume s1") {
		t.Errorf("resume argv = %v", got)
	}
}

func TestNewWrapsInTmuxWhenEnabled(t *testing.T) {
	m, cap := newTmuxModel(t)
	m.launchNewSession("/x/alpha") // single provider (claude-only test model)
	got := *cap
	if len(got) < 3 || got[0] != "tmux" {
		t.Fatalf("new should run tmux, got %v", got)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "new-session -s sm-claude-pending-1234 -c /x/alpha claude") {
		t.Errorf("new argv = %v", got)
	}
}

func TestResumeStaysInlineWhenDisabled(t *testing.T) {
	m := newTestModel() // tmux disabled
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir // else startResume opens the dir picker
	captured := &[]string{}
	m.runCmd = func(name, dir string, args ...string) tea.Cmd {
		*captured = append([]string{name, dir}, args...)
		return nil
	}
	m.list.selectSession(0)
	m.startResume()
	if len(*captured) == 0 || (*captured)[0] != "claude" {
		t.Errorf("disabled resume should run claude directly, got %v", *captured)
	}
}

func TestTmuxMissingAtStartupDisablesAndWarns(t *testing.T) {
	orig := tmuxLookPath
	tmuxLookPath = func() bool { return false }
	defer func() { tmuxLookPath = orig }()
	cfg := config.Default()
	cfg.TmuxEnabled = true
	m := New("/nope", "/nope", cfg)
	if m.tmuxEnabled {
		t.Error("missing tmux should disable integration")
	}
	if m.dialog != dialogError {
		t.Error("missing tmux should raise a startup error dialog")
	}
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

func TestBottomLabelShowsSelectedProject(t *testing.T) {
	m := newTestModel() // grouped; cursor starts on the first session (s1, project "alpha")
	if got := m.projectLabelText(); got != " ▸ alpha  " {
		t.Fatalf("projectLabelText = %q, want %q", got, " ▸ alpha  ")
	}
	if v := m.View(); !strings.Contains(v, "▸ alpha") {
		t.Errorf("View bottom line missing project label:\n%s", v)
	}
	// Move the cursor onto the project header (no session selected) → empty label.
	m2, _ := m.Update(key("k"))
	m = m2.(Model)
	if !m.list.OnHeader() {
		t.Fatal("setup: k from first session should land on the header")
	}
	if got := m.projectLabelText(); got != "" {
		t.Errorf("projectLabelText on a header = %q, want empty", got)
	}
}

func TestTmuxListMsgUpdatesMarkers(t *testing.T) {
	m := newTestModel()
	name := tmuxNameFor(m.list.sessions[0])
	m2, _ := m.Update(tmuxListMsg{set: map[string]bool{name: true}})
	m = m2.(Model)
	if !m.tmuxLive[name] {
		t.Error("model should store the live set")
	}
	if !m.list.projectHasLiveTmux(m.list.sessions[0].Project()) {
		t.Error("list pane should see the live tmux after tmuxListMsg")
	}
}

func TestFocusedBorderAndLabelColorFollowAgent(t *testing.T) {
	m := newTestModel() // grouped; cursor on first session s1 (claude, project "alpha")
	if m.focusedBorderColor() != m.st.Accent {
		t.Error("claude selection should give the coral border color")
	}
	if m.projectLabelColor() != m.st.Accent {
		t.Error("claude-majority project should give the coral label color")
	}
	// Flip the selected session and its whole project to codex, refresh, re-select.
	for i := range m.list.sessions {
		if m.list.sessions[i].CWD == "/x/alpha" {
			m.list.sessions[i].Agent = store.AgentCodex
		}
	}
	m.list.SetSessions(m.list.sessions)
	if m.focusedBorderColor() != m.st.CodexAccent {
		t.Error("codex selection should give the teal border color")
	}
	if m.projectLabelColor() != m.st.CodexAccent {
		t.Error("codex-majority project should give the teal label color")
	}
}

type fakeTmux struct {
	live    map[string]bool
	paths   map[string]string
	killed  []string
	renamed [][2]string
}

func (f *fakeTmux) List() (map[string]bool, error) {
	cp := map[string]bool{}
	for k, v := range f.live {
		cp[k] = v
	}
	return cp, nil
}
func (f *fakeTmux) Path(name string) (string, error) { return f.paths[name], nil }
func (f *fakeTmux) Kill(name string) error {
	delete(f.live, name)
	f.killed = append(f.killed, name)
	return nil
}
func (f *fakeTmux) Rename(from, to string) error {
	delete(f.live, from)
	f.live[to] = true
	f.renamed = append(f.renamed, [2]string{from, to})
	return nil
}

func TestKillOneSession(t *testing.T) {
	m := newTestModel()
	m.tmuxEnabled = true
	name := tmuxNameFor(m.list.sessions[0])
	f := &fakeTmux{live: map[string]bool{name: true}}
	m.tmux = f
	m.tmuxLive = map[string]bool{name: true}
	m.list.SetTmuxLive(m.tmuxLive)
	m.list.selectSession(0)
	m2, cmd := m.Update(key("x"))
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("x on a live session should return a refresh cmd")
	}
	cmd() // runs killOneCmd's goroutine body
	if len(f.killed) != 1 || f.killed[0] != name {
		t.Errorf("expected kill of %s, got %v", name, f.killed)
	}
}

func TestKillProjectHeaderConfirms(t *testing.T) {
	m := newTestModel()
	m.tmuxEnabled = true
	n0 := tmuxNameFor(m.list.sessions[0]) // alpha
	f := &fakeTmux{live: map[string]bool{n0: true}}
	m.tmux = f
	m.tmuxLive = map[string]bool{n0: true}
	m.list.SetTmuxLive(m.tmuxLive)
	// Put the cursor on the alpha header.
	m.list.selectSession(0)
	for !m.list.OnHeader() {
		m.list.MoveCursor(-1)
	}
	m2, _ := m.Update(key("x"))
	m = m2.(Model)
	if m.dialog != dialogKillProject {
		t.Fatalf("x on a header should open the kill-project confirm, got %v", m.dialog)
	}
	m2, cmd := m.Update(key("y"))
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("confirming should return a kill cmd")
	}
	cmd()
	if len(f.killed) != 1 || f.killed[0] != n0 {
		t.Errorf("kill-all should have killed %s, got %v", n0, f.killed)
	}
}

func TestKillHelpItemGatedByTmux(t *testing.T) {
	m := newTestModel() // disabled
	for _, it := range m.helpItems() {
		if it.label == "x kill" {
			t.Fatal("x kill must not show when tmux is disabled")
		}
	}
	m.tmuxEnabled = true
	found := false
	for _, it := range m.helpItems() {
		if it.label == "x kill" {
			found = true
		}
	}
	if !found {
		t.Error("x kill should show when tmux is enabled")
	}
}
