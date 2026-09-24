package tmux

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestShort(t *testing.T) {
	if got := Short("ABCD1234-9C8F-43A7-839B-23CECEE584F1"); got != "abcd1234-9c8f-43a7-839b-23cecee584f1" {
		t.Errorf("Short must preserve the complete ID, got %q", got)
	}
	if got := Short("s1"); got != "s1" {
		t.Errorf("Short short id = %q, want s1", got)
	}
}

func TestCodexUUIDv7NamesDoNotCollide(t *testing.T) {
	a := "01a0d221-6dc1-78d2-ae4a-86ae89d8b73c"
	b := "01a0d221-529d-7942-979c-7295b7c7f8b4"
	if Name("codex", Short(a)) == Name("codex", Short(b)) {
		t.Fatal("distinct UUIDv7 sessions must have distinct tmux identities")
	}
	if Short("SessionID") == Short("sessionid") {
		t.Fatal("non-UUID IDs must preserve case-sensitive identity")
	}
	for _, id := range []string{"x.y", "x:y", "x/pending/1", "pending-42", "x-pending-42", "", "a;kill-server"} {
		got := Short(id)
		if !safeID.MatchString(got) || IsPending(Name("codex", got)) {
			t.Errorf("unsafe or pending identity for %q: %q", id, got)
		}
		if Short(got) == got {
			t.Errorf("hashed identity must not alias a literal ID: %q", got)
		}
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
		if os.Getenv("SM_REQUIRE_TMUX_TESTS") == "1" {
			t.Fatal("tmux is required for the integration tests")
		}
		t.Skip("tmux not installed")
	}
	// macOS's default temp path plus the test name can exceed sockaddr_un's
	// path limit. A short private directory also isolates every test server.
	socketDir, err := os.MkdirTemp("/tmp", "sm-tmux-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	t.Setenv("TMUX_TMPDIR", socketDir)
	t.Setenv("TMUX", "") // never nest inside a surrounding tmux
	if out, err := exec.Command("tmux", "-f", "/dev/null", "new-session", "-d", "-s", name, "-c", cwd, "/bin/sh").CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-server").Run() })
}

func waitFor(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for " + what)
}

func attachControlClient(t *testing.T, session string) {
	t.Helper()
	cmd := exec.Command("tmux", "-C", "attach-session", "-t", "="+session)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	waitFor(t, "tmux client", func() bool {
		out, _ := exec.Command("tmux", "list-clients", "-F", "#{client_session}").Output()
		return strings.Contains(string(out), session)
	})
}

func TestSelfWrapExecutablePathWithSpaces(t *testing.T) {
	cwd := t.TempDir()
	startIsolatedTmux(t, "base", cwd)
	attachControlClient(t, "base")
	exe := filepath.Join(cwd, "sm binary's directory", "sm")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(cwd, "started")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nprintf ready > "+quoteArg(marker)+"\nexec /bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Exercise the respawn form so the existing client can attach without
	// needing a test-owned pty. Both branches share executableArgs.
	args := SelfWrapArgs([]string{exe}, cwd, true, "")
	for i, arg := range args {
		if arg == "=sm:" {
			args[i] = "=base:"
		}
		if arg == "=sm" {
			args[i] = "=base"
		}
	}
	// The launch precedes attach-session; execute just that launch in this
	// test client and verify the real executable was reached.
	args = args[:len(args)-4]
	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		t.Fatalf("self wrap: %v: %s", err, out)
	}
	waitFor(t, "sm executable", func() bool { _, err := os.Stat(marker); return err == nil })
}

func TestDetachedArgsCreatesAndSwitchesWithoutNesting(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "resume"}[resume], func(t *testing.T) {
			cwd := t.TempDir()
			startIsolatedTmux(t, "base", cwd)
			attachControlClient(t, "base")
			out, err := exec.Command("tmux", "display-message", "-p", "#{socket_path},#{pid},0").Output()
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("TMUX", strings.TrimSpace(string(out)))
			name := "sm-codex-test"
			args := DetachedArgs(name, cwd, "/bin/sh", nil, resume)
			if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
				t.Fatalf("detached launch: %v: %s", err, out)
			}
			waitFor(t, "client switch", func() bool {
				out, _ := exec.Command("tmux", "list-clients", "-F", "#{client_session}").Output()
				return strings.TrimSpace(string(out)) == name
			})
			if resume {
				// A stale discovery result must reuse the existing session, not
				// try new-session -A's forbidden nested attach.
				if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
					t.Fatalf("resume existing: %v: %s", err, out)
				}
			}
		})
	}
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
