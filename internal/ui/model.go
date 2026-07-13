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

	"github.com/dukechain2333/ai-sessions-manager/internal/config"
	"github.com/dukechain2333/ai-sessions-manager/internal/store"
	"github.com/dukechain2333/ai-sessions-manager/internal/tmux"
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
	dialogPickAgent
	dialogKillProject
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
	agentExitMsg struct{ err error }

	tmuxTickMsg struct{}
	tmuxListMsg struct{ set map[string]bool }
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
	tmuxEnabled bool
	tmux        tmux.Runner
	tmuxLive    map[string]bool
	st          styles

	list        listPane
	preview     viewport.Model
	filterInput textinput.Model
	focus       focusArea

	tabsMode bool        // false: mixed list (default); true: per-agent tab views
	tabView  store.Agent // last active tab view, restored on re-entering tab mode

	dialog             dialogKind
	errText            string
	pendingDelete      int
	pendingResume      *store.Session
	pendingNewDir      string
	pendingKillProject string
	dirs               []string
	dirCursor          int
	dirInput           textinput.Model

	cache      *store.TranscriptCache
	enrichCh   chan store.EnrichResult
	previewFor string
	loading    bool

	width, height int
	ready         bool

	// injected for tests
	trashFn func(store.Session) (string, error)
	runCmd  func(name, dir string, args ...string) tea.Cmd

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

func New(projectsDir, codexDir string, cfg config.Config) Model {
	st := stylesWithColors(cfg.Claude, cfg.Codex)
	fi := textinput.New()
	fi.Placeholder = "filter…"
	fi.Prompt = "> "
	fi.PromptStyle = lipgloss.NewStyle().Foreground(st.Accent)
	di := textinput.New()
	di.Placeholder = "…or type a path"
	di.Prompt = "> "
	provs := []store.Provider{store.NewClaudeProvider(projectsDir)}
	if cp := store.NewCodexProvider(codexDir); cp.Available() {
		provs = append(provs, cp)
	}
	ret := Model{
		projectsDir:   projectsDir,
		st:            st,
		list:          listPane{styles: st, groupByProject: true},
		filterInput:   fi,
		dirInput:      di,
		cache:         store.NewTranscriptCache(8),
		pendingDelete: -1,
		providers:     provs,
		tmuxEnabled:   cfg.TmuxEnabled,
		tmux:          tmux.Exec{},
		trashFn: func(s store.Session) (string, error) {
			p := store.ProviderFor(provs, s.Agent)
			if p == nil {
				return "", fmt.Errorf("no provider for %s", s.Agent.Label())
			}
			return p.Trash(s)
		},
		runCmd:       execCmd,
		lastClickRow: -1,
		now:          time.Now,
	}
	if cfg.View == "tabs" {
		ret.tabsMode = true
		ret.tabView = ret.defaultTabView()
		ret.setAgentView(ret.tabView)
	}
	ret.index, ret.indexErr = store.NewSearchIndex()
	if ret.tmuxEnabled && !tmuxLookPath() {
		ret.tmuxEnabled = false
		ret.dialog = dialogError
		ret.errText = "tmux integration is enabled but tmux was not found on PATH"
	}
	return ret
}

func execCmd(name, dir string, args ...string) tea.Cmd {
	c := exec.Command(name, args...)
	c.Dir = dir
	return tea.ExecProcess(c, func(err error) tea.Msg { return agentExitMsg{err} })
}

// tmuxLookPath reports whether tmux is on PATH; overridable in tests.
var tmuxLookPath = func() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// binLookPath reports an error when an agent binary (claude/codex) is not on
// PATH. Overridable in tests so the suite does not depend on those binaries
// being installed on the runner.
var binLookPath = func(bin string) error {
	_, err := exec.LookPath(bin)
	return err
}

func (m Model) Init() tea.Cmd {
	if m.tmuxEnabled {
		return tea.Batch(m.scanCmd(), m.refreshTmuxCmd(), m.tmuxTickCmd())
	}
	return m.scanCmd()
}

func (m Model) scanCmd() tea.Cmd {
	provs := m.providers
	return func() tea.Msg {
		sessions, err := store.ScanAll(provs)
		return scanDoneMsg{sessions: sessions, err: err}
	}
}

// tmuxTickCmd schedules the next discovery poll.
func (m Model) tmuxTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tmuxTickMsg{} })
}

// refreshTmuxCmd lists live sm tmux sessions once.
func (m Model) refreshTmuxCmd() tea.Cmd {
	r := m.tmux
	return func() tea.Msg {
		set, _ := r.List()
		if set == nil {
			set = map[string]bool{}
		}
		return tmuxListMsg{set: set}
	}
}

