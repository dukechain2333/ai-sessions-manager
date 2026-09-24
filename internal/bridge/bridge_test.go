package bridge

import (
	"bufio"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dukechain2333/ai-sessions-manager/internal/iterm2"
)

func TestQuoteAlwaysHard(t *testing.T) {
	if got := Quote("=sm-claude-abc"); got != `'=sm-claude-abc'` {
		t.Fatalf("Quote left the = word soft: %s", got)
	}
	if got := Quote("it's"); got != `'it'\''s'` {
		t.Fatalf("embedded quote not escaped: %s", got)
	}
}

func TestLineAttach(t *testing.T) {
	key, line, err := Line(iterm2.Launch{Name: "sm-claude-abc123", Attach: true}, "mybox", nil)
	if err != nil {
		t.Fatal(err)
	}
	if key != "mybox|sm-claude-abc123" {
		t.Fatalf("key = %q", key)
	}
	want := `ssh -t -o RemoteCommand=none -o ClearAllForwardings=yes -- mybox 'exec tmux attach-session -t '\''=sm-claude-abc123'\'''`
	if line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}
}

func TestLineTmuxFormWithBinDir(t *testing.T) {
	l := iterm2.Launch{
		Dir:    "/home/w/proj",
		Name:   "sm-claude-abc123",
		Argv:   []string{"claude", "--resume", "abc"},
		Tmux:   true,
		BinDir: "/home/w/.npm-global/bin",
	}
	_, line, err := Line(l, "mybox", nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd := `export PATH='/home/w/.npm-global/bin':"$PATH" && ` +
		`cd '/home/w/proj' && exec tmux new-session -A -s 'sm-claude-abc123' -c '/home/w/proj' 'claude' '--resume' 'abc'`
	if want := "ssh -t -o RemoteCommand=none -o ClearAllForwardings=yes -- mybox " + Quote(cmd); line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}
}

func TestLineLocalIgnoresBinDirAndHost(t *testing.T) {
	l := iterm2.Launch{
		Host:   "attacker-controlled",
		Dir:    "/home/w/proj",
		Argv:   []string{"codex", "resume", "abc"},
		BinDir: "/tmp/evil",
	}
	key, line, err := Line(l, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(line, "ssh") || strings.Contains(line, "attacker") {
		t.Fatalf("local line must not ssh anywhere: %q", line)
	}
	if strings.Contains(line, "PATH") {
		t.Fatalf("local line must ignore bindir: %q", line)
	}
	if want := `cd '/home/w/proj' && exec 'codex' 'resume' 'abc'`; line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}
	if key != "|/home/w/proj 'codex' 'resume' 'abc'" {
		t.Fatalf("untracked key should include dir+argv: %q", key)
	}
}

