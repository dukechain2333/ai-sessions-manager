// Package iterm2 builds the OSC 1337 custom control sequences that ask a
// companion AutoLaunch script inside the user's local iTerm2 to open a
// native window ssh-ing back into this host. sm emits them to the tty; a
// terminal without the script simply ignores them.
package iterm2

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// Identity is the custom-sequence id the AutoLaunch script filters on.
const Identity = "sm"

// Launch describes one native window the local script should open. Tracked
// launches carry the tmux session name and Tmux=true so the remote agent
// lands in the usual session-form tmux; Attach=true jumps to a live one.
type Launch struct {
	Host   string   `json:"host"`
	Dir    string   `json:"dir,omitempty"`
	Name   string   `json:"name,omitempty"`
	Argv   []string `json:"argv,omitempty"`
	Tmux   bool     `json:"tmux,omitempty"`
	Attach bool     `json:"attach,omitempty"`
}

// Sequence renders the escape sequence for l. insideTmux wraps it in tmux's
// passthrough envelope (ESC Ptmux; … ESC \ with inner ESCs doubled) so it
// survives a plain tmux attach; the pane needs allow-passthrough on, which
// sm best-effort enables at startup.
func Sequence(l Launch, insideTmux bool) string {
	b, err := json.Marshal(l)
	if err != nil { // all fields are marshalable; defensive only
		b = []byte("{}")
	}
	seq := "\x1b]1337;Custom=id=" + Identity + ":" + base64.StdEncoding.EncodeToString(b) + "\a"
	if insideTmux {
		return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return seq
}
