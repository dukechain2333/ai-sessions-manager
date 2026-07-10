package ui

import (
	"fmt"
	"os/exec"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dukechain2333/claude-sessions/internal/store"
)

type focusArea int

const (
	focusList focusArea = iota
	focusPreview
	focusFilter
)

type dialogKind int

const (
	dialogNone dialogKind = iota
	dialogDelete
	dialogPickDir
	dialogError
)

type (
	scanDoneMsg struct {
		sessions []store.Session
		err      error
	}
	enrichMsg     store.EnrichResult
	enrichDoneMsg struct{}
	transcriptMsg struct {
		id  string
		t   store.Transcript
		err error
	}
	claudeExitMsg    struct{ err error }
	claudeMissingMsg struct{}
)

type Model struct {
	projectsDir string
	st          styles

	list        listPane
	preview     viewport.Model
	filterInput textinput.Model
	focus       focusArea

	dialog        dialogKind
	errText       string
	pendingDelete int
	pendingResume *store.Session
	dirs          []string
	dirCursor     int
	dirInput      textinput.Model

	cache      *store.TranscriptCache
	enrichCh   chan store.EnrichResult
	previewFor string
	loading    bool

	width, height int
	ready         bool

	// injected for tests
	trashFn   func(string, store.Session) (string, error)
	runClaude func(dir string, args ...string) tea.Cmd
}

func New(projectsDir string) Model {
	st := defaultStyles()
	fi := textinput.New()
	fi.Placeholder = "filter…"
	fi.Prompt = "🔍 "
	di := textinput.New()
	di.Placeholder = "…or type a path"
	di.Prompt = "> "
	return Model{
		projectsDir:   projectsDir,
		st:            st,
		list:          listPane{styles: st},
		filterInput:   fi,
		dirInput:      di,
		cache:         store.NewTranscriptCache(8),
		pendingDelete: -1,
		trashFn:       store.TrashSession,
		runClaude:     execClaude,
	}
}

func execClaude(dir string, args ...string) tea.Cmd {
	c := exec.Command("claude", args...)
	c.Dir = dir
	return tea.ExecProcess(c, func(err error) tea.Msg { return claudeExitMsg{err} })
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.scanCmd(), checkClaudeCmd)
}

func checkClaudeCmd() tea.Msg {
	if _, err := exec.LookPath("claude"); err != nil {
		return claudeMissingMsg{}
	}
	return nil
}

func (m Model) scanCmd() tea.Cmd {
	dir := m.projectsDir
	return func() tea.Msg {
		sessions, err := store.Scan(dir)
		return scanDoneMsg{sessions: sessions, err: err}
	}
}

func waitEnrich(ch chan store.EnrichResult) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return enrichDoneMsg{}
		}
		return enrichMsg(r)
	}
}

func (m *Model) loadTranscriptCmd() tea.Cmd {
	s, _, ok := m.list.Selected()
	if !ok {
		m.preview.SetContent("")
		m.previewFor = ""
		return nil
	}
	if s.ID == m.previewFor {
		return nil
	}
	m.previewFor = s.ID
	cache, path, id := m.cache, s.Path, s.ID
	return func() tea.Msg {
		t, err := cache.Get(path)
		t.SessionID = id
		return transcriptMsg{id: id, t: t, err: err}
	}
}

// narrow reports whether the terminal is too narrow for two panes;
// below 80 columns the preview pane is hidden (per spec).
func (m Model) narrow() bool { return m.width < 80 }

