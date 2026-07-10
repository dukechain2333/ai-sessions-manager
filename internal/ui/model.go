package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
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
	enrichMsg struct {
		store.EnrichResult
		ch chan store.EnrichResult
	}
	enrichDoneMsg struct{ ch chan store.EnrichResult }
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
		list:          listPane{styles: st, groupByProject: true},
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
			return enrichDoneMsg{ch: ch}
		}
		return enrichMsg{EnrichResult: r, ch: ch}
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
	previewW := m.width - listW - 2
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
			if m.ready {
				m.preview.SetContent("")
			}
			return m, nil
		}
		ch := make(chan store.EnrichResult, len(msg.sessions))
		m.enrichCh = ch
		m.loading = true
		store.Enrich(msg.sessions, 8, ch)
		return m, tea.Batch(waitEnrich(ch), m.loadTranscriptCmd())

	case enrichMsg:
		if msg.ch != m.enrichCh {
			return m, nil // stale result from a superseded scan; do not re-arm
		}
		if msg.Err != nil {
			if msg.Index >= 0 && msg.Index < len(m.list.sessions) {
				m.list.sessions[msg.Index].Unreadable = true
				m.list.sessions[msg.Index].Enriched = true
			}
		} else {
			m.list.ApplyEnrich(msg.Index, msg.Meta)
		}
		cmd := m.loadTranscriptCmd()
		return m, tea.Batch(waitEnrich(msg.ch), cmd)

	case enrichDoneMsg:
		if msg.ch != m.enrichCh {
			return m, nil
		}
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
		// claude exiting non-zero is normal — the user declined the trust
		// prompt, pressed Ctrl-C, or /exit'd. Only surface an error when claude
		// failed to launch at all (anything that is not an *exec.ExitError).
		var exitErr *exec.ExitError
		if msg.err != nil && !errors.As(msg.err, &exitErr) {
			m.dialog = dialogError
			m.errText = "could not launch claude: " + msg.err.Error()
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
		case "g":
			m.list.ToggleGroup()
			return m, m.loadTranscriptCmd()
		case " ":
			m.list.ToggleFold()
			return m, m.loadTranscriptCmd()
		case "r":
			return m, m.scanCmd()
		case "enter":
			if m.list.OnHeader() {
				m.list.ToggleFold()
				return m, m.loadTranscriptCmd()
			}
			return m.startResume()
		case "n":
			return m.openNewSession()
		case "d":
			return m.askDelete()
		}
	}
	return m, nil
}

func (m Model) startResume() (tea.Model, tea.Cmd) {
	s, _, ok := m.list.Selected()
	if !ok {
		return m, nil
	}
	if st, err := os.Stat(s.CWD); s.CWD == "" || err != nil || !st.IsDir() {
		sess := s
		m.pendingResume = &sess
		m.openDirPicker()
		return m, nil
	}
	return m, m.runClaude(s.CWD, "--resume", s.ID)
}

func (m Model) openNewSession() (tea.Model, tea.Cmd) {
	m.pendingResume = nil
	m.openDirPicker()
	return m, nil
}

func (m *Model) openDirPicker() {
	m.dirs = store.KnownDirs(m.list.Sessions())
	m.dirCursor = 0
	m.dirInput.SetValue("")
	m.dirInput.Focus()
	m.dialog = dialogPickDir
}

func (m Model) askDelete() (tea.Model, tea.Cmd) {
	if _, idx, ok := m.list.Selected(); ok {
		m.pendingDelete = idx
		m.dialog = dialogDelete
	}
	return m, nil
}

