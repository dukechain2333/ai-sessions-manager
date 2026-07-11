package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/ai-sessions-manager/internal/store"
)

// searchModel returns a model whose sessions point at real tiny .jsonl
// files and whose index lives in a temp dir, with the full-text layer on.
func searchModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel()
	m.index = store.SearchIndex{Dir: t.TempDir()}
	m.indexErr = nil
	dir := t.TempDir()
	write := func(name string, texts ...string) string {
		p := filepath.Join(dir, name+".jsonl")
		var b strings.Builder
		for _, text := range texts {
			b.WriteString(`{"type":"user","message":{"role":"user","content":"` + text + `"}}` + "\n")
		}
		if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// s1: one hit message; s2: two hit messages (MsgHits counts MESSAGES,
	// so s2 must outrank s1 despite s1 being more recent).
	m.list.sessions[0].Path = write("s1", "the quick brown fox")
	m.list.sessions[1].Path = write("s2", "quick one", "quick two")
	for _, s := range m.list.sessions[:2] {
		if err := m.index.EnsureSession(s.Path); err != nil {
			t.Fatal(err)
		}
	}
	m.indexReady = true
	return m
}

func typeInto(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m2, _ := m.Update(key(string(r)))
		m = m2.(Model)
	}
	return m
}

// runCmds executes a cmd synchronously, flattening one level of BatchMsg,
// and returns every message produced. A scheduled debounce tick genuinely
// elapses here — that is the point: only a really-scheduled tick may drive
// the heal path in the rescan test below.
func runCmds(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, c := range batch {
		if c != nil {
			msgs = append(msgs, c())
		}
	}
	return msgs
}

func TestTabTogglesSearchLayer(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	if !m.searchAll {
		t.Fatal("tab in the filter must enable the full-text layer")
	}
	if m.filterInput.Placeholder != "search…" {
		t.Errorf("placeholder = %q, want search…", m.filterInput.Placeholder)
	}
	if m.focus != focusFilter {
		t.Error("layer toggle must keep the filter focused")
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	if m.searchAll || m.filterInput.Placeholder != "filter…" {
		t.Error("second tab must switch back to the title layer")
	}
}

func TestEscResetsToTitleLayer(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m = typeInto(t, m, "quick")
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	if m.searchAll {
		t.Error("esc must reset to the title layer")
	}
	if m.list.search != nil {
		t.Error("esc must clear search results")
	}
	if m.filterInput.Value() != "" || m.focus != focusList {
		t.Error("esc keeps its existing clear+blur behavior")
	}
}

func TestSearchAllDoesNotFuzzyFilter(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m = typeInto(t, m, "zzznothing")
	if m.list.filter != "" {
		t.Error("typing in the full-text layer must not drive the fuzzy filter")
	}
}

func TestSearchPipelineEndToEnd(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m.indexReady = true // this test isn't exercising the entry-triggered indexing pass itself
	m = typeInto(t, m, "quick")
	// drive the debounce deterministically: fire the tick for the live seq
	m2, cmd := m.Update(searchTickMsg{seq: m.searchSeq})
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("live tick must return the async search cmd")
	}
	msg := cmd() // run the search synchronously
	res, ok := msg.(searchResultMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want searchResultMsg", msg)
	}
	m.lastClickRow = 1 // M5 setup: simulate a stale click recorded before this refresh
	m2, _ = m.Update(res)
	m = m2.(Model)
	if m.matched != 2 {
		t.Fatalf("matched = %d, want 2", m.matched)
	}
	// M5: a fresh search result renumbers rows — a stale lastClickRow must
	// not survive to pair with a click on the new layout.
	if m.lastClickRow != -1 {
		t.Error("a fresh search result must reset lastClickRow (rows renumbered — same precedent as the fold path in clickList)")
	}
	if s, _, ok := m.list.Selected(); !ok || s.ID != "s2" {
		t.Errorf("s2 has more hits and must rank first, got %v", s.ID)
	}
	if !strings.Contains(m.View(), "· 2 matched") {
		t.Error("title bar must show the matched count")
	}
}