// Layout: 1 header row + 1 filter row + body + 1 help row; borders eat
// 2 rows/cols per pane.
func (m *Model) layout() {
	bodyH := m.height - 5
	if bodyH < 3 {
		bodyH = 3
	}
	listW := m.width * 2 / 5
	if listW < 20 {
		listW = 20
	}
	if m.narrow() {
		listW = m.width - 2
	}
	previewW := m.width - listW - 4
	if previewW < 10 {
		previewW = 10
	}
	m.list.SetSize(listW-2, bodyH)
	if !m.ready {
		m.preview = viewport.New(previewW, bodyH)
		m.ready = true
	} else {
		m.preview.Width = previewW
		m.preview.Height = bodyH
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case scanDoneMsg:
		if msg.err != nil {
			m.dialog = dialogError
			m.errText = fmt.Sprintf("cannot read %s: %v", m.projectsDir, msg.err)
			return m, nil
		}
		m.list.SetSessions(msg.sessions)
		m.previewFor = ""
		if len(msg.sessions) == 0 {
			return m, nil
		}
		ch := make(chan store.EnrichResult, len(msg.sessions))
		m.enrichCh = ch
		m.loading = true
		store.Enrich(msg.sessions, 8, ch)
		return m, tea.Batch(waitEnrich(ch), m.loadTranscriptCmd())

	case enrichMsg:
		if msg.Err != nil {
			if msg.Index >= 0 && msg.Index < len(m.list.sessions) {
				m.list.sessions[msg.Index].Unreadable = true
				m.list.sessions[msg.Index].Enriched = true
			}
		} else {
			m.list.ApplyEnrich(msg.Index, msg.Meta)
		}
		cmd := m.loadTranscriptCmd()
		return m, tea.Batch(waitEnrich(m.enrichCh), cmd)

	case enrichDoneMsg:
		m.loading = false
		return m, m.loadTranscriptCmd()

	case transcriptMsg:
		if msg.id != m.previewFor {
			return m, nil // stale response for a de-selected session
		}
		if msg.err != nil {
			m.preview.SetContent(m.st.ErrorText.Render(msg.err.Error()))
			return m, nil
		}
		m.preview.SetContent(renderTranscript(msg.t, m.preview.Width, m.st))
		m.preview.GotoTop()
		return m, nil

	case claudeMissingMsg:
		m.dialog = dialogError
		m.errText = "claude not found on PATH — install Claude Code first"
		return m, nil

	case claudeExitMsg:
		if msg.err != nil {
			m.dialog = dialogError
			m.errText = "claude exited with error: " + msg.err.Error()
		}
		return m, m.scanCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.dialog != dialogNone {
		return m.handleDialogKey(msg)
	}
	switch m.focus {
	case focusFilter:
		switch msg.Type {
		case tea.KeyEsc:
			m.filterInput.SetValue("")
			m.filterInput.Blur()
			m.list.SetFilter("")
			m.focus = focusList
			return m, m.loadTranscriptCmd()
		case tea.KeyEnter:
			m.filterInput.Blur()
			m.focus = focusList
			return m, nil
		}
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		m.list.SetFilter(m.filterInput.Value())
		return m, tea.Batch(cmd, m.loadTranscriptCmd())

	case focusPreview:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "tab", "esc":
			m.focus = focusList
			return m, nil
		}
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd

	default: // focusList
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "tab":
			m.focus = focusPreview
			return m, nil
		case "/":
			m.focus = focusFilter
			m.filterInput.Focus()
			return m, nil
		case "j", "down":
			m.list.MoveCursor(1)
			return m, m.loadTranscriptCmd()
		case "k", "up":
			m.list.MoveCursor(-1)
			return m, m.loadTranscriptCmd()
		case "e":
			m.list.ToggleEmpty()
			return m, m.loadTranscriptCmd()
		case "r":
			return m, m.scanCmd()
		case "enter":
			return m.startResume()
		case "n":
			return m.openNewSession()
		case "d":
			return m.askDelete()
		}
	}
	return m, nil
}

// Stubs completed in the actions task.
func (m Model) startResume() (tea.Model, tea.Cmd)    { return m, nil }
func (m Model) openNewSession() (tea.Model, tea.Cmd) { return m, nil }
func (m Model) askDelete() (tea.Model, tea.Cmd)      { return m, nil }
func (m Model) handleDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.dialog = dialogNone
	return m, nil
}
func (m Model) dialogView() string { return "" }

func (m Model) View() string {
	if !m.ready {
		return "loading…"
	}
	count := fmt.Sprintf("%d sessions", m.list.Len())
	if m.loading {
		count += " · scanning…"
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		m.st.AppTitle.Render(" cs · Claude Sessions  "),
		m.st.Count.Render(count),
	)
	filterBar := m.filterInput.View()

	var body string
	if m.dialog != dialogNone {
		body = lipgloss.Place(m.width, m.height-3, lipgloss.Center, lipgloss.Center, m.dialogView())
	} else {
		listStyle, prevStyle := m.st.PaneBlurred, m.st.PaneBlurred
		if m.focus == focusPreview {
			prevStyle = m.st.PaneFocused
		} else {
			listStyle = m.st.PaneFocused
		}
		if m.narrow() {
			body = listStyle.Render(m.list.View())
		} else {
			body = lipgloss.JoinHorizontal(lipgloss.Top,
				listStyle.Render(m.list.View()),
				prevStyle.Render(m.preview.View()),
			)
		}
	}

	help := m.st.Help.Render(" ↵ resume  tab focus  n new  d delete  / filter  e empty  r rescan  q quit")
	return header + "\n" + filterBar + "\n" + body + "\n" + help
}
