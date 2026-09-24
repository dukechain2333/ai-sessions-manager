package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"testing"
)

func drainSearchCommands(t *testing.T, m Model, first tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{first}
	for processed := 0; len(queue) > 0; processed++ {
		if processed > 100 {
			t.Fatal("nonterminating command chain")
		}
		cmd := queue[0]
		queue = queue[1:]
		if cmd == nil {
			continue
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		next, cmd := m.Update(msg)
		m = next.(Model)
		if cmd != nil {
			queue = append(queue, cmd)
		}
	}
	return m
}

func TestSearchRevalidatesEachNewQuery(t *testing.T) {
	m := searchModel(t)
	m.list.SetSessions(m.list.sessions[:2])
	m.searchAll = true
	m.indexReady = false
	m.filterInput.SetValue("quick")
	m = drainSearchCommands(t, m, m.dispatchSearch())
	if m.matched != 2 || !m.indexReady || m.indexing {
		t.Fatalf("first completed query: matched=%d ready=%t indexing=%t", m.matched, m.indexReady, m.indexing)
	}
	path := m.list.sessions[0].Path
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"type":"user","message":{"role":"user","content":"just appended externally"}}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"quick brown", "fox"} {
		m.filterInput.SetValue(query)
		m = drainSearchCommands(t, m, m.dispatchSearch())
		t.Logf("new query=%q matched=%d ready=%t indexing=%t failed=%d", query, m.matched, m.indexReady, m.indexing, m.indexFailed)
		if m.matched != 1 {
			t.Errorf("query %q must still find the pre-existing matching session after append; matched=%d", query, m.matched)
		}
	}
}