func TestStaleTickAndResultIgnored(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m = typeInto(t, m, "qu") // seq bumps per keystroke
	stale := m.searchSeq - 1 // an older debounce tick
	m2, cmd := m.Update(searchTickMsg{seq: stale})
	m = m2.(Model)
	if cmd != nil {
		t.Error("stale tick must be dropped")
	}
	m2, _ = m.Update(searchResultMsg{seq: stale, hits: []store.SessionHits{{Session: 0, MsgHits: 9}}})
	m = m2.(Model)
	if m.list.search != nil {
		t.Error("stale result must be dropped")
	}
}

func TestIndexingProgressShownAndSearchRedispatched(t *testing.T) {
	m := searchModel(t)
	m.indexReady = false
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m = typeInto(t, m, "quick")
	m2, cmd := m.Update(searchTickMsg{seq: m.searchSeq})
	m = m2.(Model)
	if !m.indexing || cmd == nil {
		t.Fatal("first full-text search must kick off indexing plus the partial search")
	}
	ch := m.indexCh
	m2, _ = m.Update(indexProgressMsg{p: store.IndexProgress{Done: 1, Total: 2}, ch: ch})
	m = m2.(Model)
	if !strings.Contains(m.View(), "indexing 1/2…") {
		t.Errorf("title bar must show indexing progress, view head: %.120s", m.View())
	}
	m2, cmd = m.Update(indexDoneMsg{ch: ch})
	m = m2.(Model)
	if !m.indexReady || m.indexing {
		t.Error("indexDoneMsg must mark the index ready")
	}
	if cmd == nil {
		t.Error("index completion must re-dispatch the search")
	}
}

// TestIndexFailureShownAsUnindexed is the I1 regression: extraction
// failures reported through IndexProgress.Err were discarded, leaving
// failed sessions silently unsearchable with no visible indication.
func TestIndexFailureShownAsUnindexed(t *testing.T) {
	m := searchModel(t)
	m.indexReady = false
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m = typeInto(t, m, "quick")
	m2, _ = m.Update(searchTickMsg{seq: m.searchSeq})
	m = m2.(Model)
	if !m.indexing {
		t.Fatal("setup: expected indexing to have kicked off")
	}
	ch := m.indexCh
	m2, _ = m.Update(indexProgressMsg{p: store.IndexProgress{Done: 1, Total: 2, Err: errors.New("boom")}, ch: ch})
	m = m2.(Model)
	if !strings.Contains(m.View(), "1 unindexed") {
		t.Errorf("title bar must show the unindexed count, view head: %.160s", m.View())
	}
}

func TestRunSearchSnapshotsSessions(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m.indexReady = true // this test isn't exercising the entry-triggered indexing pass itself
	m = typeInto(t, m, "quick")
	m2, cmd := m.Update(searchTickMsg{seq: m.searchSeq})
	m = m2.(Model)
	m.list.sessions[1].Path = "/mutated/after/dispatch.jsonl" // enrich-style in-place mutation
	msg := cmd()
	res, ok := msg.(searchResultMsg)
	if !ok {
		t.Fatalf("got %T", msg)
	}
	if len(res.hits) != 2 {
		t.Errorf("snapshot must shield the in-flight search from mutations: hits=%v", res.hits)
	}
}

