package ui

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dukechain2333/ai-sessions-manager/internal/config"
	"github.com/dukechain2333/ai-sessions-manager/internal/store"
	"github.com/dukechain2333/ai-sessions-manager/internal/tmux"
)

// TestMain makes the resume/new launch paths independent of the runner's
// environment: they check that the agent binary is on PATH, but CI runners
// have no claude/codex installed. Default the lookup to "present" for the
// whole package; the missing-binary branch is exercised explicitly by
// TestResumeErrorsWhenBinaryMissing, which overrides binLookPath itself.
func TestMain(m *testing.M) {
	binLookPath = func(string) error { return nil }
	os.Exit(m.Run())
}

// TestResumeErrorsWhenBinaryMissing covers the branch TestMain stubs away:
// when the agent binary is absent from PATH, resume raises an error dialog
// and does not launch anything.
func TestResumeErrorsWhenBinaryMissing(t *testing.T) {
	orig := binLookPath
	binLookPath = func(string) error { return errors.New("not found") }
	defer func() { binLookPath = orig }()

	m := newTestModel()
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir // real dir, so resume reaches the binary check
	launched := false
	m.runCmd = func(string, string, ...string) tea.Cmd {
		launched = true
		return nil
	}
	m.list.selectSession(0)
	m2, _ := m.startResume()
	m = m2.(Model)
	if m.dialog != dialogError {
		t.Errorf("missing binary should raise an error dialog, got dialog=%v", m.dialog)
	}
	if launched {
		t.Error("resume must not launch anything when the binary is missing")
	}
}

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

func TestUpAtTopRingsBellStaysInList(t *testing.T) {
	m := newTestModel()
	rang := false
	m.bell = func() tea.Msg { rang = true; return nil }
	// Walk up to the very top row.
	for {
		before := m.list.cursor
		m2, _ := m.Update(key("k"))
		m = m2.(Model)
		if m.list.cursor == before {
			break // at the top edge now
		}
	}
	top := m.list.cursor
	m2, cmd := m.Update(key("k")) // one more up, at the edge
	m = m2.(Model)
	if m.focus != focusList {
		t.Fatalf("up at the top must stay in the list, focus=%v", m.focus)
	}
	if m.list.cursor != top {
		t.Errorf("cursor moved at the top edge: %d → %d", top, m.list.cursor)
	}
	if cmd == nil {
		t.Fatal("up at the top must return the bell command")
	}
	cmd()
	if !rang {
		t.Error("up at the top must ring the bell")
	}
}

func TestDownAtBottomRingsBellStaysInList(t *testing.T) {
	m := newTestModel()
	rang := false
	m.bell = func() tea.Msg { rang = true; return nil }
	for {
		before := m.list.cursor
		m2, _ := m.Update(key("j"))
		m = m2.(Model)
		if m.list.cursor == before {
			break // at the bottom edge
		}
	}
	bottom := m.list.cursor
	m2, cmd := m.Update(key("j"))
	m = m2.(Model)
	if m.focus != focusList {
		t.Fatalf("down at the bottom must stay in the list, focus=%v", m.focus)
	}
	if m.list.cursor != bottom {
		t.Errorf("cursor moved at the bottom edge: %d → %d", bottom, m.list.cursor)
	}
	if cmd == nil {
		t.Fatal("down at the bottom must return the bell command")
	}
	cmd()
	if !rang {
		t.Error("down at the bottom must ring the bell")
	}
}

