// Package ui implements the cs terminal interface with Bubble Tea.
package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

type styles struct {
	Accent         lipgloss.AdaptiveColor
	CodexAccent    lipgloss.AdaptiveColor
	AppTitle       lipgloss.Style
	Count          lipgloss.Style
	ListTitle      lipgloss.Style
	ListTitleSel   lipgloss.Style
	ListMeta       lipgloss.Style
	ListMetaSel    lipgloss.Style
	CodexTitleSel  lipgloss.Style
	ClaudeTag      lipgloss.Style
	CodexTag       lipgloss.Style
	GroupHeader    lipgloss.Style
	GroupHeaderSel lipgloss.Style
	GroupCount     lipgloss.Style
	UserMsg        lipgloss.Style
	AssistantMsg   lipgloss.Style
	ToolMsg        lipgloss.Style
	PaneBlurred    lipgloss.Style
	Help           lipgloss.Style
	ErrorText      lipgloss.Style
	DialogBox      lipgloss.Style
}

func defaultStyles() styles {
	accent := lipgloss.AdaptiveColor{Light: "#C15F3C", Dark: "#D97757"}
	codex := lipgloss.AdaptiveColor{Light: "#0A7C66", Dark: "#10A37F"}
	text := lipgloss.AdaptiveColor{Light: "#333333", Dark: "#DEDEDE"}
	dim := lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#767676"}
	faint := lipgloss.AdaptiveColor{Light: "#D0D0D0", Dark: "#3A3A3A"}
	warn := lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	return styles{
		Accent:         accent,
		CodexAccent:    codex,
		AppTitle:       lipgloss.NewStyle().Bold(true).Foreground(text),
		Count:          lipgloss.NewStyle().Foreground(dim),
		ListTitle:      lipgloss.NewStyle().Foreground(text),
		ListTitleSel:   lipgloss.NewStyle().Bold(true).Foreground(accent),
		ListMeta:       lipgloss.NewStyle().Foreground(dim),
		ListMetaSel:    lipgloss.NewStyle().Foreground(accent),
		CodexTitleSel:  lipgloss.NewStyle().Bold(true).Foreground(codex),
		ClaudeTag:      lipgloss.NewStyle().Foreground(accent),
		CodexTag:       lipgloss.NewStyle().Foreground(codex),
		GroupHeader:    lipgloss.NewStyle().Bold(true).Foreground(text),
		GroupHeaderSel: lipgloss.NewStyle().Bold(true).Foreground(accent),
		GroupCount:     lipgloss.NewStyle().Foreground(accent),
		UserMsg:        lipgloss.NewStyle().Bold(true).Foreground(text),
		AssistantMsg:   lipgloss.NewStyle().Foreground(text),
		ToolMsg:        lipgloss.NewStyle().Foreground(dim),
		PaneBlurred:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(faint),
		Help:           lipgloss.NewStyle().Foreground(dim),
		ErrorText:      lipgloss.NewStyle().Bold(true).Foreground(warn),
		DialogBox:      lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
	}
}

// TitleMark is the Claude-style ✻ rendered in the accent color.
func (s styles) TitleMark() string {
	return lipgloss.NewStyle().Foreground(s.Accent).Render("✻")
}

// AgentAccent is the accent color for an agent: coral for Claude, teal for Codex.
func (s styles) AgentAccent(a store.Agent) lipgloss.AdaptiveColor {
	if a == store.AgentCodex {
		return s.CodexAccent
	}
	return s.Accent
}

func humanTime(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2 2006")
	}
}
