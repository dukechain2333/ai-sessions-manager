package store

import "testing"

func TestAgentLabel(t *testing.T) {
	if AgentClaude.Label() != "claude" || AgentCodex.Label() != "codex" {
		t.Errorf("labels: %q %q", AgentClaude.Label(), AgentCodex.Label())
	}
}

func TestSessionHasAgent(t *testing.T) {
	s := Session{Agent: AgentCodex}
	if s.Agent != AgentCodex {
		t.Errorf("Agent = %q", s.Agent)
	}
}
