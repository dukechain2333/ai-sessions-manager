// Package ghostty opens native Ghostty windows and runs a command in them.
// macOS drives Ghostty's AppleScript dictionary (Ghostty 1.3+; the first
// use triggers the system Automation permission prompt). Linux asks the
// running GTK instance for a window over `ghostty +new-window` (Ghostty
// 1.3+, D-Bus). Used by the `sm ssh` helper on the desktop and by a local
// window-mode sm.
package ghostty

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// openScript creates a window, waits for its terminal to exist (the object
// tree may not be populated the instant "new window" returns), types the
// command into the shell and presses enter. Typing into a real shell —
// instead of passing a command to execute — keeps the user's environment
// and leaves the window open showing any error instead of flashing closed.
const openScript = `on run argv
	set cmdLine to item 1 of argv
	tell application "Ghostty"
		activate
		set win to new window
		set t to missing value
		repeat 40 times
			try
				set t to focused terminal of selected tab of win
				exit repeat
			on error
				delay 0.05
			end try
		end repeat
		if t is missing value then error "new window has no terminal"
		delay 0.2
		input text cmdLine to t
		send key "enter" to t
		return (id of win) as text
	end tell
end run`

// focusScript raises the window with the given id if it still exists.
const focusScript = `on run argv
	set wid to item 1 of argv
	tell application "Ghostty"
		repeat with w in windows
			if (id of w) as text is wid then
				activate window w
				return "ok"
			end if
		end repeat
	end tell
	return "gone"
end run`

// Opener opens (or refocuses) Ghostty windows. One Opener holds the dedupe
// state: a launch key seen before focuses its still-open window instead of
// opening a second one (macOS only — the Linux IPC returns no handle).
type Opener struct {
	mu      sync.Mutex
	windows map[string]string // launch key → AppleScript window id
	goos    string
	run     func(name string, args ...string) (string, error) // injected for tests
}

// New returns an Opener after checking this process can actually reach a
// Ghostty: it must run inside one (TERM_PROGRAM), and on Linux the ghostty
// binary must be on PATH and support commands through +new-window IPC.
func New() (*Opener, error) {
	o := &Opener{windows: map[string]string{}, goos: runtime.GOOS, run: runOut}
	if os.Getenv("TERM_PROGRAM") != "ghostty" && os.Getenv("GHOSTTY_RESOURCES_DIR") == "" {
		return nil, errors.New("this terminal is not Ghostty")
	}
	switch o.goos {
	case "darwin":
		return o, nil
	case "linux":
		if _, err := exec.LookPath("ghostty"); err != nil {
			return nil, errors.New("ghostty not found on PATH")
		}
		if err := o.checkLinuxVersion(); err != nil {
			return nil, err
		}
		return o, nil
	}
	return nil, errors.New("Ghostty windows are not supported on " + o.goos)
}

var versionRE = regexp.MustCompile(`(?m)^Ghostty ([0-9]+)\.([0-9]+)\.[0-9]+(?:[-+][A-Za-z0-9.+-]+)?(?:\r?\n|$)`)

// The 1.2.x GTK receiver only logged +new-window's command arguments, then
// opened the default shell. Command execution is implemented in 1.3.0:
// https://github.com/ghostty-org/ghostty/blob/v1.3.0/src/apprt/gtk/class/application.zig
func (o *Opener) checkLinuxVersion() error {
	out, err := o.run("ghostty", "--version")
	if err != nil {
		return fmt.Errorf("cannot check Ghostty version: %w", err)
	}
	parts := versionRE.FindStringSubmatch(out)
	if len(parts) == 3 {
		major, majorErr := strconv.Atoi(parts[1])
		minor, minorErr := strconv.Atoi(parts[2])
		if majorErr == nil && minorErr == nil && (major > 1 || (major == 1 && minor >= 3)) {
			return nil
		}
	}
	return errors.New("Ghostty 1.3.0 or newer is required for Linux window commands; upgrade Ghostty or use current-terminal mode")
}

// Open runs line in a native Ghostty window. A key seen before refocuses
// the existing window when it is still open.
func (o *Opener) Open(key, line string) error {
	if o.goos == "linux" {
		// The IPC hands back no window handle, so no dedupe here; the
		// remote tmux new-session -A still collapses duplicate agents.
		// An explicit working directory also avoids Ghostty 1.3.0 appending
		// an inferred --working-directory to the -e command's arguments.
		// Preserve the helper's cwd for relative SSH identity/config paths.
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("ghostty working directory: %w", err)
		}
		_, err = o.run("ghostty", "+new-window", "--working-directory="+cwd, "-e", "/bin/sh", "-c", line)
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if id, ok := o.windows[key]; ok {
		out, err := o.run("osascript", "-e", focusScript, id)
		if err != nil {
			return fmt.Errorf("ghostty focus: %w", err)
		}
		switch strings.TrimSpace(out) {
		case "ok":
			return nil
		case "gone":
			delete(o.windows, key)
		default:
			return errors.New("ghostty focus: unexpected response")
		}
	}
	// The leading space keeps the typed line out of histories configured
	// with HIST_IGNORE_SPACE.
	out, err := o.run("osascript", "-e", openScript, " "+line)
	if err != nil {
		return fmt.Errorf("ghostty window: %v", err)
	}
	if id := strings.TrimSpace(out); id != "" {
		o.windows[key] = id
	}
	return nil
}

// runOut runs a command with a hang guard and returns its stdout; stderr
// (osascript's error channel) is folded into the returned error.
func runOut(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	err := c.Run()
	if err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			err = fmt.Errorf("%v: %s", err, msg)
		}
	}
	return out.String(), err
}