func (m Model) handleDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.dialog {
	case dialogError:
		m.dialog = dialogNone
		m.errText = ""
		return m, nil

	case dialogDelete:
		switch msg.String() {
		case "y", "enter":
			idx := m.pendingDelete
			m.pendingDelete = -1
			m.dialog = dialogNone
			if idx >= 0 && idx < len(m.list.sessions) {
				s := m.list.sessions[idx]
				if _, err := m.trashFn(m.projectsDir, s); err != nil {
					m.dialog = dialogError
					m.errText = "delete failed: " + err.Error()
					return m, nil
				}
				m.list.RemoveSession(idx)
				m.previewFor = ""
			}
			return m, m.loadTranscriptCmd()
		case "n", "esc":
			m.pendingDelete = -1
			m.dialog = dialogNone
			return m, nil
		}
		return m, nil

	case dialogPickDir:
		switch msg.Type {
		case tea.KeyEsc:
			m.dialog = dialogNone
			m.pendingResume = nil
			m.dirInput.Blur()
			return m, nil
		case tea.KeyUp, tea.KeyDown:
			delta := 1
			if msg.Type == tea.KeyUp {
				delta = -1
			}
			m.dirCursor += delta
			if m.dirCursor < 0 {
				m.dirCursor = 0
			}
			if m.dirCursor >= len(m.dirs) {
				m.dirCursor = len(m.dirs) - 1
			}
			return m, nil
		case tea.KeyEnter:
			dir := strings.TrimSpace(m.dirInput.Value())
			if dir == "" {
				if m.dirCursor < 0 || m.dirCursor >= len(m.dirs) {
					return m, nil
				}
				dir = m.dirs[m.dirCursor]
			}
			if strings.HasPrefix(dir, "~") {
				if home, err := os.UserHomeDir(); err == nil {
					dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
				}
			}
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				m.dialog = dialogError
				m.errText = "not a directory: " + dir
				m.pendingResume = nil
				return m, nil
			}
			pending := m.pendingResume
			m.pendingResume = nil
			m.dialog = dialogNone
			m.dirInput.Blur()
			if pending != nil {
				return m, m.runClaude(dir, "--resume", pending.ID)
			}
			return m, m.runClaude(dir)
		}
		var cmd tea.Cmd
		m.dirInput, cmd = m.dirInput.Update(msg)
		return m, cmd
	}
	m.dialog = dialogNone
	return m, nil
}

func (m Model) dialogView() string {
	switch m.dialog {
	case dialogError:
		return m.st.DialogBox.Render(
			m.st.ErrorText.Render("Error") + "\n\n" + m.errText + "\n\n" +
				m.st.Help.Render("press any key"))

	case dialogDelete:
		title := ""
		if m.pendingDelete >= 0 && m.pendingDelete < len(m.list.sessions) {
			s := m.list.sessions[m.pendingDelete]
			title = s.Title
			if title == "" {
				title = s.ID
			}
			title += "  (" + s.Project() + ")"
		}
		return m.st.DialogBox.Render(
			"Move session to trash?\n\n  " + title + "\n\n" +
				m.st.Help.Render("y confirm · n cancel"))

	case dialogPickDir:
		var b strings.Builder
		header := "Start new session in:"
		if m.pendingResume != nil {
			header = "Original directory is gone. Resume in:"
		}
		b.WriteString(header + "\n\n")
		if len(m.dirs) == 0 {
			b.WriteString(m.st.ListMeta.Render("  (no known directories)") + "\n")
		}
		for i, d := range m.dirs {
			line := "  " + d
			if i == m.dirCursor {
				line = m.st.ListTitleSel.Render("▶ " + d)
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n" + m.dirInput.View() + "\n\n")
		b.WriteString(m.st.Help.Render("↑/↓ pick · type a path · ↵ go · esc cancel"))
		return m.st.DialogBox.Render(b.String())
	}
	return ""
}

func (m Model) View() string {
	if !m.ready {
		return "loading…"
	}
	count := fmt.Sprintf("%d sessions", m.list.Len())
	if m.loading {
		count += " · scanning…"
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		m.st.AppTitle.Render(" sm · AI Sessions  "),
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

	help := m.st.Help.Render(" ↵ resume  tab focus  n new  d delete  / filter  g group  space fold  e empty  r rescan  q quit")
	return header + "\n" + filterBar + "\n" + body + "\n" + help
}
