// Package bridge carries sm's native-window launches from a remote sm to a
// helper on the user's own desktop, over an ssh reverse-forwarded unix
// socket. The helper side is `sm ssh <destination>` run on the desktop: it
// listens on the socket and opens local Ghostty windows or Warp tabs.
// The remote side is sm itself, which finds the socket path in
// $LC_SM_BRIDGE (an LC_* name so stock sshd AcceptEnv rules let it through,
// the same trick iTerm2 uses for LC_TERMINAL) and writes one JSON launch
// per connection.
//
// The payload is the iTerm2 escape-bridge shape (iterm2.Launch) and the
// validation mirrors scripts/iterm2/sm_open_window.py, with one tightening:
// the destination new windows ssh to is ALWAYS the one the user typed on
// the sm ssh command line — a host arriving in the payload is ignored.
package bridge

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/dukechain2333/ai-sessions-manager/internal/iterm2"
)

// EnvVar names the environment variable that carries the remote socket path
// across the ssh connection (via SetEnv; LC_* passes default AcceptEnv).
const EnvVar = "LC_SM_BRIDGE"

// Socket returns the reverse-forwarded socket path advertised by an
// enclosing `sm ssh` connection, or "" when there is none. Only absolute
// paths are accepted — the value crosses a trust boundary.
func Socket() string {
	p := os.Getenv(EnvVar)
	if !strings.HasPrefix(p, "/") {
		return ""
	}
	return p
}

// Same allowlists as the iTerm2 bridge script; keep the two in sync.
var (
	hostRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,127}$`)
	nameRE   = regexp.MustCompile(`^sm(-[a-z0-9]+)+$`)
	idRE     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,255}$`)
	binDirRE = regexp.MustCompile(`^/[A-Za-z0-9._@/-]{0,512}$`)
)

// AgentArgvOK accepts only commands sm itself emits. A character allowlist
// alone would also admit configuration and execution subcommands such as
// `codex mcp add`, which can run arbitrary programs on the desktop.
func AgentArgvOK(argv []string) bool {
	if len(argv) != 1 && len(argv) != 3 {
		return false
	}
	if argv[0] != "claude" && argv[0] != "codex" {
		return false
	}
	if len(argv) == 1 {
		return true
	}
	return idRE.MatchString(argv[2]) &&
		((argv[0] == "claude" && argv[1] == "--resume") ||
			(argv[0] == "codex" && argv[1] == "resume"))
}

// HostOK reports whether dest is safe to embed in an ssh command line: it
// must start with an alphanumeric, so it can never be parsed as a flag.
func HostOK(dest string) bool { return hostRE.MatchString(dest) }

// Quote wraps s in hard single quotes for POSIX shells. Always quoting (not
// only when a char looks unsafe) is deliberate: shlex.quote-style "safe"
// bare words already bit us once — an unquoted =word triggers zsh's equals
// expansion.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func quoteJoin(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = Quote(a)
	}
	return strings.Join(q, " ")
}

