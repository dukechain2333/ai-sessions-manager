// Package store reads Claude Code session history from ~/.claude/projects.
package store

import (
	"path/filepath"
	"strings"
	"time"
)

type Session struct {
	ID            string // session uuid (filename without .jsonl)
	Path          string // absolute path to the .jsonl file
	Slug          string // directory name under the projects dir
	CWD           string // working directory recorded in the session
	Title         string
	FirstPrompt   string
	GitBranch     string
	LastActivity  time.Time
	UserMessages  int
	TotalMessages int
	Enriched      bool
	Unreadable    bool
}

// Project is the short label shown next to a session.
func (s Session) Project() string {
	if s.CWD != "" {
		return filepath.Base(s.CWD)
	}
	return s.Slug
}

// Empty reports whether the session contains no real user prompts
// (hook/meta-only files). Only meaningful once enriched.
func (s Session) Empty() bool {
	return s.Enriched && s.UserMessages == 0
}

func (s *Session) Apply(m Meta) {
	s.Title = m.Title
	s.FirstPrompt = m.FirstPrompt
	s.GitBranch = m.GitBranch
	if m.CWD != "" {
		s.CWD = m.CWD
	}
	if !m.LastActivity.IsZero() {
		s.LastActivity = m.LastActivity
	}
	s.UserMessages = m.UserMessages
	s.TotalMessages = m.TotalMessages
	s.Enriched = true
}

// Truncate collapses whitespace and cuts s to at most n runes,
// ending with an ellipsis when cut.
func Truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
