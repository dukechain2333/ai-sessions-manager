package tmux

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestShort(t *testing.T) {
	if got := Short("ABCD1234-9c8f-43a7"); got != "abcd1234" {
		t.Errorf("Short = %q, want abcd1234", got)
	}
	if got := Short("s1"); got != "s1" {
		t.Errorf("Short short id = %q, want s1", got)
	}
}

func TestName(t *testing.T) {
	if got := Name("claude", "abcd1234"); got != "sm-claude-abcd1234" {
		t.Errorf("Name = %q", got)
	}
}

func TestPending(t *testing.T) {
	n := PendingName("codex", 42)
	if n != "sm-codex-pending-42" {
		t.Errorf("PendingName = %q", n)
	}
	if !IsPending(n) {
		t.Error("IsPending should be true for a pending name")
	}
	if IsPending("sm-codex-abcd1234") {
		t.Error("IsPending should be false for a normal name")
	}
	if got := PendingAgent(n); got != "codex" {
		t.Errorf("PendingAgent = %q, want codex", got)
	}
}

func TestPendingNonce(t *testing.T) {
	got, ok := PendingNonce(PendingName("codex", 42))
	if !ok || got != 42 {
		t.Errorf("PendingNonce = %d, %v; want 42, true", got, ok)
	}
	if _, ok := PendingNonce("sm-codex-abcd1234"); ok {
		t.Error("PendingNonce should not parse a non-pending name")
	}
	if _, ok := PendingNonce("sm-codex-pending-nope"); ok {
		t.Error("PendingNonce should not parse a non-numeric nonce")
	}
}

func TestResumeArgs(t *testing.T) {
	got := ResumeArgs("sm-claude-s1", "/x/alpha", "claude", []string{"--resume", "s1"})
	want := []string{"new-session", "-A", "-s", "sm-claude-s1", "-c", "/x/alpha", "claude", "--resume", "s1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResumeArgs = %v", got)
	}
}

func TestNewArgs(t *testing.T) {
	got := NewArgs("sm-codex-pending-42", "/x/beta", "codex", nil)
	want := []string{"new-session", "-s", "sm-codex-pending-42", "-c", "/x/beta", "codex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewArgs = %v", got)
	}
}

func TestWindowArgs(t *testing.T) {
	got := WindowArgs("sm-claude-s1", "/x/alpha", "claude", []string{"--resume", "s1"})
	want := []string{"new-window", "-c", "/x/alpha", "-n", "sm-claude-s1", "claude", "--resume", "s1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WindowArgs named = %v", got)
	}
	got = WindowArgs("", "/x/beta", "codex", nil)
	want = []string{"new-window", "-c", "/x/beta", "codex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WindowArgs unnamed = %v", got)
	}
}

func TestParseList(t *testing.T) {
	out := "sm-claude-s1\nother-session\nsm-codex-pending-9\n\n"
	got := parseList(out)
	if !got["sm-claude-s1"] || !got["sm-codex-pending-9"] {
		t.Errorf("parseList missing sm- names: %v", got)
	}
	if got["other-session"] {
		t.Error("parseList should drop non-sm names")
	}
	if len(got) != 2 {
		t.Errorf("parseList size = %d, want 2", len(got))
	}
}

func TestParseWindows(t *testing.T) {
	out := "@1\tmain\tsm-claude-s1\n@2\tmain\tvim\n@3\twork\tsm-codex-pending-9\n\n"
	got := parseWindows(out)
	if w := got["sm-claude-s1"]; w != [2]string{"@1", "main"} {
		t.Errorf("sm-claude-s1 = %v, want {@1 main}", w)
	}
	if w := got["sm-codex-pending-9"]; w != [2]string{"@3", "work"} {
		t.Errorf("sm-codex-pending-9 = %v, want {@3 work}", w)
	}
	if _, ok := got["vim"]; ok {
		t.Error("parseWindows should drop non-sm window names")
	}
	if len(got) != 2 {
		t.Errorf("parseWindows size = %d, want 2", len(got))
	}
}

func TestSelfWrapArgs(t *testing.T) {
	self := []string{"/usr/local/bin/sm", "--config", "/x/c.json"}
	got := SelfWrapArgs(self, "/work", false, "")
	want := []string{"new-session", "-A", "-s", "sm", "-n", "sm", "-c", "/work",
		"/usr/local/bin/sm", "--config", "/x/c.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fresh session = %v", got)
	}
	got = SelfWrapArgs(self, "", false, "")
	want = []string{"new-session", "-A", "-s", "sm", "-n", "sm",
		"/usr/local/bin/sm", "--config", "/x/c.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("empty cwd must drop -c, got %v", got)
	}
	got = SelfWrapArgs(self, "/work", true, "@3")
	want = []string{"select-window", "-t", "@3", ";", "attach-session", "-t", "=sm"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reattach = %v", got)
	}
	got = SelfWrapArgs(self, "/work", true, "")
	want = []string{"new-window", "-t", "=sm:", "-n", "sm", "-c", "/work",
		"/usr/local/bin/sm", "--config", "/x/c.json", ";", "attach-session", "-t", "=sm"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("respawn = %v", got)
	}
}

func TestParseSelfWindow(t *testing.T) {
	out := "@1\tvim\n@2\tsm\n"
	if got := parseSelfWindow(out); got != "@2" {
		t.Errorf("parseSelfWindow = %q, want @2", got)
	}
	if got := parseSelfWindow("@1\tother\n\n"); got != "" {
		t.Errorf("no sm window should yield empty, got %q", got)
	}
}

// startIsolatedTmux points every tmux invocation in this test at a private
// socket dir and starts one detached session there. The user's real tmux
// server is never touched. Skips when tmux is not installed.
func startIsolatedTmux(t *testing.T, name, cwd string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX", "") // never nest inside a surrounding tmux
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", cwd).CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-server").Run() })
}

// Regression: on tmux 3.4, display-message -t "=name" is parsed as a
// target-pane and silently yields "" (exit 0), which made adoption skip
// every pending session forever. The session form must resolve exactly
// AND return the real pane_current_path.
func TestExecPathSessionForm(t *testing.T) {
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := "sm-claude-pending-42"
	startIsolatedTmux(t, name, cwd)
	got, err := Exec{}.Path(name)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != cwd {
		t.Errorf("Path = %q, want %q", got, cwd)
	}
}

// Exact match must hold: a name that is only a prefix of a live session
// (no exact match) must not resolve to that session's path.
func TestExecPathExactMatch(t *testing.T) {
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	startIsolatedTmux(t, "sm-claude-pending-425", cwd)
	if got, err := (Exec{}).Path("sm-claude-pending-42"); err == nil && got == cwd {
		t.Errorf("Path resolved a prefix to %q; want no match", got)
	}
}