func TestRescanRefreshesActiveSearch(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m.indexReady = true // arrange: the rescan below is what exercises revalidation, not this first search
	m = typeInto(t, m, "quick")
	m2, cmd := m.Update(searchTickMsg{seq: m.searchSeq})
	m = m2.(Model)
	m2, _ = m.Update(cmd().(searchResultMsg))
	m = m2.(Model)
	if m.list.search == nil {
		t.Fatal("setup: expected active results")
	}
	seqBefore := m.searchSeq
	m.indexReady = false // scanDoneMsg's success path resets this on every rescan (including r's); simulated here since we drive scanDoneMsg directly, bypassing the real scanCmd
	reordered := append([]store.Session(nil), m.list.Sessions()...)
	reordered[0], reordered[1] = reordered[1], reordered[0] // rescan re-sorts
	m2, cmd = m.Update(scanDoneMsg{sessions: reordered})
	m = m2.(Model)
	if m.list.search != nil {
		t.Error("rescan must clear results computed against the old ordering")
	}
	if m.searchSeq <= seqBefore {
		t.Error("rescan must advance searchSeq to orphan in-flight results")
	}
	// The re-search must arrive via a really-scheduled debounce tick — a
	// direct search would skip the tick path's EnsureAll revalidation kick.
	var tick searchTickMsg
	gotTick := false
	for _, msg := range runCmds(t, cmd) {
		switch msg := msg.(type) {
		case searchResultMsg:
			t.Error("rescan must not search directly; revalidation would be skipped")
		case searchTickMsg:
			tick, gotTick = msg, true
		}
	}
	if !gotTick {
		t.Fatal("rescan with an active query must schedule the debounce tick")
	}
	if tick.seq != m.searchSeq {
		t.Fatalf("scheduled tick seq = %d, want live %d", tick.seq, m.searchSeq)
	}
	m2, cmd = m.Update(tick)
	m = m2.(Model)
	if !m.indexing {
		t.Error("the rescan's tick must kick index revalidation (EnsureAll)")
	}
	if cmd == nil {
		t.Fatal("live tick must return the async search cmd")
	}
	var res searchResultMsg
	gotRes := false
	for _, msg := range runCmds(t, cmd) {
		if r, ok := msg.(searchResultMsg); ok {
			res, gotRes = r, true
		}
	}
	if !gotRes {
		t.Fatal("the rescan's tick must produce a search result")
	}
	m2, _ = m.Update(res)
	m = m2.(Model)
	if m.list.search == nil || m.matched != 2 {
		t.Errorf("heal must repopulate results against the new ordering, matched=%d", m.matched)
	}
}

func TestDeleteInvalidatesInFlightSearch(t *testing.T) {
	m := searchModel(t)
	m.trashFn = func(string, store.Session) (string, error) { return "/trash/x", nil }
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m.indexReady = true // this test isn't exercising the entry-triggered indexing pass itself
	m = typeInto(t, m, "quick")
	m2, cmd := m.Update(searchTickMsg{seq: m.searchSeq})
	m = m2.(Model)
	inflight := cmd                                  // result computed against pre-delete indices
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // leave filter focus
	m = m2.(Model)
	m2, _ = m.Update(key("d"))
	m = m2.(Model)
	m2, _ = m.Update(key("y"))
	m = m2.(Model)
	m2, _ = m.Update(inflight().(searchResultMsg)) // stale seq now
	m = m2.(Model)
	if m.list.search != nil && len(m.list.search) == 2 {
		t.Error("stale in-flight result must not be applied after a delete")
	}
}

func TestPreviewJumpsAndCyclesHits(t *testing.T) {
	m := searchModel(t)
	m.searchAll = true
	m.activeQuery = "quick"
	// Each filler renders ~25 wrapped lines at the fixture's preview width
	// (58), so consecutive hits land at viewport offsets that stay distinct
	// even after SetYOffset clamping (content ≫ viewport height 25).
	long := strings.Repeat("filler words here ", 80)
	tr := store.Transcript{SessionID: "s1", Messages: []store.Message{
		{Kind: store.KindAssistant, Text: long},
		{Kind: store.KindUser, Text: "the quick brown fox"},
		{Kind: store.KindAssistant, Text: long},
		{Kind: store.KindUser, Text: "quick again"},
	}}
	m.previewFor = "s1"
	m2, _ := m.Update(transcriptMsg{id: "s1", t: tr})
	m = m2.(Model)
	if len(m.hitMsgs) != 2 || m.hitMsgs[0] != 1 || m.hitMsgs[1] != 3 {
		t.Fatalf("hitMsgs = %v, want [1 3]", m.hitMsgs)
	}
	if m.preview.YOffset == 0 {
		t.Error("preview must jump to the first hit (message 1 sits below a long message)")
	}
	if !strings.Contains(m.preview.View(), "\x1b[7m") {
		t.Error("hit terms must be reverse-video highlighted")
	}
	first := m.preview.YOffset
	m.focus = focusPreview
	m2, _ = m.Update(key("n"))
	m = m2.(Model)
	if m.preview.YOffset <= first {
		t.Error("n must jump to the next hit further down")
	}
	m2, _ = m.Update(key("n")) // wraps to the first hit
	m = m2.(Model)
	if m.preview.YOffset != first {
		t.Errorf("n past the last hit must wrap: YOffset=%d want %d", m.preview.YOffset, first)
	}
	m2, _ = m.Update(key("N")) // back to the last hit
	m = m2.(Model)
	if m.preview.YOffset <= first {
		t.Error("N must wrap backwards to the last hit")
	}
}

