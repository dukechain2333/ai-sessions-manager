package store

// Agent identifies which coding agent produced a session.
type Agent string

const (
	AgentClaude Agent = "claude"
	AgentCodex  Agent = "codex"
)

// Label is the lowercase agent name shown in the UI.
func (a Agent) Label() string { return string(a) }
