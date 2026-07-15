// Package tmux owns sm's tmux session naming, argv builders, and the
// injectable Runner boundary. sm keeps no persisted tmux state: the live set
// is discovered by listing sessions whose names carry the "sm-" prefix.
package tmux

import (
	"os/exec"
	"strconv"
	"strings"
)

// Prefix marks every sm-managed tmux session name.
const Prefix = "sm-"

const pendingInfix = "-pending-"

// Short is the first 8 lowercased characters of a session id (fewer if the
// id is shorter). tmux mangles '.', so the full UUID is never embedded.
func Short(id string) string {
	s := strings.ToLower(id)
	if len(s) > 8 {
		s = s[:8]
	}
	return s
}

// Name is the tmux session name for an agent and short id: sm-<agent>-<id8>.
func Name(agent, id8 string) string {
	return Prefix + agent + "-" + id8
}

// PendingName is a provisional name for a new session whose id is not known
// yet: sm-<agent>-pending-<nonce>.
func PendingName(agent string, nonce int64) string {
	return Prefix + agent + pendingInfix + strconv.FormatInt(nonce, 10)
}

// IsPending reports whether name is a provisional new-session tmux.
func IsPending(name string) bool {
	return strings.HasPrefix(name, Prefix) && strings.Contains(name, pendingInfix)
}

// PendingAgent extracts the agent segment of a provisional name, or "".
func PendingAgent(name string) string {
	if !IsPending(name) {
		return ""
	}
	rest := strings.TrimPrefix(name, Prefix)
	i := strings.Index(rest, pendingInfix)
	if i < 0 {
		return ""
	}
	return rest[:i]
}

// PendingNonce extracts the creation nonce encoded by PendingName. The nonce
// is the UnixNano of the moment the provisional tmux was created, which bounds
// how old an adoptable session may be. ok is false for non-pending names.
func PendingNonce(name string) (int64, bool) {
	if !IsPending(name) {
		return 0, false
	}
	i := strings.Index(name, pendingInfix)
	n, err := strconv.ParseInt(name[i+len(pendingInfix):], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ResumeArgs builds the tmux argv (after the "tmux" binary) that attaches to
// session name if it exists, else creates it in cwd running the agent command.
func ResumeArgs(name, cwd, agentName string, agentArgs []string) []string {
	args := []string{"new-session", "-A", "-s", name, "-c", cwd, agentName}
	return append(args, agentArgs...)
}

// NewArgs builds the tmux argv for a fresh (non-attach) session in cwd.
func NewArgs(name, cwd, agentName string, agentArgs []string) []string {
	args := []string{"new-session", "-s", name, "-c", cwd, agentName}
	return append(args, agentArgs...)
}

// WindowArgs builds the tmux argv (after the "tmux" binary) that opens a new
// window in the caller's current tmux session, running the agent command in
// cwd. A non-empty name tags the window for sm's tracking — -n also disables
// tmux's automatic-rename for that window, so the name stays stable until
// adoption renames it. An empty name leaves the window untracked.
func WindowArgs(name, cwd, agentName string, agentArgs []string) []string {
	args := []string{"new-window", "-c", cwd}
	if name != "" {
		args = append(args, "-n", name)
	}
	args = append(args, agentName)
	return append(args, agentArgs...)
}

// SelfSession / SelfWindow name sm's own tmux home used by the open_in
// "window" startup wrap. Deliberately NOT "sm-" prefixed: agent tracking
// discovers by that prefix, and sm's own tmux must stay invisible to
// ●/x/adoption. Reattachment probes by exact match instead.
const (
	SelfSession = "sm"
	SelfWindow  = "sm"
)

// SelfWrapArgs builds the tmux argv (after the "tmux" binary) that lands the
// user inside sm's own tmux session, (re)starting sm as needed. selfCmd is
// sm's own binary and args; cwd pins the window so relative flag paths keep
// resolving. Three server states (probed by SelfState): no session — create
// it running sm; session with a live sm window — select it and attach (the
// detached-workspace reattach); session whose sm window has exited — spawn a
// fresh sm window (new-window makes it current) and attach. The last branch
// is why a bare `new-session -A` is not enough: quitting sm kills its window
// while agent windows keep the session alive. The first branch still carries
// -A so two sm's racing past SelfState both land attached instead of the
// loser dying on "duplicate session". An empty cwd (deleted directory) drops
// -c rather than handing tmux an empty path.
func SelfWrapArgs(selfCmd []string, cwd string, sessionExists bool, smWindowID string) []string {
	switch {
	case !sessionExists:
		args := []string{"new-session", "-A", "-s", SelfSession, "-n", SelfWindow}
		if cwd != "" {
			args = append(args, "-c", cwd)
		}
		return append(args, selfCmd...)
	case smWindowID != "":
		return []string{"select-window", "-t", smWindowID, ";", "attach-session", "-t", "=" + SelfSession}
	default:
		args := []string{"new-window", "-t", "=" + SelfSession + ":", "-n", SelfWindow}
		if cwd != "" {
			args = append(args, "-c", cwd)
		}
		args = append(args, selfCmd...)
		return append(args, ";", "attach-session", "-t", "="+SelfSession)
	}
}

// SelfState probes the tmux server for sm's own session and its live sm
// window id ("" when absent). A missing server is (false, "").
func SelfState() (sessionExists bool, smWindowID string) {
	if exec.Command("tmux", "has-session", "-t", "="+SelfSession).Run() != nil {
		return false, ""
	}
	out, err := exec.Command("tmux", "list-windows", "-t", "="+SelfSession, "-F",
		"#{window_id}\t#{window_name}").Output()
	if err != nil {
		return true, ""
	}
	return true, parseSelfWindow(string(out))
}

// parseSelfWindow finds the SelfWindow-named window id in list-windows
// "id\tname" output, or "".
func parseSelfWindow(out string) string {
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) == 2 && parts[1] == SelfWindow {
			return parts[0]
		}
	}
	return ""
}