func TestPreviewNoQueryNoHighlight(t *testing.T) {
	m := searchModel(t)
	tr := store.Transcript{SessionID: "s1", Messages: []store.Message{{Kind: store.KindUser, Text: "quick"}}}
	m.previewFor = "s1"
	m2, _ := m.Update(transcriptMsg{id: "s1", t: tr})
	m = m2.(Model)
	if strings.Contains(m.preview.View(), "\x1b[7m") {
		t.Error("no active query → no highlight")
	}
	if m.preview.YOffset != 0 {
		t.Error("no active query → no jump")
	}
}

func TestQueryChangeRefreshesPreviewHighlights(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m.indexReady = true // this test isn't exercising the entry-triggered indexing pass itself
	m = typeInto(t, m, "quick")
	m2, cmd := m.Update(searchTickMsg{seq: m.searchSeq})
	m = m2.(Model)
	m2, cmd = m.Update(cmd().(searchResultMsg))
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("setup: first result must reload the preview")
	}
	m2, _ = m.Update(cmd()) // transcriptMsg for the selected session (s2)
	m = m2.(Model)
	if len(m.hitMsgs) != 2 {
		t.Fatalf("setup: query 'quick' must hit both s2 messages, got %v", m.hitMsgs)
	}
	// change the query without changing the selection: only msg 1 ("quick two") contains "two"
	m.filterInput.SetValue("two")
	_ = m.dispatchSearch() // bumps searchSeq; the live tick is fed manually below
	m2, cmd = m.Update(searchTickMsg{seq: m.searchSeq})
	m = m2.(Model)
	m2, cmd = m.Update(cmd().(searchResultMsg))
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("query change with unchanged selection must still reload the preview")
	}
	m2, _ = m.Update(cmd())
	m = m2.(Model)
	if len(m.hitMsgs) != 1 || m.hitMsgs[0] != 1 {
		t.Errorf("hitMsgs = %v, want [1] for query 'two' (stale highlights not refreshed)", m.hitMsgs)
	}
}

// TestClaudeExitRevalidatesIndex is the C1 regression: search → resume a
// session → chat → exit rescans the session list, but must also revalidate
// the full-text index (the resumed session's mtime/size may have changed
// while claude was running). Before the fix, only the `r` key did this.
func TestClaudeExitRevalidatesIndex(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m.indexReady = true // arrange: this test isn't exercising the entry-triggered indexing pass itself
	m = typeInto(t, m, "quick")
	m2, cmd := m.Update(searchTickMsg{seq: m.searchSeq})
	m = m2.(Model)
	m2, _ = m.Update(cmd().(searchResultMsg))
	m = m2.(Model)
	if m.list.search == nil {
		t.Fatal("setup: expected active search results")
	}

	// The returned scanCmd is irrelevant (it would hit the fake projects
	// dir) — drive the rescan's completion directly with the same sessions,
	// the way the real scanCmd's result would look after a resumed
	// session's mtime/size changed.
	m2, _ = m.Update(claudeExitMsg{})
	m = m2.(Model)
	m2, cmd = m.Update(scanDoneMsg{sessions: m.list.Sessions()})
	m = m2.(Model)

	var tick searchTickMsg
	gotTick := false
	for _, msg := range runCmds(t, cmd) {
		if tk, ok := msg.(searchTickMsg); ok {
			tick, gotTick = tk, true
		}
	}
	if !gotTick {
		t.Fatal("the post-exit rescan with an active query must schedule the debounce tick")
	}
	m2, _ = m.Update(tick)
	m = m2.(Model)
	if !m.indexing {
		t.Error("the rescan after claude exits must revalidate the index (EnsureAll re-kicked)")
	}
}