// killOneCmd kills one tmux session and re-lists.
func (m Model) killOneCmd(name string) tea.Cmd {
	r := m.tmux
	return func() tea.Msg {
		_ = r.Kill(name)
		set, _ := r.List()
		if set == nil {
			set = map[string]bool{}
		}
		return tmuxListMsg{set: set}
	}
}

// killProjectCmd kills every live tmux belonging to project's sessions
// (named children), plus any provisional tmux whose path base is project.
func (m Model) killProjectCmd(project string) tea.Cmd {
	r := m.tmux
	sessions := append([]store.Session(nil), m.list.Sessions()...)
	return func() tea.Msg {
		set, _ := r.List()
		if set == nil {
			set = map[string]bool{}
		}
		for _, s := range sessions {
			if s.Project() != project {
				continue
			}
			name := tmuxNameFor(s)
			if set[name] {
				_ = r.Kill(name)
				delete(set, name)
			}
		}
		for name := range set {
			if !tmux.IsPending(name) {
				continue
			}
			if p, err := r.Path(name); err == nil && filepath.Base(p) == project {
				_ = r.Kill(name)
				delete(set, name)
			}
		}
		return tmuxListMsg{set: set}
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

// defaultTabView is the first tab view: Claude, unless its projects dir is
// missing while a Codex provider registered.
func (m Model) defaultTabView() store.Agent {
	if len(m.providers) > 1 && !m.providers[0].Available() {
		return store.AgentCodex
	}
	return store.AgentClaude
}

// setAgentView switches the list view and re-tints the one piece of chrome
// not re-derived every render: the filter prompt. AgentAccent("") is the
// default accent, so the mixed list keeps today's coral prompt.
func (m *Model) setAgentView(a store.Agent) {
	m.list.SetAgent(a)
	m.filterInput.PromptStyle = lipgloss.NewStyle().Foreground(m.st.AgentAccent(a))
}

// toggleViewMode flips list ⇄ tab mode. Entering tab mode restores the last
// tab view (Claude on first entry); leaving parks it and returns to the
// mixed list.
func (m *Model) toggleViewMode() {
	if m.tabsMode {
		m.tabsMode = false
		m.tabView = m.list.Agent()
		m.setAgentView("")
		return
	}
	m.tabsMode = true
	if m.tabView == "" {
		m.tabView = m.defaultTabView()
	}
	m.setAgentView(m.tabView)
}

// switchAgentView activates tab view a. No-op outside tab mode, with a
// single provider, or when a is already active. Shared by the `a` key and
// title-tab clicks.
func (m Model) switchAgentView(a store.Agent) (tea.Model, tea.Cmd) {
	if !m.tabsMode || len(m.providers) <= 1 || a == "" || a == m.list.Agent() {
		return m, nil
	}
	m.setAgentView(a)
	m.lastClickRow = -1 // rows renumbered — a stale click must not pair
	return m, m.loadTranscriptCmd()
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

// agentTab is one title-bar tab: its rendered label and the agent a click
// on it activates. View() and the mouse hit-test share this table.
type agentTab struct {
	label string
	agent store.Agent
}

// agentTabs returns the title-bar tabs (Claude first, active bracketed,
// live per-view counts), or nil in list mode / with a single provider.
func (m Model) agentTabs() []agentTab {
	if !m.tabsMode || len(m.providers) <= 1 {
		return nil
	}
	mk := func(a store.Agent) agentTab {
		label := fmt.Sprintf("%s %d", agentTitle(a), m.list.AgentTotal(a))
		if m.list.Agent() == a {
			label = "[" + label + "]"
		}
		return agentTab{label: label, agent: a}
	}
	return []agentTab{mk(store.AgentClaude), mk(store.AgentCodex)}
}

// projectLabelText is the current-project label shown at the far left of the
// bottom instruction row: " ▸ <project>  " for the selected session, or "" when
// no session is selected. It is the single source of truth for both the
// rendered label and the mouse help-bar x offset, so the two cannot drift.
func (m Model) projectLabelText() string {
	s, _, ok := m.list.Selected()
	if !ok {
		return ""
	}
	return " ▸ " + store.Truncate(s.Project(), 40) + "  "
}

// tmuxNameFor is the tmux session name sm uses for a session.
func tmuxNameFor(s store.Session) string {
	return tmux.Name(string(s.Agent), tmux.Short(s.ID))
}

// focusedBorderColor is the border color of the focused pane: the active
// view's accent in tab mode; in the mixed list, the selected session's
// agent accent (default accent with no selection), as before.
func (m Model) focusedBorderColor() lipgloss.AdaptiveColor {
	if a := m.list.Agent(); a != "" {
		return m.st.AgentAccent(a)
	}
	if s, _, ok := m.list.Selected(); ok {
		return m.st.AgentAccent(s.Agent)
	}
	return m.st.Accent
}

// projectLabelColor is the bottom-left label color: the active view's
// accent in tab mode; the majority agent of the selected session's project
// in the mixed list, as before.
func (m Model) projectLabelColor() lipgloss.AdaptiveColor {
	if a := m.list.Agent(); a != "" {
		return m.st.AgentAccent(a)
	}
	if s, _, ok := m.list.Selected(); ok {
		return m.st.AgentAccent(m.list.projectMajorityAgent(s.Project()))
	}
	return m.st.Accent
}

// projectHasLiveTmux reports whether the selected project has any live tmux.
func (m Model) projectHasLiveTmux(project string) bool {
	return m.list.projectHasLiveTmux(project)
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

// adoptPending links each live provisional new-session tmux to the newest
// matching (same cwd + agent) session that isn't already backed by a real
// sm-<agent>-<id8> tmux, renaming the provisional session to that name. It
// mutates set to reflect the renames.
func adoptPending(r tmux.Runner, sessions []store.Session, set map[string]bool) {
	backed := map[string]bool{}
	for name := range set {
		if !tmux.IsPending(name) {
			backed[name] = true
		}
	}
	for name := range set {
		if !tmux.IsPending(name) {
			continue
		}
		cwd, err := r.Path(name)
		if err != nil || cwd == "" {
			continue
		}
		agent := tmux.PendingAgent(name)
		best := -1
		for i, s := range sessions {
			if string(s.Agent) != agent || s.CWD != cwd {
				continue
			}
			target := tmux.Name(agent, tmux.Short(s.ID))
			if backed[target] || set[target] {
				continue
			}
			if best < 0 || s.LastActivity.After(sessions[best].LastActivity) {
				best = i
			}
		}
		if best < 0 {
			continue
		}
		target := tmux.Name(agent, tmux.Short(sessions[best].ID))
		if r.Rename(name, target) == nil {
			delete(set, name)
			set[target] = true
			backed[target] = true
		}
	}
}

// adoptCmd re-lists tmux, adopts provisional sessions against the given
// scanned sessions, and returns the resulting live set.
func (m Model) adoptCmd(sessions []store.Session) tea.Cmd {
	r := m.tmux
	snap := append([]store.Session(nil), sessions...)
	return func() tea.Msg {
		set, _ := r.List()
		if set == nil {
			set = map[string]bool{}
		}
		adoptPending(r, snap, set)
		return tmuxListMsg{set: set}
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
			m.errText = "cannot read sessions: " + msg.err.Error()
			return m, nil
		}
		// Every successful (re)scan revalidates the full-text index —
		// covers startup, `r`, and the rescan agentExitMsg triggers after
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
		cmds := []tea.Cmd{waitEnrich(ch), m.loadTranscriptCmd()}
		if m.tmuxEnabled {
			cmds = append(cmds, m.refreshTmuxCmd())
		}
		if m.searchAll && m.activeQuery != "" {
			m.list.SetSearchResults(nil) // never render old indices over the new ordering
			cmds = append(cmds, m.dispatchSearch())
		}
		return m, tea.Batch(cmds...)

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
		if m.tmuxEnabled {
			return m, tea.Batch(m.loadTranscriptCmd(), m.adoptCmd(m.list.Sessions()))
		}
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

	case agentExitMsg:
		// the agent exiting non-zero is normal — the user declined the trust
		// prompt, pressed Ctrl-C, or /exit'd. Only surface an error when it
		// failed to launch at all (anything that is not an *exec.ExitError).
		var exitErr *exec.ExitError
		if msg.err != nil && !errors.As(msg.err, &exitErr) {
			m.dialog = dialogError
			m.errText = "could not launch: " + msg.err.Error()
		}
		if m.tmuxEnabled {
			return m, tea.Batch(m.scanCmd(), m.refreshTmuxCmd())
		}
		return m, m.scanCmd()

	case tmuxTickMsg:
		if !m.tmuxEnabled {
			return m, nil
		}
		return m, tea.Batch(m.adoptCmd(m.list.Sessions()), m.tmuxTickCmd())

	case tmuxListMsg:
		m.tmuxLive = msg.set
		m.list.SetTmuxLive(msg.set)
		return m, nil

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
		case "a":
			if m.tabsMode {
				return m.switchAgentView(otherAgent(m.list.Agent()))
			}
			m.list.ToggleAgentGroup()
			return m, m.loadTranscriptCmd()
		case "v":
			m.toggleViewMode()
			m.lastClickRow = -1 // rows renumbered
			// Force a reload: the visible session set changed even when the
			// cursor happens to land back on the same id (same reasoning as
			// the toggleSearchLayer/Esc paths).
			m.previewFor = ""
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
		case "x":
			if !m.tmuxEnabled {
				return m, nil
			}
			if m.list.OnHeader() {
				if proj, ok := m.list.CursorProject(); ok && m.projectHasLiveTmux(proj) {
					m.pendingKillProject = proj
					m.dialog = dialogKillProject
				}
				return m, nil
			}
			if s, _, ok := m.list.Selected(); ok {
				name := tmuxNameFor(s)
				if m.tmuxLive[name] {
					return m, m.killOneCmd(name)
				}
			}
			return m, nil
		}
	}
	return m, nil
}

// runAgentCmd launches an agent, wrapping it in tmux when integration is on.
// resume != nil resumes that session; resume == nil starts a new session with
// p in cwd.
func (m Model) runAgentCmd(p store.Provider, cwd string, resume *store.Session) tea.Cmd {
	if resume != nil {
		name, args := p.ResumeCommand(*resume)
		if m.tmuxEnabled {
			sess := tmux.Name(string(resume.Agent), tmux.Short(resume.ID))
			return m.runCmd("tmux", cwd, tmux.ResumeArgs(sess, cwd, name, args)...)
		}
		return m.runCmd(name, cwd, args...)
	}
	name, args := p.NewCommand()
	if m.tmuxEnabled {
		pend := tmux.PendingName(string(p.Agent()), m.now().UnixNano())
		return m.runCmd("tmux", cwd, tmux.NewArgs(pend, cwd, name, args)...)
	}
	return m.runCmd(name, cwd, args...)
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
	p := store.ProviderFor(m.providers, s.Agent)
	if p == nil {
		m.dialog = dialogError
		m.errText = "no handler for agent " + s.Agent.Label()
		return m, nil
	}
	if err := binLookPath(p.Binary()); err != nil {
		m.dialog = dialogError
		m.errText = p.Binary() + " not found on PATH"
		return m, nil
	}
	return m, m.runAgentCmd(p, s.CWD, &s)
}

func (m Model) openNewSession() (tea.Model, tea.Cmd) {
	if s, _, ok := m.list.Selected(); ok && s.CWD != "" {
		if st, err := os.Stat(s.CWD); err == nil && st.IsDir() {
			return m.launchNewSession(s.CWD)
		}
	}
	m.pendingResume = nil
	m.openDirPicker() // no selection: fall back to dir picker, then agent pick
	return m, nil
}

// launchNewSession starts a new session in dir. In tab mode the view IS
// the agent choice, so it launches directly. In list mode: a single
// provider launches directly; two or more fall back to dialogPickAgent.
func (m Model) launchNewSession(dir string) (Model, tea.Cmd) {
	m.dialog = dialogNone
	if a := m.list.Agent(); a != "" {
		p := store.ProviderFor(m.providers, a)
		if p == nil {
			m.dialog = dialogError
			m.errText = a.Label() + " is not available"
			return m, nil
		}
		if err := binLookPath(p.Binary()); err != nil {
			m.dialog = dialogError
			m.errText = p.Binary() + " not found on PATH"
			return m, nil
		}
		return m, m.runAgentCmd(p, dir, nil)
	}
	if len(m.providers) == 1 {
		p := m.providers[0]
		if err := binLookPath(p.Binary()); err != nil {
			m.dialog = dialogError
			m.errText = p.Binary() + " not found on PATH"
			return m, nil
		}
		return m, m.runAgentCmd(p, dir, nil)
	}
	m.pendingNewDir = dir
	m.dialog = dialogPickAgent
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
				if _, err := m.trashFn(s); err != nil {
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
			m.dirInput.Blur()
			if pending != nil {
				m.dialog = dialogNone
				p := store.ProviderFor(m.providers, pending.Agent)
				if p == nil {
					m.dialog = dialogError
					m.errText = "no handler for agent " + pending.Agent.Label()
					return m, nil
				}
				if err := binLookPath(p.Binary()); err != nil {
					m.dialog = dialogError
					m.errText = p.Binary() + " not found on PATH"
					return m, nil
				}
				return m, m.runAgentCmd(p, dir, pending)
			}
			return m.launchNewSession(dir)
		}
		var cmd tea.Cmd
		m.dirInput, cmd = m.dirInput.Update(msg)
		return m, cmd

	case dialogPickAgent:
		var agent store.Agent
		switch msg.String() {
		case "1", "c":
			agent = store.AgentClaude
		case "2", "x":
			agent = store.AgentCodex
		case "esc", "n":
			m.dialog = dialogNone
			m.pendingNewDir = ""
			return m, nil
		default:
			return m, nil
		}
		p := store.ProviderFor(m.providers, agent)
		dir := m.pendingNewDir
		m.dialog = dialogNone
		m.pendingNewDir = ""
		if p == nil {
			m.dialog = dialogError
			m.errText = agent.Label() + " is not available"
			return m, nil
		}
		if err := binLookPath(p.Binary()); err != nil {
			m.dialog = dialogError
			m.errText = p.Binary() + " not found on PATH"
			return m, nil
		}
		return m, m.runAgentCmd(p, dir, nil)

	case dialogKillProject:
		proj := m.pendingKillProject
		m.pendingKillProject = ""
		m.dialog = dialogNone
		switch msg.String() {
		case "y", "enter":
			return m, m.killProjectCmd(proj)
		}
		return m, nil
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

	case dialogPickAgent:
		return m.st.DialogBox.Render(
			"New session in " + m.pendingNewDir + "\n\n" +
				"  [1] Claude    [2] Codex\n\n" +
				m.st.Help.Render("1/2 choose · esc cancel"))

	case dialogKillProject:
		n := 0
		for _, s := range m.list.Sessions() {
			if s.Project() == m.pendingKillProject && m.tmuxLive[tmuxNameFor(s)] {
				n++
			}
		}
		return m.st.DialogBox.Render(fmt.Sprintf(
			"Kill %d tmux in %s?\n\n%s", n, m.pendingKillProject,
			m.st.Help.Render("y confirm · n cancel")))
	}
	return ""
}

func (m Model) View() string {
	if !m.ready {
		return "loading…"
	}
	tabs := m.agentTabs()
	status := ""
	if tabs == nil {
		status = fmt.Sprintf("%d sessions", m.list.Len())
		if m.searchAll && m.activeQuery != "" {
			status = fmt.Sprintf("%d sessions · %d matched", len(m.list.Sessions()), m.matched)
			if !m.indexReady {
				status += "…"
			}
		}
	}
	if m.indexing {
		status += fmt.Sprintf(" · indexing %d/%d…", m.indexDone, m.indexTotal)
	}
	if m.indexFailed > 0 {
		status += fmt.Sprintf(" · %d unindexed", m.indexFailed)
	}
	if m.loading {
		status += " · scanning…"
	}
	segs := []string{
		m.st.TitleMarkFor(m.list.Agent()), // ✻ in the active view's accent
		m.st.AppTitle.Render(" sm · AI Sessions  "),
	}
	for i, tb := range tabs {
		st := m.st.Count
		if tb.agent == m.list.Agent() {
			st = lipgloss.NewStyle().Bold(true).Foreground(m.st.AgentAccent(tb.agent))
		}
		lbl := tb.label
		if i < len(tabs)-1 {
			lbl += "  " // two-space separator; tabAt (Task 6) mirrors this
		}
		segs = append(segs, st.Render(lbl))
	}
	segs = append(segs, m.st.Count.Render(status))
	header := lipgloss.JoinHorizontal(lipgloss.Top, segs...)
	filterBar := m.filterInput.View()

	var body string
	if m.dialog != dialogNone {
		body = lipgloss.Place(m.width, m.height-3, lipgloss.Center, lipgloss.Center, m.dialogView())
	} else {
		focused := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(m.focusedBorderColor())
		listStyle, prevStyle := m.st.PaneBlurred, m.st.PaneBlurred
		if m.focus == focusPreview {
			prevStyle = focused
		} else {
			listStyle = focused
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
	label := m.projectLabelText()
	labelW := lipgloss.Width(label)
	helpBudget := m.width - labelW
	if helpBudget < 0 {
		helpBudget = 0
	}
	styledLabel := lipgloss.NewStyle().Bold(true).Foreground(m.projectLabelColor()).
		MaxWidth(m.width).Render(label)
	styledHelp := m.st.Help.MaxWidth(helpBudget).Render(helpLineFor(m.helpItems()))
	return header + "\n" + filterBar + "\n" + body + "\n" + styledLabel + styledHelp
}