func TestLinePayloadHostNeverUsed(t *testing.T) {
	l := iterm2.Launch{Host: "evil.example", Name: "sm-claude-abc", Attach: true}
	_, line, err := Line(l, "trusted", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(line, "evil.example") || !strings.Contains(line, "-- trusted ") {
		t.Fatalf("payload host leaked into line: %q", line)
	}
}

func TestLineExtraSSHArgsQuoted(t *testing.T) {
	l := iterm2.Launch{Name: "sm-claude-abc", Attach: true}
	_, line, err := Line(l, "mybox", []string{"-p", "2222"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, `ssh -t -o RemoteCommand=none -o ClearAllForwardings=yes '-p' '2222' -- mybox `) {
		t.Fatalf("extra args not carried: %q", line)
	}
}

func TestAgentCommandShapes(t *testing.T) {
	for _, argv := range [][]string{
		{"claude"}, {"codex"},
		{"claude", "--resume", "abc-123"}, {"codex", "resume", "abc-123"},
	} {
		if _, _, err := Line(iterm2.Launch{Dir: "/tmp", Argv: argv}, "", nil); err != nil {
			t.Errorf("valid argv %q rejected: %v", argv, err)
		}
	}
	for _, argv := range [][]string{
		{"codex", "mcp", "add", "evil", "--", "/bin/sh"},
		{"codex", "exec", "payload"},
		{"codex", "-c", "mcp_servers.evil.command=/bin/sh"},
		{"claude", "--mcp-config", "/tmp/evil.json"},
		{"claude", "--dangerously-skip-permissions"},
		{"codex", "resume", "--last"},
		{"claude", "--resume", "--dangerously-skip-permissions"},
		{"codex", "resume", "id", "--dangerously-bypass-approvals-and-sandbox"},
		{"claude", "--resume", "id\n"},
		{"codex", "resume", "=sh"},
		{"codex", "resume", "/tmp/session"},
	} {
		if _, _, err := Line(iterm2.Launch{Dir: "/tmp", Argv: argv}, "", nil); err == nil {
			t.Errorf("unsafe argv accepted: %q", argv)
		}
	}
}

func TestWindowKeySurvivesSessionAdoption(t *testing.T) {
	pending := "sm-claude-pending-123"
	key, _, err := Line(iterm2.Launch{Dir: "/tmp", Name: pending, Argv: []string{"claude"}, Tmux: true}, "mybox", nil)
	if err != nil {
		t.Fatal(err)
	}
	adoptedKey, line, err := Line(iterm2.Launch{Name: "sm-claude-abc123", WindowKey: pending, Attach: true}, "mybox", nil)
	if err != nil {
		t.Fatal(err)
	}
	if key != adoptedKey || strings.Contains(line, pending) || !strings.Contains(line, "=sm-claude-abc123") {
		t.Fatalf("adoption changed window identity or command target: key=%q, adopted=%q, line=%q", key, adoptedKey, line)
	}
	for _, invalid := range []string{"other", "sm-abc\n", "sm-abc;echo"} {
		if _, _, err := Line(iterm2.Launch{Name: "sm-claude-abc123", WindowKey: invalid, Attach: true}, "mybox", nil); err == nil {
			t.Errorf("accepted invalid window key %q", invalid)
		}
	}
}

// ssh -G evaluates real OpenSSH option precedence without connecting anywhere.
func TestWindowSSHDoesNotInheritLoginCommandOrForwardings(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("OpenSSH not installed")
	}
	config := filepath.Join(t.TempDir(), "ssh_config")
	if err := os.WriteFile(config, []byte("Host mybox\n HostName 192.0.2.1\n RemoteCommand tmux attach\n LocalForward 18080 127.0.0.1:8080\n ExitOnForwardFailure yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, line, err := Line(iterm2.Launch{Name: "sm-claude-abc", Attach: true}, "mybox", []string{
		"-p", "2222", "-o", "RemoteCommand=echo wrong", "-o", "ClearAllForwardings=no", "-L", "18081:127.0.0.1:8081",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := exec.Command("sh", "-c", `ssh() { command ssh -G -F "$SM_TEST_SSH_CONFIG" "$@"; }; `+line)
	c.Env = append(os.Environ(), "SM_TEST_SSH_CONFIG="+config)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("ssh option evaluation failed: %v\n%s", err, out)
	}
	got := string(out)
	if strings.Contains(got, "localforward ") || strings.Contains(got, "remotecommand ") || !strings.Contains(got, "port 2222\n") {
		t.Fatalf("window retained login-only settings or lost connection settings:\n%s", got)
	}
}

func TestLineRejects(t *testing.T) {
	cases := map[string]struct {
		spec iterm2.Launch
		dest string
	}{
		"bad destination":     {iterm2.Launch{Name: "sm-claude-abc", Attach: true}, "-oProxyCommand=evil"},
		"attach bad name":     {iterm2.Launch{Name: "not-sm", Attach: true}, "mybox"},
		"empty argv":          {iterm2.Launch{Dir: "/d"}, "mybox"},
		"unknown agent":       {iterm2.Launch{Argv: []string{"rm", "-rf", "/"}}, "mybox"},
		"arg with semicolon":  {iterm2.Launch{Argv: []string{"claude", "x;reboot"}}, "mybox"},
		"arg with space":      {iterm2.Launch{Argv: []string{"claude", "a b"}}, "mybox"},
		"relative bindir":     {iterm2.Launch{Argv: []string{"claude"}, BinDir: "evil/bin"}, "mybox"},
		"bindir with quote":   {iterm2.Launch{Argv: []string{"claude"}, BinDir: "/e'vil"}, "mybox"},
		"tmux form bad name":  {iterm2.Launch{Argv: []string{"claude"}, Tmux: true, Name: "sm-"}, "mybox"},
		"dir with escape":     {iterm2.Launch{Argv: []string{"claude"}, Dir: "/tmp/\x1b]0;x\a"}, "mybox"},
		"dir with newline":    {iterm2.Launch{Argv: []string{"claude"}, Dir: "/tmp/a\nb"}, "mybox"},
		"tmux form no name":   {iterm2.Launch{Argv: []string{"claude"}, Tmux: true}, "mybox"},
		"attach without name": {iterm2.Launch{Attach: true}, "mybox"},
	}
	for name, c := range cases {
		if _, _, err := Line(c.spec, c.dest, nil); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestSocketRequiresAbsolutePath(t *testing.T) {
	t.Setenv(EnvVar, "relative/path.sock")
	if Socket() != "" {
		t.Fatal("relative socket path accepted")
	}
	t.Setenv(EnvVar, "/tmp/sm-bridge-x.sock")
	if Socket() != "/tmp/sm-bridge-x.sock" {
		t.Fatal("absolute socket path rejected")
	}
}

// startServe returns a Send-able socket path served by open.
func startServe(t *testing.T, dest string, open Handler) string {
	t.Helper()
	// macOS's Unix socket path limit is shorter than t.TempDir paths for
	// descriptive test names. Keep the private test directory name short.
	dir, err := os.MkdirTemp("", "sm-bridge-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "b.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go Serve(ln, dest, nil, open, func(string, ...any) {})
	return sock
}

func TestSendServeRoundTrip(t *testing.T) {
	var gotKey, gotLine string
	sock := startServe(t, "mybox", func(key, line string) error {
		gotKey, gotLine = key, line
		return nil
	})
	err := Send(sock, iterm2.Launch{Name: "sm-claude-abc123", Attach: true})
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "mybox|sm-claude-abc123" {
		t.Fatalf("key = %q", gotKey)
	}
	if !strings.Contains(gotLine, "attach-session") {
		t.Fatalf("line = %q", gotLine)
	}
}

func TestSendSurfacesValidationError(t *testing.T) {
	sock := startServe(t, "mybox", func(string, string) error { return nil })
	err := Send(sock, iterm2.Launch{Argv: []string{"rm", "-rf"}})
	if err == nil || !strings.Contains(err.Error(), "agent") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestSendSurfacesOpenError(t *testing.T) {
	sock := startServe(t, "mybox", func(string, string) error { return errors.New("window exploded") })
	err := Send(sock, iterm2.Launch{Name: "sm-claude-abc", Attach: true})
	if err == nil || !strings.Contains(err.Error(), "window exploded") {
		t.Fatalf("expected open error, got %v", err)
	}
}

func TestSendDeadSocket(t *testing.T) {
	err := Send(filepath.Join(t.TempDir(), "gone.sock"), iterm2.Launch{Name: "sm-claude-abc", Attach: true})
	if err == nil || !strings.Contains(err.Error(), "sm ssh") {
		t.Fatalf("expected reconnect hint, got %v", err)
	}
}

func TestServeRejectsMalformedPayloadTypesAndMixedAttach(t *testing.T) {
	sock := startServe(t, "mybox", func(string, string) error {
		t.Error("invalid payload reached window opener")
		return nil
	})
	for _, payload := range []string{
		`[]`, `null`,
		`{"argv":"claude"}`, `{"argv":["claude",123]}`,
		`{"argv":["claude"],"dir":false}`,
		`{"argv":["claude"],"tmux":"false"}`,
		`{"name":"sm-claude-abc","attach":1}`,
		`{"name":"sm-claude-abc","attach":true,"argv":["codex","exec","payload"]}`,
		`{"name":"sm-claude-abc","attach":true,"tmux":true}`,
		`{"name":"sm-claude-abc","attach":true,"dir":"/tmp/\n"}`,
		`{"name":"sm-claude-abc\n","attach":true}`,
	} {
		c, err := net.Dial("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Write([]byte(payload + "\n")); err != nil {
			c.Close()
			t.Fatal(err)
		}
		response, err := bufio.NewReader(c).ReadString('\n')
		c.Close()
		if err != nil || !strings.HasPrefix(response, "err ") {
			t.Errorf("payload %s: response=%q, err=%v", payload, response, err)
		}
	}
}