// TestToggleOnRevalidates is the C1 regression for spec's "re-entering
// full-text mode … re-checks validity keys": toggling the layer off must
// not itself disturb indexReady, but toggling it back on must.
func TestToggleOnRevalidates(t *testing.T) {
	m := searchModel(t)
	// Arrange: already in the full-text layer with a previously built,
	// ready index (searchModel sets indexReady=true) — bypass
	// toggleSearchLayer itself so entering this state doesn't touch it.
	m.searchAll = true
	m.focus = focusFilter // Tab only toggles the layer while the filter is focused
	if !m.indexReady {
		t.Fatal("setup: expected a ready index")
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab}) // layer off
	m = m2.(Model)
	if m.searchAll {
		t.Fatal("setup: expected the layer off")
	}
	if !m.indexReady {
		t.Fatal("setup: toggling off must not itself touch indexReady")
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // layer on again
	m = m2.(Model)
	if !m.searchAll {
		t.Fatal("setup: expected the layer on again")
	}
	if m.indexReady {
		t.Error("re-entering the full-text layer must revalidate the index (indexReady must go false)")
	}
}

// TestStaleBuildDoesNotMarkReady is the C1 regression for the indexDoneMsg
// overwrite subtlety: an EnsureAll build in flight when a new invalidation
// lands (e.g. a rescan completing concurrently) must not be trusted as
// "ready" once it completes — it was dispatched against pre-invalidation
// state.
func TestStaleBuildDoesNotMarkReady(t *testing.T) {
	m := searchModel(t)
	m.indexReady = false
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m = typeInto(t, m, "quick")
	m2, _ = m.Update(searchTickMsg{seq: m.searchSeq}) // kicks EnsureAll: indexing=true
	m = m2.(Model)
	if !m.indexing {
		t.Fatal("setup: expected indexing to be in flight")
	}
	ch := m.indexCh

	// An invalidation lands mid-build (e.g. a rescan completing concurrently).
	m2, _ = m.Update(scanDoneMsg{sessions: m.list.Sessions()})
	m = m2.(Model)
	if !m.indexStale {
		t.Fatal("setup: expected the in-flight build to be marked stale")
	}

	m2, _ = m.Update(indexDoneMsg{ch: ch})
	m = m2.(Model)
	if m.indexReady {
		t.Error("a build that went stale mid-flight must not be marked ready")
	}
	if m.indexing {
		t.Error("indexDoneMsg must still clear indexing even for a stale build")
	}
}

// TestEscClearsStaleHighlights is the I2 regression: esc out of the
// full-text layer must not leave the previous highlighted render (and its
// hitMsgs/n-N state) live when the selection happens to land back on the
// same session. The query "fox" matches only s1, and s1 is also the
// natural top pick in the default (non-search) grouped view, so the
// selection is unchanged across the esc — the exact condition under which
// loadTranscriptCmd would otherwise skip the reload (s.ID == previewFor).
func TestEscClearsStaleHighlights(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
	m.indexReady = true // this test isn't exercising the entry-triggered indexing pass itself
	m = typeInto(t, m, "fox")
	m2, cmd := m.Update(searchTickMsg{seq: m.searchSeq})
	m = m2.(Model)
	m2, _ = m.Update(cmd().(searchResultMsg))
	m = m2.(Model)
	s, _, ok := m.list.Selected()
	if !ok || s.ID != "s1" {
		t.Fatalf("setup: expected s1 selected by the single-hit search, got %v ok=%v", s.ID, ok)
	}
	tr := store.Transcript{SessionID: "s1", Messages: []store.Message{
		{Kind: store.KindUser, Text: "the quick brown fox"},
	}}
	m2, _ = m.Update(transcriptMsg{id: "s1", t: tr})
	m = m2.(Model)
	if len(m.hitMsgs) == 0 || !strings.Contains(m.preview.View(), "\x1b[7m") {
		t.Fatal("setup: expected the preview to be highlighted")
	}

	m2, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	if len(m.hitMsgs) != 0 {
		t.Error("esc must clear hitMsgs immediately, even before the reload lands")
	}
	for _, msg := range runCmds(t, cmd) {
		m2, _ = m.Update(msg)
		m = m2.(Model)
	}
	if after, _, _ := m.list.Selected(); after.ID != "s1" {
		t.Fatalf("setup invariant broken: selection moved off s1 (%v) — test no longer exercises the same-session path", after.ID)
	}
	if strings.Contains(m.preview.View(), "\x1b[7m") {
		t.Error("esc must clear the stale highlighted render even when the selection lands on the same session")
	}
}

