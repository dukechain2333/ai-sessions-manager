package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/dukechain2333/ai-sessions-manager/internal/config"
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

func TestStylesWithColorsOverridesAccents(t *testing.T) {
	st := stylesWithColors(
		config.AgentColors{Light: "#111111", Dark: "#222222"},
		config.AgentColors{Light: "#333333", Dark: "#444444"},
	)
	if st.Accent.Light != "#111111" || st.Accent.Dark != "#222222" {
		t.Errorf("claude accent = %+v", st.Accent)
	}
	if st.CodexAccent.Light != "#333333" || st.CodexAccent.Dark != "#444444" {
		t.Errorf("codex accent = %+v", st.CodexAccent)
	}
	// A derived style must pick up the override too.
	if st.ClaudeTag.GetForeground() != lipgloss.AdaptiveColor(st.Accent) {
		t.Error("ClaudeTag should use the overridden accent")
	}
}
