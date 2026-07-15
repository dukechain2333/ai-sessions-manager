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

// Runner is the injectable tmux boundary. The real implementation is Exec;
// tests inject a fake.
type Runner interface {
	// List returns the set of live sm-prefixed session names. A missing tmux
	// server yields an empty set, not an error.
	List() (map[string]bool, error)
	// Path returns a session's pane_current_path (used to place provisional
	// new-session tmux during adoption).
	Path(name string) (string, error)
	Kill(name string) error
	Rename(from, to string) error
}

// Exec is the real Runner; it shells out to tmux.
type Exec struct{}

func (Exec) List() (map[string]bool, error) {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		// No server running (or no sessions) is an empty set, not an error.
		return map[string]bool{}, nil
	}
	return parseList(string(out)), nil
}

func (Exec) Path(name string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", name, "#{pane_current_path}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (Exec) Kill(name string) error {
	return exec.Command("tmux", "kill-session", "-t", name).Run()
}

func (Exec) Rename(from, to string) error {
	return exec.Command("tmux", "rename-session", "-t", from, to).Run()
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