// Line builds the shell line a new window should run for spec, plus the
// key windows are deduped on. dest is the ssh destination the user typed on
// the sm ssh command line ("" = spec runs on this same machine, no ssh);
// sshArgs are extra ssh flags carried into each window's connection. spec
// is untrusted input — it originates on the far side of the connection —
// so every field is validated against the same allowlists as the iTerm2
// bridge script, and spec.Host is deliberately never used.
func Line(spec iterm2.Launch, dest string, sshArgs []string) (key, line string, err error) {
	if dest != "" && !HostOK(dest) {
		return "", "", errors.New("invalid ssh destination")
	}
	if spec.WindowKey != "" && !nameRE.MatchString(spec.WindowKey) {
		return "", "", errors.New("invalid window key")
	}
	if spec.Name != "" && !nameRE.MatchString(spec.Name) {
		return "", "", errors.New("invalid tmux session name")
	}
	if spec.Host != "" && !HostOK(spec.Host) {
		return "", "", errors.New("invalid payload host")
	}
	if spec.BinDir != "" && !binDirRE.MatchString(spec.BinDir) {
		return "", "", errors.New("invalid bindir")
	}
	// The command is typed into a live shell; quoting does not protect its
	// line editor from control bytes. Check this even on attach payloads.
	if strings.ContainsFunc(spec.Dir, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", "", errors.New("dir contains control characters")
	}
	var cmd string
	if spec.Attach {
		if len(spec.Argv) != 0 || spec.Tmux {
			return "", "", errors.New("attach cannot include an agent command")
		}
		if !nameRE.MatchString(spec.Name) {
			return "", "", errors.New("invalid tmux session name")
		}
		key = dest + "|" + spec.Name
		cmd = "exec tmux attach-session -t " + Quote("="+spec.Name)
	} else {
		if !AgentArgvOK(spec.Argv) {
			return "", "", errors.New("argv is not a supported agent launch")
		}
		inner := quoteJoin(spec.Argv)
		// PATH prepend for the ssh form only: the remote end of a fresh ssh
		// runs with sshd's bare PATH, so tmux panes it creates would miss
		// the agent binary. A local launch deliberately ignores bindir — a
		// fresh local shell already has the user's PATH, and honoring a
		// forged bindir would let the payload steer binary resolution.
		path := ""
		if spec.BinDir != "" && dest != "" {
			path = "export PATH=" + Quote(spec.BinDir) + `:"$PATH" && `
		}
		d := Quote(spec.Dir)
		if spec.Tmux {
			if !nameRE.MatchString(spec.Name) {
				return "", "", errors.New("invalid tmux session name")
			}
			cmd = path + "cd " + d + " && exec tmux new-session -A -s " + Quote(spec.Name) + " -c " + d + " " + inner
		} else {
			cmd = path + "cd " + d + " && exec " + inner
		}
		// Tracked launches have a unique tmux name; untracked ones key on
		// dir+argv so different projects never collide on one window.
		if spec.Name != "" {
			key = dest + "|" + spec.Name
		} else {
			key = dest + "|" + spec.Dir + " " + inner
		}
	}
	if spec.WindowKey != "" {
		key = dest + "|" + spec.WindowKey
	}
	if dest == "" {
		return key, cmd, nil
	}
	// These windows run an explicit command and share the original login's
	// forwards. Reset session-only options before user flags (ssh takes the
	// first value) while preserving host, identity, port and jump settings.
	sshPart := "ssh -t -o RemoteCommand=none -o ClearAllForwardings=yes"
	if len(sshArgs) > 0 {
		sshPart += " " + quoteJoin(sshArgs)
	}
	// "--" ends option parsing so dest can never be read as a flag.
	return key, sshPart + " -- " + dest + " " + Quote(cmd), nil
}

// Send delivers one launch to the sm ssh helper listening on sock and waits
// for its verdict ("ok" or "err <reason>"). The generous read deadline
// covers the helper actually opening the window before it replies.
func Send(sock string, l iterm2.Launch) error {
	c, err := net.DialTimeout("unix", sock, 3*time.Second)
	if err != nil {
		return fmt.Errorf("window bridge not reachable — reconnect with `sm ssh` (%v)", err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if _, err := c.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("window bridge: %v", err)
	}
	if uc, ok := c.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}
	resp, err := bufio.NewReader(io.LimitReader(c, 4<<10)).ReadString('\n')
	resp = strings.TrimSpace(resp)
	if resp == "ok" {
		return nil
	}
	if strings.HasPrefix(resp, "err ") {
		return errors.New("window bridge: " + strings.TrimPrefix(resp, "err "))
	}
	return fmt.Errorf("window bridge: no response (%v)", err)
}

// Handler opens (or refocuses) one window running line; key dedupes windows
// across launches.
type Handler func(key, line string) error

// Serve accepts one launch per connection on ln until ln is closed. Each
// payload is decoded, validated and rendered via Line, then handed to open;
// the reply line is "ok" or "err <reason>". logf receives diagnostics (the
// helper's tty belongs to the interactive ssh session, so callers usually
// point it at a file or discard it).
func Serve(ln net.Listener, dest string, sshArgs []string, open Handler, logf func(format string, a ...any)) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(60 * time.Second))
			var spec iterm2.Launch
			if err := json.NewDecoder(io.LimitReader(c, 64<<10)).Decode(&spec); err != nil {
				logf("bad payload: %v", err)
				fmt.Fprintln(c, "err bad payload")
				return
			}
			key, line, err := Line(spec, dest, sshArgs)
			if err != nil {
				logf("rejected launch: %v", err)
				fmt.Fprintf(c, "err %v\n", err)
				return
			}
			logf("open %s: %s", key, line)
			if err := open(key, line); err != nil {
				logf("open failed: %v", err)
				fmt.Fprintf(c, "err %v\n", err)
				return
			}
			fmt.Fprintln(c, "ok")
		}(c)
	}
}