func TestClickSearchIconTogglesLayer(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(click(1, 1)) // the prompt glyph
	m = m2.(Model)
	if !m.searchAll {
		t.Fatal("clicking the prompt glyph must enable the full-text layer")
	}
	if m.focus != focusFilter || !m.filterInput.Focused() {
		t.Error("icon click must also focus the input")
	}
	m2, _ = m.Update(click(10, 1)) // bar body: focus only, no toggle
	m = m2.(Model)
	if !m.searchAll {
		t.Error("clicking the bar body must not toggle the layer")
	}
	m2, _ = m.Update(click(1, 1))
	m = m2.(Model)
	if m.searchAll {
		t.Error("second icon click must toggle back")
	}
}

func TestPromptZoneNarrowedToPromptGlyph(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(click(2, 1)) // column 2 is now input area, not the prompt
	m = m2.(Model)
	if m.searchAll {
		t.Fatal("clicking column 2 must not toggle the layer (prompt is columns 0-1)")
	}
	if m.focus != focusFilter {
		t.Error("clicking the bar must still focus the filter")
	}
	m2, _ = m.Update(click(1, 1))
	m = m2.(Model)
	if !m.searchAll {
		t.Error("clicking the prompt glyph must still toggle")
	}
}

func TestClaudeThemeChrome(t *testing.T) {
	m := newTestModel()
	view := m.View()
	if !strings.Contains(view, "✻ sm · AI Sessions") {
		t.Errorf("title must carry the ✻ mark; head: %.80s", view)
	}
	if !strings.Contains(view, "> ") || strings.Contains(view, "🔍") {
		t.Error("filter prompt must be '> ', not 🔍")
	}
}

func TestSKeyFocusesFullTextSearch(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("s"))
	m = m2.(Model)
	if m.focus != focusFilter || !m.filterInput.Focused() {
		t.Fatal("s must focus the search bar")
	}
	if !m.searchAll || m.filterInput.Placeholder != "search…" {
		t.Fatal("s must land on the full-text layer")
	}
	// leave the bar with the layer still on, press s again from the list
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	m2, _ = m.Update(key("s"))
	m = m2.(Model)
	if !m.searchAll {
		t.Error("s with the layer already on must not flip it back off")
	}
	if m.focus != focusFilter {
		t.Error("s must still focus the bar")
	}
}

func TestUpAtTopEntersBarDownReturns(t *testing.T) {
	m := searchModel(t)         // grouped: row 0 = alpha header, row 1 = s1 (initial cursor)
	m2, _ := m.Update(key("k")) // row 1 → row 0 (normal move, no focus change)
	m = m2.(Model)
	if m.focus != focusList {
		t.Fatal("k above the first session must stay in the list (moved to the header)")
	}
	m2, _ = m.Update(key("k")) // at row 0: enter the bar
	m = m2.(Model)
	if m.focus != focusFilter || !m.filterInput.Focused() {
		t.Fatal("k at the top row must focus the search bar")
	}
	if m.searchAll {
		t.Error("arrow entry must keep the current (title) layer")
	}
	// ↓ returns to the list keeping the text
	m = typeInto(t, m, "abc")
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m2.(Model)
	if m.focus != focusList || m.filterInput.Focused() {
		t.Fatal("down in the bar must return focus to the list")
	}
	if m.filterInput.Value() != "abc" {
		t.Error("down must keep the query, like enter")
	}
}

func TestClickHelpSearchFocusesFullText(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(click(50, 29)) // inside "s search" [49,56]
	m = m2.(Model)
	if m.focus != focusFilter || !m.filterInput.Focused() {
		t.Fatal("clicking 's search' must focus the search bar")
	}
	if !m.searchAll || m.filterInput.Placeholder != "search…" {
		t.Error("clicking 's search' must land on the full-text layer")
	}
}