func TestInteriorMoveDoesNotRing(t *testing.T) {
	m := newTestModel()
	rang := false
	m.bell = func() tea.Msg { rang = true; return nil }
	// Ensure we start with room to move down (cursor on the first session/header).
	before := m.list.cursor
	m2, _ := m.Update(key("j"))
	m = m2.(Model)
	if m.list.cursor == before {
		t.Skip("test model has no interior move available")
	}
	if rang {
		t.Error("an interior move must not ring the bell")
	}
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
	windows map[string][2]string // window name → {window id, session name}
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

func (f *fakeTmux) Window(name string) (string, string, bool) {
	w, ok := f.windows[name]
	if !ok {
		return "", "", false
	}
	return w[0], w[1], true
}

func TestAdoptRenamesNewestMatch(t *testing.T) {
	sessions := []store.Session{
		{ID: "old11111", CWD: "/x/alpha", Agent: store.AgentClaude, LastActivity: time.Unix(100, 0)},
		{ID: "new22222", CWD: "/x/alpha", Agent: store.AgentClaude, LastActivity: time.Unix(200, 0)},
	}
	pend := tmux.PendingName("claude", 5)
	f := &fakeTmux{
		live:  map[string]bool{pend: true},
		paths: map[string]string{pend: "/x/alpha"},
	}
	set := map[string]bool{pend: true}
	adoptPending(f, sessions, set)
	want := tmux.Name("claude", tmux.Short("new22222"))
	if len(f.renamed) != 1 || f.renamed[0][1] != want {
		t.Errorf("expected rename to %s, got %v", want, f.renamed)
	}
	if !set[want] || set[pend] {
		t.Errorf("set should swap pending for adopted: %v", set)
	}
}

// A new-session tmux must never adopt a session that was already idle when the
// tmux was created: the real session has no transcript yet at adopt time, and
// claiming the newest pre-existing one hijacks an unrelated conversation.
func TestAdoptSkipsSessionsOlderThanPending(t *testing.T) {
	start := time.Unix(1000, 0)
	sessions := []store.Session{
		{ID: "stale111", CWD: "/x/alpha", Agent: store.AgentClaude, LastActivity: start.Add(-5 * time.Second)},
	}
	pend := tmux.PendingName("claude", start.UnixNano())
	f := &fakeTmux{
		live:  map[string]bool{pend: true},
		paths: map[string]string{pend: "/x/alpha"},
	}
	set := map[string]bool{pend: true}
	adoptPending(f, sessions, set)
	if len(f.renamed) != 0 {
		t.Errorf("must not adopt a session older than the pending tmux, got %v", f.renamed)
	}
	if !set[pend] {
		t.Errorf("pending should stay pending until its real session appears: %v", set)
	}
}

// Once the real session writes its transcript, it is adopted even though an
// older session in the same cwd is still unbacked.
func TestAdoptPicksSessionNewerThanPending(t *testing.T) {
	start := time.Unix(1000, 0)
	sessions := []store.Session{
		{ID: "stale111", CWD: "/x/alpha", Agent: store.AgentClaude, LastActivity: start.Add(-5 * time.Second)},
		{ID: "real2222", CWD: "/x/alpha", Agent: store.AgentClaude, LastActivity: start.Add(2 * time.Second)},
	}
	pend := tmux.PendingName("claude", start.UnixNano())
	f := &fakeTmux{
		live:  map[string]bool{pend: true},
		paths: map[string]string{pend: "/x/alpha"},
	}
	set := map[string]bool{pend: true}
	adoptPending(f, sessions, set)
	want := tmux.Name("claude", tmux.Short("real2222"))
	if len(f.renamed) != 1 || f.renamed[0][1] != want {
		t.Errorf("expected rename to %s, got %v", want, f.renamed)
	}
}

func TestAdoptSkipsBacked(t *testing.T) {
	backed := tmux.Name("claude", tmux.Short("new22222"))
	sessions := []store.Session{
		{ID: "new22222", CWD: "/x/alpha", Agent: store.AgentClaude, LastActivity: time.Unix(200, 0)},
	}
	pend := tmux.PendingName("claude", 5)
	f := &fakeTmux{
		live:  map[string]bool{pend: true, backed: true},
		paths: map[string]string{pend: "/x/alpha"},
	}
	set := map[string]bool{pend: true, backed: true}
	adoptPending(f, sessions, set)
	if len(f.renamed) != 0 {
		t.Errorf("should not adopt an already-backed session, got %v", f.renamed)
	}
}

func TestAdoptNoMatchIsNoop(t *testing.T) {
	sessions := []store.Session{
		{ID: "z", CWD: "/x/beta", Agent: store.AgentClaude, LastActivity: time.Unix(200, 0)},
	}
	pend := tmux.PendingName("claude", 5)
	f := &fakeTmux{live: map[string]bool{pend: true}, paths: map[string]string{pend: "/x/other"}}
	set := map[string]bool{pend: true}
	adoptPending(f, sessions, set)
	if len(f.renamed) != 0 {
		t.Errorf("no cwd match should be a no-op, got %v", f.renamed)
	}
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

// drainCmd runs a command and recursively flattens tea.Batch results into the
// list of concrete messages produced.
func drainCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, drainCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestAdoptionRunsAgainstEnrichedSessionsOnEnrichDone(t *testing.T) {
	m := newTestModel() // s1 claude, CWD /x/alpha, already enriched
	m.tmuxEnabled = true
	pend := tmux.PendingName("claude", 7)
	f := &fakeTmux{live: map[string]bool{pend: true}, paths: map[string]string{pend: "/x/alpha"}}
	m.tmux = f
	// enrichDone must dispatch adoption against the ENRICHED list (CWD known),
	// renaming the pending tmux to s1's real name.
	m2, cmd := m.Update(enrichDoneMsg{ch: m.enrichCh})
	_ = m2
	if cmd == nil {
		t.Fatal("enrichDone should return a cmd when tmux is enabled")
	}
	for _, msg := range drainCmd(cmd) {
		_ = msg // running the batch executes adoptCmd's closure (side effects on f)
	}
	want := tmux.Name("claude", tmux.Short("s1"))
	if len(f.renamed) == 0 || f.renamed[len(f.renamed)-1][1] != want {
		t.Fatalf("adoption should rename %s -> %s against enriched sessions; renamed=%v", pend, want, f.renamed)
	}
}

// newTwoAgentModel is newTestModel with a second (codex) provider and a
// mixed-agent session set, for mode/view tests. Starts in list mode.
//
// The claude dir is a real (empty) tempdir rather than the usual fake
// "/nonexistent-projects-dir" literal: defaultTabView reads
// providers[0].Available() to decide the first tab, and this fixture wants
// the ordinary "both agents present" case (default tab = claude), not the
// "claude not installed" edge case that TestStartupModeFromConfig covers
// directly. No session data comes from a real scan here either way — it's
// injected below via scanDoneMsg.
func newTwoAgentModel(t *testing.T) Model {
	t.Helper()
	m := New(t.TempDir(), "/nonexistent-codex-dir", config.Default())
	m.providers = append(m.providers, store.NewCodexProvider(t.TempDir()))
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(Model)
	m2, _ = m.Update(scanDoneMsg{sessions: mixedSessions()})
	return m2.(Model)
}

func TestVKeyTogglesViewMode(t *testing.T) {
	m := newTwoAgentModel(t)
	if m.list.Agent() != "" {
		t.Fatalf("default agent=%q, want list mode / mixed", m.list.Agent())
	}
	m2, _ := m.Update(key("v"))
	m = m2.(Model)
	if m.list.Agent() != store.AgentClaude {
		t.Errorf("v: agent=%q, want tab mode / claude", m.list.Agent())
	}
	// The claude view lands on the same newest session the mixed list was
	// previewing, so no reload is issued — the preview must simply still
	// track the selection.
	if s, _, ok := m.list.Selected(); !ok || s.ID != m.previewFor {
		t.Errorf("preview tracks %q but selection is %v (ok=%v)", m.previewFor, s.ID, ok)
	}
	m2, _ = m.Update(key("v"))
	m = m2.(Model)
	if m.list.Agent() != "" {
		t.Errorf("v v: agent=%q, want list mode / mixed", m.list.Agent())
	}
}

func TestAKeyPerMode(t *testing.T) {
	m := newTwoAgentModel(t)
	before := m.list.groupByAgent
	m2, _ := m.Update(key("a"))
	m = m2.(Model)
	if m.list.groupByAgent == before {
		t.Error("list mode `a` must toggle agent subgrouping")
	}
	flag := m.list.groupByAgent
	m2, _ = m.Update(key("v"))
	m = m2.(Model)
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	if m.list.Agent() != store.AgentCodex {
		t.Error("tab mode `a` must switch to the codex view")
	}
	if m.list.groupByAgent != flag {
		t.Error("tab mode `a` must not touch subgrouping")
	}
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	if m.list.Agent() != store.AgentClaude {
		t.Error("tab mode `a` must switch back to claude")
	}
}

func TestTabViewRememberedAcrossModes(t *testing.T) {
	m := newTwoAgentModel(t)
	m2, _ := m.Update(key("v")) // tabs: claude
	m = m2.(Model)
	m2, _ = m.Update(key("a")) // codex
	m = m2.(Model)
	m2, _ = m.Update(key("v")) // back to list
	m = m2.(Model)
	if m.list.Agent() != "" {
		t.Fatalf("list mode agent = %q, want mixed", m.list.Agent())
	}
	m2, _ = m.Update(key("v")) // tabs again
	m = m2.(Model)
	if m.list.Agent() != store.AgentCodex {
		t.Errorf("re-entering tab mode = %q, want the remembered codex view", m.list.Agent())
	}
}

func TestTitleTabsOnlyInTabMode(t *testing.T) {
	m := newTwoAgentModel(t)
	if v := m.View(); !strings.Contains(v, "4 sessions") || strings.Contains(v, "[Claude") {
		t.Errorf("list mode title must keep the plain count:\n%s", v)
	}
	m2, _ := m.Update(key("v"))
	m = m2.(Model)
	v := m.View()
	if !strings.Contains(v, "[Claude 2]") || !strings.Contains(v, "Codex 2") {
		t.Errorf("tab mode title must show both tabs, active bracketed:\n%s", v)
	}
	if strings.Contains(v, "sessions") {
		t.Errorf("tab mode title must not show the old count string:\n%s", v)
	}
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	if v := m.View(); !strings.Contains(v, "[Codex 2]") || !strings.Contains(v, "Claude 2") {
		t.Errorf("codex view must bracket the codex tab:\n%s", v)
	}
}

func TestTitleTabsNeedTwoProviders(t *testing.T) {
	m := newTestModel() // single provider
	m2, _ := m.Update(key("v"))
	m = m2.(Model)
	if m.list.Agent() == "" {
		t.Fatal("v should still enter tab mode with one provider")
	}
	if v := m.View(); strings.Contains(v, "[Claude") || !strings.Contains(v, "2 sessions") {
		t.Errorf("single-provider tab mode keeps the plain count:\n%s", v)
	}
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	if m.list.Agent() != store.AgentClaude {
		t.Error("single provider: tab-mode `a` must be a no-op")
	}
}

func TestStartupModeFromConfig(t *testing.T) {
	cfg := config.Default()
	cfg.View = "tabs"
	m := New("/nonexistent-projects-dir", "/nonexistent-codex-dir", cfg)
	if m.list.Agent() != store.AgentClaude {
		t.Errorf("config view=tabs: agent=%q, want the claude tab view", m.list.Agent())
	}
	m = New("/nonexistent-projects-dir", t.TempDir(), cfg) // codex registers, claude dir missing
	if m.list.Agent() != store.AgentCodex {
		t.Errorf("claude dir missing: startup tab view = %q, want codex", m.list.Agent())
	}
}

func TestChromeColorsFollowActiveView(t *testing.T) {
	m := newTwoAgentModel(t)
	m2, _ := m.Update(key("v"))
	m = m2.(Model)
	m2, _ = m.Update(key("a")) // codex view
	m = m2.(Model)
	// Even with nothing selected the color keys off the view, not the
	// selection — the branch the old selected-session logic gets wrong.
	m.list.SetFilter("zzzz-no-match")
	if _, _, ok := m.list.Selected(); ok {
		t.Fatal("setup: filter should leave no selection")
	}
	if m.focusedBorderColor() != m.st.CodexAccent {
		t.Error("empty codex view should still give the teal border color")
	}
}

func TestSwitchKeepsFilterApplied(t *testing.T) {
	m := newTwoAgentModel(t)
	m2, _ := m.Update(key("v"))
	m = m2.(Model)
	m2, _ = m.Update(key("/"))
	m = m2.(Model)
	for _, r := range "rollout" {
		m2, _ = m.Update(key(string(r)))
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // back to list focus
	m = m2.(Model)
	if got := m.list.Len(); got != 0 {
		t.Fatalf("claude view filtered by 'rollout': Len = %d, want 0", got)
	}
	m2, _ = m.Update(key("a"))
	m = m2.(Model)
	if got := m.list.Len(); got != 2 {
		t.Errorf("codex view must re-apply the live filter: Len = %d, want 2", got)
	}
}

// TestFirstListVisitAfterTabsStartupSelectsSession covers a startup-only
// regression: New() calls setAgentView(tabView) before any sessions exist,
// which parks a zero viewState under the "" (mixed list) key. The user's
// first real `v` into the mixed list then finds that key already "seen" and
// skips cursorToFirstSession(), landing on row 0 — a project header — with
// no session selected and a cleared preview.
func TestFirstListVisitAfterTabsStartupSelectsSession(t *testing.T) {
	cfg := config.Default()
	cfg.View = "tabs"
	m := New(t.TempDir(), "/nonexistent-codex-dir", cfg)
	m.providers = append(m.providers, store.NewCodexProvider(t.TempDir()))
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(Model)
	m2, _ = m.Update(scanDoneMsg{sessions: mixedSessions()})
	m = m2.(Model)
	m2, _ = m.Update(key("v")) // first visit to the mixed list this run
	m = m2.(Model)
	if _, _, ok := m.list.Selected(); !ok {
		t.Error("first mixed-list visit after a tabs startup should select a session, not a header")
	}
}

// TestTitleRowTruncatesToWidth guards the header line built in View() by
// lipgloss.JoinHorizontal: with no width clamp, a long indexing/scanning
// status suffix can push the row past the terminal width and wrap, which
// corrupts the alt-screen frame (the help bar was clamped for the same
// reason).
func TestTitleRowTruncatesToWidth(t *testing.T) {
	m := newTwoAgentModel(t)
	m2, _ := m.Update(key("v"))
	m = m2.(Model)
	m.indexing = true
	m.indexDone, m.indexTotal = 100, 200
	m.indexFailed = 5
	m.loading = true
	m2, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 30})
	m = m2.(Model)
	first := strings.SplitN(m.View(), "\n", 2)[0]
	if w := lipgloss.Width(first); w > 44 {
		t.Errorf("title row width = %d, must be clamped to 44", w)
	}
}

func TestVBeforeScanThenBackSelectsSession(t *testing.T) {
	m := New(t.TempDir(), "/nonexistent-codex-dir", config.Default())
	m.providers = append(m.providers, store.NewCodexProvider(t.TempDir()))
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(Model)
	m2, _ = m.Update(key("v")) // the user is faster than the first scan
	m = m2.(Model)
	m2, _ = m.Update(scanDoneMsg{sessions: mixedSessions()})
	m = m2.(Model)
	m2, _ = m.Update(key("v")) // back to the mixed list
	m = m2.(Model)
	if _, _, ok := m.list.Selected(); !ok {
		t.Error("returning to the pre-scan-parked mixed list must select a session")
	}
}

func TestKillProjectScopedToActiveView(t *testing.T) {
	m := newTwoAgentModel(t)
	claudeName := tmuxNameFor(m.list.sessions[0]) // s1: alpha, claude
	codexName := tmuxNameFor(m.list.sessions[3])  // x1: alpha, codex
	ft := &fakeTmux{live: map[string]bool{claudeName: true, codexName: true}, paths: map[string]string{}}
	m.tmux = ft
	m2, _ := m.Update(key("v")) // claude tab view
	m = m2.(Model)
	m.killProjectCmd("alpha", m.list.Agent())()
	if len(ft.killed) != 1 || ft.killed[0] != claudeName {
		t.Errorf("claude view kill set = %v, want only %s", ft.killed, claudeName)
	}
	if !ft.live[codexName] {
		t.Error("the hidden codex tmux must survive a claude-view project kill")
	}
	// The mixed list keeps the project-wide kill.
	m2, _ = m.Update(key("v"))
	m = m2.(Model)
	m.killProjectCmd("alpha", m.list.Agent())()
	if ft.live[codexName] {
		t.Error("the mixed list's project kill should cover both agents")
	}
}
