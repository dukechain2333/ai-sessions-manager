// Package ui implements the cs terminal interface with Bubble Tea.
package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type styles struct {
	AppTitle     lipgloss.Style
	Count        lipgloss.Style
	ListTitle    lipgloss.Style
	ListTitleSel lipgloss.Style
	ListMeta     lipgloss.Style
	ListMetaSel  lipgloss.Style
	GroupHeader  lipgloss.Style
	UserMsg      lipgloss.Style
	AssistantMsg lipgloss.Style
	ToolMsg      lipgloss.Style
	PaneFocused  lipgloss.Style
	PaneBlurred  lipgloss.Style
	Help         lipgloss.Style
	ErrorText    lipgloss.Style
	DialogBox    lipgloss.Style
}

func defaultStyles() styles {
	accent := lipgloss.AdaptiveColor{Light: "56", Dark: "212"}
	dim := lipgloss.AdaptiveColor{Light: "245", Dark: "241"}
	text := lipgloss.AdaptiveColor{Light: "235", Dark: "252"}
	warn := lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	return styles{
		AppTitle:     lipgloss.NewStyle().Bold(true).Foreground(accent),
		Count:        lipgloss.NewStyle().Foreground(dim),
		ListTitle:    lipgloss.NewStyle().Foreground(text),
		ListTitleSel: lipgloss.NewStyle().Bold(true).Foreground(accent),
		ListMeta:     lipgloss.NewStyle().Foreground(dim),
		ListMetaSel:  lipgloss.NewStyle().Foreground(accent),
		GroupHeader:  lipgloss.NewStyle().Bold(true).Foreground(text).Underline(true),
		UserMsg:      lipgloss.NewStyle().Bold(true).Foreground(text),
		AssistantMsg: lipgloss.NewStyle().Foreground(text),
		ToolMsg:      lipgloss.NewStyle().Foreground(dim),
		PaneFocused:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent),
		PaneBlurred:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dim),
		Help:         lipgloss.NewStyle().Foreground(dim),
		ErrorText:    lipgloss.NewStyle().Bold(true).Foreground(warn),
		DialogBox:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
	}
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
