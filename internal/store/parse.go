package store

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// Meta is the lightweight per-session metadata extracted by one
// streaming pass over a session .jsonl file.
type Meta struct {
	Title         string
	FirstPrompt   string
	CWD           string
	GitBranch     string
	LastActivity  time.Time
	UserMessages  int
	TotalMessages int
}

type rawRecord struct {
	Type      string          `json:"type"`
	AiTitle   string          `json:"aiTitle"`
	CWD       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Timestamp string          `json:"timestamp"`
	IsMeta    bool            `json:"isMeta"`
	Message   json.RawMessage `json:"message"`
}

type apiMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func newScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	// Single records can be megabytes (pasted files, tool results).
	sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	return sc
}

func ParseMetadata(path string) (Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	var m Meta
	sc := newScanner(f)
	for sc.Scan() {
		var rec rawRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue // malformed lines are never fatal
		}
		switch rec.Type {
		case "ai-title":
			if rec.AiTitle != "" {
				m.Title = rec.AiTitle // last one wins
			}
		case "user", "assistant":
			// First cwd wins: Claude Code files a session under the directory
			// it was started in and resolves `--resume` against the current
			// directory's project, so a session that later cd'd into a
			// subdirectory must still be resumed from its origin.
			if m.CWD == "" && rec.CWD != "" {
				m.CWD = rec.CWD
			}
			if rec.GitBranch != "" {
				m.GitBranch = rec.GitBranch
			}
			if t, err := time.Parse(time.RFC3339, rec.Timestamp); err == nil && t.After(m.LastActivity) {
				m.LastActivity = t
			}
			if rec.Type == "assistant" {
				m.TotalMessages++
			} else if p := realPrompt(rec); p != "" {
				m.TotalMessages++
				m.UserMessages++
				if m.FirstPrompt == "" {
					m.FirstPrompt = p
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Meta{}, err
	}
	if m.Title == "" {
		m.Title = Truncate(m.FirstPrompt, 60)
	}
	return m, nil
}

// realPrompt returns the text of a user record iff it is a prompt the
// human actually typed: not a meta record, not a tool_result, and not
// harness-injected markup (which always starts with "<").
func realPrompt(rec rawRecord) string {
	if rec.IsMeta {
		return ""
	}
	text := strings.TrimSpace(firstText(rec.Message))
	if text == "" || strings.HasPrefix(text, "<") {
		return ""
	}
	return text
}

func firstText(raw json.RawMessage) string {
	var msg apiMessage
	if json.Unmarshal(raw, &msg) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(msg.Content, &s) == nil {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(msg.Content, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				return b.Text
			}
		}
	}
	return ""
}
