package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dukechain2333/ai-sessions-manager/internal/config"
)

// The settings dialog edits a working copy of the startup config (m.setForm)
// and writes it back with config.Save on "s". Apply-on-restart by design:
// a save never touches the running Model's state.

type settingKind int

const (
	settingEnum settingKind = iota
	settingBool
	settingText
	settingHex
)

// settingRow is one line of the form. get/set go through the working copy
// as strings (bools as "true"/"false") so one table drives rendering,
// cycling, and editing for every kind.
type settingRow struct {
	label   string
	kind    settingKind
	options []string // settingEnum only
	get     func(*config.Config) string
	set     func(*config.Config, string)
}

// settingsRows is the full form, in the file's key order.
func settingsRows() []settingRow {
	return []settingRow{
		{label: "view", kind: settingEnum, options: []string{"list", "tabs"},
			get: func(c *config.Config) string { return c.View },
			set: func(c *config.Config, v string) { c.View = v }},
		{label: "open_in mode", kind: settingEnum, options: []string{config.OpenInCurrent, config.OpenInWindow},
			get: func(c *config.Config) string { return c.OpenIn },
			set: func(c *config.Config, v string) { c.OpenIn = v }},
		{label: "iterm2 ssh", kind: settingText,
			get: func(c *config.Config) string { return c.ITerm2SSH },
			set: func(c *config.Config, v string) { c.ITerm2SSH = v }},
		{label: "tmux enabled", kind: settingBool,
			get: func(c *config.Config) string { return strconv.FormatBool(c.TmuxEnabled) },
			set: func(c *config.Config, v string) { c.TmuxEnabled = v == "true" }},
		{label: "claude light", kind: settingHex,
			get: func(c *config.Config) string { return c.Claude.Light },
			set: func(c *config.Config, v string) { c.Claude.Light = v }},
		{label: "claude dark", kind: settingHex,
			get: func(c *config.Config) string { return c.Claude.Dark },
			set: func(c *config.Config, v string) { c.Claude.Dark = v }},
		{label: "codex light", kind: settingHex,
			get: func(c *config.Config) string { return c.Codex.Light },
			set: func(c *config.Config, v string) { c.Codex.Light = v }},
		{label: "codex dark", kind: settingHex,
			get: func(c *config.Config) string { return c.Codex.Dark },
			set: func(c *config.Config, v string) { c.Codex.Dark = v }},
	}
}

// openSettings seeds the form from the startup file config and shows the
// dialog. m.cfg, not the runtime fields — those may have been downgraded
// (tmux missing, open_in fallback) and colors only live in baked styles.
func (m *Model) openSettings() {
	m.setForm = m.cfg
	m.setCursor = 0
	m.setEditing = false
	m.setErr = ""
	m.dialog = dialogSettings
}

// handleSettingsKey is dialogSettings' key handler (Tasks 4–6 extend it).
func (m Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := settingsRows()
	if m.setEditing {
		switch msg.Type {
		case tea.KeyEsc:
			m.setEditing = false
			m.setErr = ""
			m.setInput.Blur()
			return m, nil
		case tea.KeyEnter:
			row := rows[m.setCursor]
			v := strings.TrimSpace(m.setInput.Value())
			if row.kind == settingHex && !config.ValidHex(v) {
				m.setErr = "colors must be #RRGGBB"
				return m, nil
			}
			row.set(&m.setForm, v)
			m.setEditing = false
			m.setErr = ""
			m.setInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.setInput, cmd = m.setInput.Update(msg)
		return m, cmd
	}
	switch msg.String() {
	case "j", "down":
		if m.setCursor < len(rows)-1 {
			m.setCursor++
		}
		return m, nil
	case "k", "up":
		if m.setCursor > 0 {
			m.setCursor--
		}
		return m, nil
	case "esc", "q":
		m.dialog = dialogNone
		m.setErr = ""
		return m, nil
	case "enter", " ", "left", "right", "h", "l":
		return m.activateSettingRow(rows[m.setCursor], msg.String())
	}
	return m, nil
}

// activateSettingRow applies the "change" keys to the row under the cursor:
// enums cycle (backward on h/←), bools toggle, text rows open the editor
// (Task 5).
func (m Model) activateSettingRow(row settingRow, key string) (tea.Model, tea.Cmd) {
	switch row.kind {
	case settingEnum:
		delta := 1
		if key == "h" || key == "left" {
			delta = -1
		}
		cur := row.get(&m.setForm)
		idx := 0
		for i, o := range row.options {
			if o == cur {
				idx = i
			}
		}
		row.set(&m.setForm, row.options[(idx+delta+len(row.options))%len(row.options)])
	case settingBool:
		if key == "enter" || key == " " {
			row.set(&m.setForm, strconv.FormatBool(row.get(&m.setForm) != "true"))
		}
	case settingText, settingHex:
		if key != "enter" {
			return m, nil
		}
		m.setInput.SetValue(row.get(&m.setForm))
		m.setInput.CursorEnd()
		m.setInput.Focus()
		m.setEditing = true
		m.setErr = ""
	}
	return m, nil
}

// settingsView renders the form.
func (m Model) settingsView() string {
	rows := settingsRows()
	var b strings.Builder
	b.WriteString(m.st.AppTitle.Render("Settings") + "\n\n")
	for i, r := range rows {
		marker := "  "
		if i == m.setCursor {
			marker = m.st.ListTitleSel.Render("▶") + " "
		}
		b.WriteString(marker + fmt.Sprintf("%-13s", r.label) + m.renderSettingValue(r, i) + "\n")
	}
	b.WriteString("\n")
	if m.setErr != "" {
		b.WriteString(m.st.ErrorText.Render(m.setErr) + "\n")
	}
	help := "j/k move · ↵/←/→ change · s save · esc close"
	if m.setEditing {
		help = "↵ apply · esc cancel edit"
	}
	b.WriteString(m.st.Help.Render(help))
	return m.st.DialogBox.Render(b.String())
}

// renderSettingValue renders one row's value cell; the row being edited
// shows the live text input instead.
func (m Model) renderSettingValue(r settingRow, i int) string {
	if m.setEditing && i == m.setCursor {
		return m.setInput.View()
	}
	v := r.get(&m.setForm)
	switch r.kind {
	case settingEnum:
		return "◂ " + v + " ▸"
	case settingBool:
		if v == "true" {
			return "[x]"
		}
		return "[ ]"
	case settingHex:
		return v + " " + lipgloss.NewStyle().Foreground(lipgloss.Color(v)).Render("██")
	default:
		if v == "" {
			return m.st.ListMeta.Render("(unset)")
		}
		return v
	}
}
