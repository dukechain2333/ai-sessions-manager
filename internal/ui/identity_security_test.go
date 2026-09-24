package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dukechain2333/ai-sessions-manager/internal/config"
	"github.com/dukechain2333/ai-sessions-manager/internal/iterm2"
	"github.com/dukechain2333/ai-sessions-manager/internal/store"
	"github.com/dukechain2333/ai-sessions-manager/internal/tmux"
)

func TestDeleteKeepsConfirmedIdentityAcrossRescan(t *testing.T) {
	for _, remains := range []bool{true, false} {
		m := newTestModel()
		a := store.Session{ID: "a", Path: "/a.jsonl", Agent: store.AgentClaude}
		b := store.Session{ID: "b", Path: "/b.jsonl", Agent: store.AgentClaude}
		m.list.SetSessions([]store.Session{a, b})
		trashed := ""
		m.trashFn = func(s store.Session) (string, error) { trashed = s.Path; return "", nil }
		next, _ := m.Update(key("d"))
		m = next.(Model)
		reordered := []store.Session{b}
		if remains {
			reordered = append(reordered, a)
		}
		next, _ = m.Update(scanDoneMsg{sessions: reordered})
		m = next.(Model)
		if !strings.Contains(m.dialogView(), "a") {
			t.Fatal("confirmation no longer shows the captured target")
		}
		next, _ = m.Update(key("y"))
		m = next.(Model)
		want := ""
		if remains {
			want = a.Path
		}
		if trashed != want || len(m.list.sessions) != 1 || m.list.sessions[0].ID != b.ID {
			t.Fatalf("target remains=%t: trashed=%q sessions=%v", remains, trashed, m.list.sessions)
		}
	}
}

func TestEnrichmentFollowsPathAfterDeletion(t *testing.T) {
	m := newTestModel()
	a := store.Session{ID: "a", Path: "/a.jsonl", CWD: "/a", Agent: store.AgentClaude}
	b := store.Session{ID: "b", Path: "/b.jsonl", CWD: "/b", Agent: store.AgentClaude}
	m.list.SetSessions([]store.Session{a, b})
	ch := make(chan store.EnrichResult, 3)
	m.enrichCh = ch
	m.trashFn = func(store.Session) (string, error) { return "", nil }
	next, _ := m.Update(key("d"))
	m = next.(Model)
	next, _ = m.Update(key("y"))
	m = next.(Model)
	for _, result := range []store.EnrichResult{
		{Index: 0, Path: a.Path, Meta: store.Meta{CWD: a.CWD, Title: "deleted", UserMessages: 1}},
		{Index: 0, Path: a.Path, Err: errors.New("deleted")},
		{Index: 1, Path: b.Path, Meta: store.Meta{CWD: b.CWD, Title: "survivor", UserMessages: 1}},
	} {
		next, _ = m.Update(enrichMsg{ch: ch, EnrichResult: result})
		m = next.(Model)
	}
	s := m.list.sessions[0]
	if s.ID != b.ID || s.CWD != b.CWD || s.Title != "survivor" || s.Unreadable {
		t.Fatalf("stale index corrupted surviving session: %+v", s)
	}
}

func TestUntrustedTranscriptCannotEmitTerminalCommands(t *testing.T) {
	osc := iterm2.Sequence(iterm2.Launch{Dir: "/tmp", Argv: []string{"codex", "mcp", "add", "untrusted", "--", "/bin/true"}}, false)
	for _, control := range []string{osc, "\x1b]52;c;dW50cnVzdGVk\a", "\x1bPtmux;\x1b\x1b]1337;Custom=id=sm:bad\a\x1b\\", "\x1b[2J", "\x9b2J", "\u009b2J", "\a\r\x00"} {
		m := newTestModel()
		text := "before " + control + " after"
		tr := store.Transcript{Messages: []store.Message{{Kind: store.KindAssistant, Text: text}}}
		m.previewFor = "test"
		m.searchAll, m.activeQuery = true, "after"
		next, _ := m.Update(transcriptMsg{id: "test", t: tr})
		m = next.(Model)
		view := m.View()
		if strings.Contains(view, control) || strings.Contains(view, "\x1b]") || strings.Contains(view, "\x1bP") || strings.Contains(view, "\u009b") {
			t.Fatalf("untrusted terminal command survived: %q", control)
		}
		if !strings.Contains(m.preview.View(), "\x1b[7mafter\x1b[27m") {
			t.Fatal("sanitization removed the application's search highlighting")
		}
		if tr.Messages[0].Text != text {
			t.Fatal("display sanitization mutated cached source text")
		}
	}
}

func TestSessionLabelsStripTerminalCommands(t *testing.T) {
	m := newTestModel()
	osc := "\x1b]52;c;dW50cnVzdGVk\a"
	m.list.SetSessions([]store.Session{{ID: "a", Title: "title" + osc, CWD: "/project" + osc, GitBranch: osc, Agent: store.AgentClaude}})
	if strings.Contains(m.View(), "\x1b]") {
		t.Fatal("list title, project or branch emitted an OSC sequence")
	}
}

func TestAdoptionPreservesNativeWindowKey(t *testing.T) {
	m, sequences := newITerm2Model(t)
	m.tmuxEnabled = true
	dir := t.TempDir()
	m.list.sessions[0].CWD = dir
	pending := tmux.PendingName("claude", 0)
	m.tmux = &fakeTmux{live: map[string]bool{pending: true}, paths: map[string]string{pending: dir}}
	next, _ := m.Update(m.adoptCmd(m.list.sessions)())
	m = next.(Model)
	m.startResume()
	if len(*sequences) != 1 {
		t.Fatalf("expected one attach request, got %d", len(*sequences))
	}
	l := decodeLaunch(t, (*sequences)[0])
	if !l.Attach || l.Name != tmuxNameFor(m.list.sessions[0]) || l.WindowKey != pending {
		t.Fatalf("attach lost original native-window identity: %+v", l)
	}
}

func TestColdLaunchInsideTmuxUsesSilentSwitch(t *testing.T) {
	orig := insideTmux
	insideTmux = func() bool { return true }
	t.Cleanup(func() { insideTmux = orig })
	for _, resume := range []bool{true, false} {
		m := newTestModel()
		m.tmuxEnabled, m.openIn = true, config.OpenInCurrent
		m.list.sessions[0].CWD = t.TempDir()
		called := false
		m.runCmd = func(string, string, ...string) tea.Cmd { t.Fatal("must not attempt nested attachment"); return nil }
		m.runSilent = func(name, dir string, args ...string) tea.Cmd {
			called = true
			if name != "tmux" || dir != m.list.sessions[0].CWD || !strings.Contains(strings.Join(args, " "), "switch-client") {
				t.Fatalf("unexpected tmux launch: %s %s %v", name, dir, args)
			}
			return nil
		}
		if resume {
			m.startResume()
		} else {
			m.launchNewSession(m.list.sessions[0].CWD)
		}
		if !called {
			t.Fatalf("resume=%t did not dispatch detached launch and switch", resume)
		}
	}
}
