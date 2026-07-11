package ui

import (
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
	m2, _ = m.Update(res)
	m = m2.(Model)
	if m.matched != 2 {
		t.Fatalf("matched = %d, want 2", m.matched)
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

func TestRunSearchSnapshotsSessions(t *testing.T) {
	m := searchModel(t)
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
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

func TestDeleteInvalidatesInFlightSearch(t *testing.T) {
	m := searchModel(t)
	m.trashFn = func(string, store.Session) (string, error) { return "/trash/x", nil }
	m2, _ := m.Update(key("/"))
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)
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
