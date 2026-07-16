package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"

	"github.com/dukechain2333/ai-sessions-manager/internal/ghostty"
)

const sshUsage = `usage: sm ssh <destination> [ssh options...]

Connects like plain ssh, plus a window bridge: while the session is open,
resuming a session in a window-mode sm on the far side opens a native
Ghostty window on THIS machine that sshes back into <destination>.

<destination> comes first (a host or ssh alias); everything after it is
passed to ssh unchanged and reused when new windows dial back.`

// SSHMain implements `sm ssh <destination> [ssh options...]`: an interactive
// ssh session wrapped with the reverse-forwarded window bridge. It returns
// the process exit code.
func SSHMain(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(os.Stderr, sshUsage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	dest, extra := args[0], args[1:]
	if !HostOK(dest) {
		fmt.Fprintf(os.Stderr, "sm ssh: destination %q must come first and start with a letter or digit\n", dest)
		return 2
	}
	opener, err := ghostty.New()
	if err != nil {
		// No bridge possible from this terminal — degrade to plain ssh so
		// the command still does the obvious thing.
		fmt.Fprintf(os.Stderr, "sm ssh: %v — connecting without the window bridge\n", err)
		return runSSH(append(extra, dest))
	}

	// A private 0700 directory, not a bare socket in shared /tmp: the socket
	// itself is born with the process umask before any chmod could land, so
	// the directory is what actually keeps other users out from the start.
	sockDir, err := os.MkdirTemp("", "sm-bridge-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sm ssh: %v — connecting without the window bridge\n", err)
		return runSSH(append(extra, dest))
	}
	defer os.RemoveAll(sockDir)
	local := filepath.Join(sockDir, "helper.sock")
	ln, err := net.Listen("unix", local)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sm ssh: %v — connecting without the window bridge\n", err)
		return runSSH(append(extra, dest))
	}
	defer ln.Close()
	_ = os.Chmod(local, 0o600)

	// Random remote path: predictable names in a shared /tmp could be
	// squatted, and two concurrent sm ssh sessions must not collide.
	// StreamLocalBindMask 0177 leaves the forwarded socket 0600, so other
	// users on the server cannot feed the bridge; StreamLocalBindUnlink
	// clears a stale leftover. SetEnv uses an LC_* name because stock sshd
	// AcceptEnv (Debian/Ubuntu default "AcceptEnv LANG LC_*") lets it
	// through — the same route iTerm2's LC_TERMINAL rides.
	remote := "/tmp/sm-bridge-" + randHex(8) + ".sock"
	logf := func(string, ...any) {}
	if os.Getenv("SM_BRIDGE_DEBUG") != "" {
		logf = func(format string, a ...any) { fmt.Fprintf(os.Stderr, "sm bridge: "+format+"\r\n", a...) }
	}
	go Serve(ln, dest, extra, opener.Open, logf)

	sshArgs := []string{
		"-R", remote + ":" + local,
		"-o", "StreamLocalBindUnlink=yes",
		"-o", "StreamLocalBindMask=0177",
		"-o", "SetEnv=" + EnvVar + "=" + remote,
	}
	sshArgs = append(sshArgs, extra...)
	sshArgs = append(sshArgs, dest)
	fmt.Fprintf(os.Stderr, "sm ssh: window bridge ready — window-mode launches on %s open Ghostty windows here\n", dest)
	return runSSH(sshArgs)
}

// runSSH runs ssh in the foreground on this tty. It must stay a child (not
// an exec-replacement) so the bridge listener keeps serving; Ctrl-C is left
// for ssh itself to handle, since the tty delivers it to the whole
// foreground process group including us.
func runSSH(args []string) int {
	c := exec.Command("ssh", args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	signal.Ignore(os.Interrupt)
	if err := c.Run(); err != nil {
		if c.ProcessState != nil {
			return c.ProcessState.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "sm ssh:", err)
		return 1
	}
	return 0
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		// crypto/rand failing is effectively fatal elsewhere too; fall back
		// to a pid-derived suffix rather than crash a login helper.
		return fmt.Sprintf("%x", os.Getpid())
	}
	return hex.EncodeToString(b)
}
