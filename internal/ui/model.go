package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

const searchDebounce = 150 * time.Millisecond

type (
	searchTickMsg   struct{ seq int }
	searchResultMsg struct {
		seq  int
		hits []store.SessionHits
	}
	indexProgressMsg struct {
		p  store.IndexProgress
		ch chan store.IndexProgress
	}
	indexDoneMsg struct{ ch chan store.IndexProgress }
)

type Model struct {
	projectsDir string
	providers   []store.Provider
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

	// mouse double-click tracking; now is injected for tests
	lastClickZone zone
	lastClickRow  int
	lastClickAt   time.Time
	now           func() time.Time

	// full-text search layer
	searchAll   bool
	searchSeq   int
	activeQuery string
	matched     int
	index       store.SearchIndex
	indexErr    error
	indexReady  bool
	indexing    bool
	indexStale  bool
	indexDone   int
	indexTotal  int
	indexFailed int
	indexCh     chan store.IndexProgress

	// preview hit navigation
	msgStarts []int
	hitMsgs   []int
	curHit    int
}

func New(projectsDir, codexDir string) Model {
	st := defaultStyles()
	fi := textinput.New()
	fi.Placeholder = "filter…"
	fi.Prompt = "> "
	fi.PromptStyle = lipgloss.NewStyle().Foreground(st.Accent)
	di := textinput.New()
	di.Placeholder = "…or type a path"
	di.Prompt = "> "
	ret := Model{
		projectsDir:   projectsDir,
		st:            st,
		list:          listPane{styles: st, groupByProject: true},
		filterInput:   fi,
		dirInput:      di,
		cache:         store.NewTranscriptCache(8),
		pendingDelete: -1,
		trashFn:       store.TrashSession,
		runClaude:     execClaude,
		lastClickRow:  -1,
		now:           time.Now,
	}
	provs := []store.Provider{store.NewClaudeProvider(projectsDir)}
	if cp := store.NewCodexProvider(codexDir); cp.Available() {
		provs = append(provs, cp)
	}
	ret.providers = provs
	ret.index, ret.indexErr = store.NewSearchIndex()
	return ret
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
	provs := m.providers
	return func() tea.Msg {
		sessions, err := store.ScanAll(provs)
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

// toggleSearchLayer flips between the title fuzzy filter and the full-text
// layer. Shared by Tab in the filter and the "> " prompt glyph click.
func (m *Model) toggleSearchLayer() tea.Cmd {
	if m.indexErr != nil {
		m.dialog = dialogError
		m.errText = "search index unavailable: " + m.indexErr.Error()
		return nil
	}
	m.searchAll = !m.searchAll
	if m.searchAll {
		m.filterInput.Placeholder = "search…"
		m.list.SetFilter("")
		m.indexReady = false // re-entering full-text mode re-checks validity keys (spec)
		if m.indexing {
			m.indexStale = true
		}
		return m.dispatchSearch()
	}
	m.filterInput.Placeholder = "filter…"
	m.matched = 0
	m.list.SetSearchResults(nil)
	m.list.SetFilter(m.filterInput.Value())
	// Same reasoning as the Esc path: force a reload so a selection that
	// lands back on the same session doesn't keep the stale highlighted
	// render (and its hitMsgs/n-N state) alive.
	m.previewFor = ""
	m.hitMsgs = nil
	m.curHit = 0
	return m.loadTranscriptCmd()
}

// dispatchSearch starts (or restarts) the debounce clock for the current
// query. Empty queries clear results immediately.
func (m *Model) dispatchSearch() tea.Cmd {
	m.searchSeq++
	q := strings.TrimSpace(m.filterInput.Value())
	if q == "" {
		m.activeQuery = ""
		m.matched = 0
		m.list.SetSearchResults(nil)
		return m.loadTranscriptCmd()
	}
	seq := m.searchSeq
	return tea.Tick(searchDebounce, func(time.Time) tea.Msg { return searchTickMsg{seq: seq} })
}

// runSearch is the async search over the index cache. It snapshots the
// sessions slice on the Update goroutine first — the enrich pump mutates
// session structs in place, so the search goroutine must never read the
// live slice (same reason Enrich itself snapshots up front).
func (m *Model) runSearch(seq int) tea.Cmd {
	ix, q := m.index, m.filterInput.Value()
	sessions := append([]store.Session(nil), m.list.Sessions()...)
	return func() tea.Msg {
		hits, _ := ix.Search(q, sessions)
		return searchResultMsg{seq: seq, hits: hits}
	}
}

func waitIndex(ch chan store.IndexProgress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return indexDoneMsg{ch: ch}
		}
		return indexProgressMsg{p: p, ch: ch}
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
	cache, path, id, agent := m.cache, s.Path, s.ID, s.Agent
	provs := m.providers
	return func() tea.Msg {
		t, err := cache.Get(path, func() (store.Transcript, error) {
			p := store.ProviderFor(provs, agent)
			if p == nil {
				return store.Transcript{}, fmt.Errorf("no provider for %s", agent.Label())
			}
			return p.ParseTranscript(path)
		})
		t.SessionID = id
		return transcriptMsg{id: id, t: t, err: err}
	}
}

// narrow reports whether the terminal is too narrow for two panes;
// below 80 columns the preview pane is hidden (per spec).
func (m Model) narrow() bool { return m.width < 80 }

// bodyHeight is the pane content height: total height minus title, filter,
// help, and the panes' top/bottom border rows.
func (m *Model) bodyHeight() int {
	h := m.height - 5
	if h < 3 {
		return 3
	}
	return h
}

// paneWidths returns the outer widths of the list and preview panes.
// layout() and mouse hit-testing must agree on these, so they live here.
func (m *Model) paneWidths() (listW, previewW int) {
	listW = m.width * 2 / 5
	if listW < 20 {
		listW = 20
	}
	if m.narrow() {
		listW = m.width - 2
	}
	previewW = m.width - listW - 2
	if previewW < 10 {
		previewW = 10
	}
	return listW, previewW
}

// Layout: 1 header row + 1 filter row + body + 1 help row; borders eat
// 2 rows/cols per pane.
func (m *Model) layout() {
	bodyH := m.bodyHeight()
	listW, previewW := m.paneWidths()
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
		// Every successful (re)scan revalidates the full-text index —
		// covers startup, `r`, and the rescan claudeExitMsg triggers after
		// a resumed session's mtime/size may have changed. An EnsureAll
		// build already in flight when this lands cannot be trusted as
		// "ready" once it completes (it was dispatched against the
		// pre-scan session list); indexStale flags that for indexDoneMsg.
		m.indexReady = false
		if m.indexing {
			m.indexStale = true
		}
		m.list.SetSessions(msg.sessions)
		m.lastClickRow = -1 // rows renumbered — stale click index must not pair into a double-click
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
		store.Enrich(msg.sessions, m.providers, 8, ch)
		if m.searchAll && m.activeQuery != "" {
			m.list.SetSearchResults(nil) // never render old indices over the new ordering
			return m, tea.Batch(waitEnrich(ch), m.loadTranscriptCmd(), m.dispatchSearch())
		}
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
			// Enrichment can flip a session to Empty and drop it from the
			// visible rows, renumbering them. Invalidate any pending click so a
			// second click at the same coordinates can't pair with a stale row
			// index and resume the wrong session.
			m.lastClickRow = -1
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
		tr := msg.t
		// tr is a copy of the message slice header, but tr.Messages[i].Text =
		// … below mutates the shared backing array of the cached transcript.
		// Deep-copy the messages first so highlighting never poisons the cache.
		msgs := make([]store.Message, len(tr.Messages))
		copy(msgs, tr.Messages)
		tr.Messages = msgs
		m.hitMsgs = nil
		m.curHit = 0
		terms := store.SplitTerms(m.activeQuery)
		if m.searchAll && len(terms) > 0 {
			for i := range tr.Messages {
				if tr.Messages[i].Kind == store.KindTool {
					continue
				}
				lower := strings.ToLower(tr.Messages[i].Text)
				for _, t := range terms {
					if strings.Contains(lower, t) {
						tr.Messages[i].Text = highlightTerms(tr.Messages[i].Text, terms)
						m.hitMsgs = append(m.hitMsgs, i)
						break
					}
				}
			}
		}
		content, starts := renderTranscript(tr, m.preview.Width, m.st)
		m.msgStarts = starts
		m.preview.SetContent(content)
		m.preview.GotoTop()
		if len(m.hitMsgs) > 0 {
			m.preview.SetYOffset(m.msgStarts[m.hitMsgs[0]])
		}
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

	case searchTickMsg:
		if !m.searchAll || msg.seq != m.searchSeq {
			return m, nil
		}
		m.activeQuery = strings.TrimSpace(m.filterInput.Value())
		var cmds []tea.Cmd
		if !m.indexReady && !m.indexing {
			ch := make(chan store.IndexProgress, 8)
			m.indexCh = ch
			m.indexing = true
			m.indexDone, m.indexTotal = 0, len(m.list.Sessions())
			m.indexFailed = 0
			provs := m.providers
			m.index.EnsureAll(m.list.Sessions(), func(s store.Session) (store.Transcript, error) {
				p := store.ProviderFor(provs, s.Agent)
				if p == nil {
					return store.Transcript{}, fmt.Errorf("no provider for %s", s.Agent.Label())
				}
				return p.ParseTranscript(s.Path)
			}, 4, ch)
			cmds = append(cmds, waitIndex(ch))
		}
		cmds = append(cmds, m.runSearch(msg.seq))
		return m, tea.Batch(cmds...)

	case searchResultMsg:
		if !m.searchAll || msg.seq != m.searchSeq {
			return m, nil
		}
		m.matched = len(msg.hits)
		m.list.SetSearchResults(msg.hits)
		m.lastClickRow = -1 // rows renumbered — stale indexes must not pair (same precedent as the fold path in clickList)
		// the highlight set depends on the query, not just the session —
		// force the preview to re-render
		m.previewFor = ""
		return m, m.loadTranscriptCmd()

	case indexProgressMsg:
		if msg.ch != m.indexCh {
			return m, nil
		}
		m.indexDone, m.indexTotal = msg.p.Done, msg.p.Total
		if msg.p.Err != nil {
			m.indexFailed++
		}
		return m, waitIndex(msg.ch)

	case indexDoneMsg:
		if msg.ch != m.indexCh {
			return m, nil
		}
		if m.indexStale {
			// An invalidation landed while this build was in flight; it was
			// dispatched against state we now know is outdated, so it
			// cannot be trusted as "ready". Leave indexReady false and, if
			// a search is still active, dispatch through the debounce path
			// (not runSearch directly) so the next tick re-kicks EnsureAll.
			m.indexStale = false
			m.indexing = false
			if m.searchAll && m.activeQuery != "" {
				return m, m.dispatchSearch()
			}
			return m, nil
		}
		m.indexReady = true
		m.indexing = false
		if m.searchAll && m.activeQuery != "" {
			return m, m.runSearch(m.searchSeq)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
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
			if m.searchAll {
				m.searchAll = false
				m.filterInput.Placeholder = "filter…"
				m.matched = 0
				m.activeQuery = ""
				m.list.SetSearchResults(nil)
				// Force the preview to reload: without this, a selection
				// that lands back on the same session would keep showing
				// the stale highlighted render (and its hitMsgs/n-N state)
				// because loadTranscriptCmd short-circuits on an unchanged
				// previewFor.
				m.previewFor = ""
				m.hitMsgs = nil
				m.curHit = 0
			}
			return m, m.loadTranscriptCmd()
		case tea.KeyEnter:
			m.filterInput.Blur()
			m.focus = focusList
			return m, nil
		case tea.KeyDown:
			m.filterInput.Blur()
			m.focus = focusList
			return m, nil
		case tea.KeyTab:
			return m, m.toggleSearchLayer()
		}
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		if m.searchAll {
			return m, tea.Batch(cmd, m.dispatchSearch())
		}
		m.list.SetFilter(m.filterInput.Value())
		return m, tea.Batch(cmd, m.loadTranscriptCmd())

	case focusPreview:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "tab", "esc":
			m.focus = focusList
			return m, nil
		case "n", "N":
			if len(m.hitMsgs) > 0 {
				if msg.String() == "n" {
					m.curHit = (m.curHit + 1) % len(m.hitMsgs)
				} else {
					m.curHit = (m.curHit - 1 + len(m.hitMsgs)) % len(m.hitMsgs)
				}
				m.preview.SetYOffset(m.msgStarts[m.hitMsgs[m.curHit]])
				return m, nil
			}
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
			if m.list.cursor == 0 {
				// walking up past the top row enters the search bar
				m.focus = focusFilter
				m.filterInput.Focus()
				return m, nil
			}
			m.list.MoveCursor(-1)
			return m, m.loadTranscriptCmd()
		case "s":
			// s = search: focus the bar on the full-text layer. / stays the
			// title-filter entry; s never flips an already-on layer back.
			m.focus = focusFilter
			m.filterInput.Focus()
			if !m.searchAll {
				return m, m.toggleSearchLayer()
			}
			return m, nil
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
			// indexReady reset now happens centrally in scanDoneMsg's
			// success path (covers every rescan source, not just this key).
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
			if m.searchAll && m.activeQuery != "" {
				// dispatchSearch self-bumps the seq — orphaning any in-flight
				// result computed against pre-delete indices — and its tick
				// path revalidates the index when needed.
				return m, tea.Batch(m.loadTranscriptCmd(), m.dispatchSearch())
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
	if m.searchAll && m.activeQuery != "" {
		count = fmt.Sprintf("%d sessions · %d matched", len(m.list.Sessions()), m.matched)
		if !m.indexReady {
			count += "…"
		}
	}
	if m.indexing {
		count += fmt.Sprintf(" · indexing %d/%d…", m.indexDone, m.indexTotal)
	}
	if m.indexFailed > 0 {
		count += fmt.Sprintf(" · %d unindexed", m.indexFailed)
	}
	if m.loading {
		count += " · scanning…"
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		m.st.TitleMark(), // ✻ in accent
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
			// Pad the list to the full body height. Without this the frame is
			// only as tall as the list, the help bar floats above the bottom
			// row, and mouse hit-testing (which assumes help is the last row)
			// maps clicks on the blank strip to help-bar actions.
			body = listStyle.Height(m.bodyHeight()).Render(m.list.View())
		} else {
			body = lipgloss.JoinHorizontal(lipgloss.Top,
				listStyle.Render(m.list.View()),
				prevStyle.Render(m.preview.View()),
			)
		}
	}

	// Clamp to the terminal width so a help line wider than the screen
	// truncates cleanly instead of wrapping onto another row (which would
	// corrupt the alt-screen frame). The full bar needs ~105 columns.
	help := m.st.Help.MaxWidth(m.width).Render(helpLine())
	return header + "\n" + filterBar + "\n" + body + "\n" + help
}