// Runner is the injectable tmux boundary. The real implementation is Exec;
// tests inject a fake. Names may denote tmux *sessions* (open_in "current")
// or tmux *windows* (open_in "window"); List discovers both, and the other
// operations resolve a name session-first, then window.
type Runner interface {
	// List returns the set of live sm-prefixed session and window names. A
	// missing tmux server yields an empty set, not an error.
	List() (map[string]bool, error)
	// Path returns the pane_current_path of a session or window (used to
	// place provisional new-session tmux during adoption). For the window
	// form, the path is resolved via the window's active pane.
	Path(name string) (string, error)
	Kill(name string) error
	Rename(from, to string) error
	// Window resolves an sm-prefixed *window* name to its tmux window id
	// ("@N") and owning session name. ok is false when no such window is
	// live — the name is session-form, pending-session-form, or dead.
	Window(name string) (id, session string, ok bool)
}

// Exec is the real Runner; it shells out to tmux.
type Exec struct{}

func (Exec) List() (map[string]bool, error) {
	set := map[string]bool{}
	if out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output(); err == nil {
		for name := range parseList(string(out)) {
			set[name] = true
		}
	}
	for name := range listWindows() {
		set[name] = true
	}
	// No server running (or no sessions) is an empty set, not an error.
	return set, nil
}

func (Exec) Window(name string) (string, string, bool) {
	w, ok := listWindows()[name]
	if !ok {
		return "", "", false
	}
	return w[0], w[1], true
}

func (e Exec) Kill(name string) error {
	if !hasSession(name) {
		if id, _, ok := e.Window(name); ok {
			return exec.Command("tmux", "kill-window", "-t", id).Run()
		}
	}
	return exec.Command("tmux", "kill-session", "-t", "="+name).Run()
}

func (e Exec) Rename(from, to string) error {
	if !hasSession(from) {
		if id, _, ok := e.Window(from); ok {
			return exec.Command("tmux", "rename-window", "-t", id, to).Run()
		}
	}
	return exec.Command("tmux", "rename-session", "-t", "="+from, to).Run()
}

func (e Exec) Path(name string) (string, error) {
	target := "=" + name
	if !hasSession(name) {
		if id, _, ok := e.Window(name); ok {
			target = id
		}
	}
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target, "#{pane_current_path}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// hasSession reports whether a tmux *session* with exactly this name is live
// ("=" pins tmux's default prefix matching to an exact match).
func hasSession(name string) bool {
	return exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil
}

// listWindows returns every live sm-prefixed window, keyed by window name.
func listWindows() map[string][2]string {
	out, err := exec.Command("tmux", "list-windows", "-a", "-F",
		"#{window_id}\t#{session_name}\t#{window_name}").Output()
	if err != nil {
		return map[string][2]string{}
	}
	return parseWindows(string(out))
}

// parseList keeps only sm-prefixed names from tmux list-sessions output.
func parseList(out string) map[string]bool {
	set := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if strings.HasPrefix(name, Prefix) {
			set[name] = true
		}
	}
	return set
}

// parseWindows maps sm-prefixed window names to {window id, session name}.
// Input rows are "id\tsession\tname". Duplicate names keep the first row —
// callers always target the id, so a duplicate can hide but never mis-target.
func parseWindows(out string) map[string][2]string {
	m := map[string][2]string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 3)
		if len(parts) != 3 || !strings.HasPrefix(parts[2], Prefix) {
			continue
		}
		if _, dup := m[parts[2]]; !dup {
			m[parts[2]] = [2]string{parts[0], parts[1]}
		}
	}
	return m
}
