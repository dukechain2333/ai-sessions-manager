package bridge

import (
	"errors"
	"net"
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
	want := `ssh -t -- mybox 'exec tmux attach-session -t '\''=sm-claude-abc123'\'''`
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
	if want := "ssh -t -- mybox " + Quote(cmd); line != want {
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
	if !strings.HasPrefix(line, `ssh -t '-p' '2222' -- mybox `) {
		t.Fatalf("extra args not carried: %q", line)
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
	sock := filepath.Join(t.TempDir(), "b.sock")
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
