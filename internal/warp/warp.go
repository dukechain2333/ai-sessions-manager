// Package warp opens native Warp tabs and runs a command in them. Warp
// has no scripting dictionary and no local-window CLI (oz drives cloud
// agents only); the one programmatic "open a tab and run a command"
// path is a Tab Config file whose commands run in the new pane, delivered
// through the warp://tab_config URI. Used by the `sm ssh` helper on the
// desktop and by a local window-mode sm.
package warp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Opener opens native Warp tabs. Unlike Ghostty's it holds no dedupe
// state: Warp hands back no tab handle, so every launch opens a fresh
// tab (duplicate tracked launches still collapse in the target
// machine's tmux new-session -A).
type Opener struct {
	goos string
	dir  string                                            // tab_configs dir
	run  func(name string, args ...string) (string, error) // injected for tests
}

// New returns an Opener after checking this process can actually reach a
// Warp: TERM_PROGRAM must say so — or, inside a tmux attach (which
// rewrites TERM_PROGRAM to "tmux"), the inherited
// WARP_TERMINAL_SESSION_UUID — and on Linux xdg-open must be on PATH to
// deliver the warp:// URI.
func New() (*Opener, error) {
	if os.Getenv("TERM_PROGRAM") != "WarpTerminal" && os.Getenv("WARP_TERMINAL_SESSION_UUID") == "" {
		return nil, errors.New("this terminal is not Warp")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	o := &Opener{goos: runtime.GOOS, run: runOut}
	switch o.goos {
	case "darwin":
		o.dir = filepath.Join(home, ".warp", "tab_configs")
		return o, nil
	case "linux":
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return nil, errors.New("xdg-open not found on PATH")
		}
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		o.dir = filepath.Join(data, "warp-terminal", "tab_configs")
		return o, nil
	}
	return nil, errors.New("Warp tabs are not supported on " + o.goos)
}

// runOut runs a command with a hang guard and returns its stdout; stderr
// is folded into the returned error. Same idiom as ghostty's.
func runOut(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	err := c.Run()
	if err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			err = errors.New(err.Error() + ": " + msg)
		}
	}
	return out.String(), err
}

// renderTabConfig builds the Tab Config that runs line in a fresh
// terminal pane. No directory key: line already carries its own cd
// (bridge.Line's local form) or is a complete ssh command. Verified live
// that Warp accepts a directory-less pane (Task 3 step 6); if a Warp
// update regresses that, add `directory = "~"` here — the cd in line
// still decides the real working dir.
func renderTabConfig(line string) string {
	return "name = \"sm\"\n\n[[panes]]\nid = \"main\"\ntype = \"terminal\"\ncommands = [" + tomlString(line) + "]\n"
}

// tomlString renders s as a TOML basic string: backslash and double
// quote escaped per the TOML spec; \t \n \r get their shorthand escapes.
// Other control characters are stripped, not escaped — they can never
// legitimately appear in a launch line (bridge.Line rejects them
// upstream), and \u-escaping would deliver raw control bytes into a
// shell command line.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			// Skip control characters
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Open runs line in a native Warp tab (a new tab in the frontmost window):
// land the Tab Config, then deliver the URI. key is accepted for the
// bridge.Handler shape and ignored — no tab handle comes back, so there is
// nothing to refocus. Past a successful open/xdg-open exit the launch is
// fire-and-forget.
func (o *Opener) Open(_, line string) error {
	if err := o.writeConfig(renderTabConfig(line)); err != nil {
		return errors.New("warp window: " + err.Error())
	}
	opener := "open"
	if o.goos == "linux" {
		opener = "xdg-open"
	}
	if _, err := o.run(opener, "warp://tab_config/sm"); err != nil {
		return errors.New("warp window: " + err.Error())
	}
	return nil
}

// writeConfig lands content at <dir>/sm.toml via temp-file-plus-rename in
// the same directory, so Warp can never read a half-written file.
func (o *Opener) writeConfig(content string) error {
	if err := os.MkdirAll(o.dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(o.dir, "sm-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(o.dir, "sm.toml"))
}
