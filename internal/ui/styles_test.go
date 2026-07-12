package ui

import (
	"testing"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

func TestAgentAccent(t *testing.T) {
	st := defaultStyles()
	if st.AgentAccent(store.AgentClaude) != st.Accent {
		t.Error("claude accent should be the coral Accent")
	}
	if st.AgentAccent(store.AgentCodex) != st.CodexAccent {
		t.Error("codex accent should be the teal CodexAccent")
	}
	if st.Accent == st.CodexAccent {
		t.Error("Accent and CodexAccent must differ")
	}
}
