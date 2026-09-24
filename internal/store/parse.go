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
// human actually typed: not a meta record or a tool_result. Only known
// harness wrappers are removed; HTML/XML is otherwise ordinary user text.
func realPrompt(rec rawRecord) string {
	if rec.IsMeta {
		return ""
	}
	return claudeUserText(rec.Message)
}

func stripClaudeContext(text string) string {
	return stripContextBlocks(text, []string{
		"system-reminder", "local-command-caveat", "local-command-stdout",
		"command-name", "command-message", "command-args",
	})
}

// claudeHumanText also handles background-task notifications, which Claude
// stores as user messages without isMeta. Their generated output-file hint is
// outside the XML envelope and can occupy a separate text block. Keep actual
// trailing human text, and never classify arbitrary/incomplete XML as a task.
func claudeHumanText(text, taskOutput string) (string, string) {
	text, taskOutput = stripTaskOutputHint(stripClaudeContext(text), taskOutput)
	const open, close = "<task-notification>", "</task-notification>"
	for strings.HasPrefix(text, open) {
		end := strings.Index(text[len(open):], close)
		if end < 0 {
			break
		}
		body := text[len(open) : len(open)+end]
		if taskField(body, "task-id") == "" || taskField(body, "status") == "" {
			break
		}
		taskOutput = taskField(body, "output-file")
		text = strings.TrimSpace(text[len(open)+end+len(close):])
		text, taskOutput = stripTaskOutputHint(text, taskOutput)
		text = stripClaudeContext(text)
	}
	if text != "" {
		// A later human block that happens to mention this path is unrelated.
		taskOutput = ""
	}
	return text, taskOutput
}

// Notification summaries can contain literal shell/XML punctuation, so this
// recognizes the small harness envelope rather than requiring valid XML.
func taskField(body, tag string) string {
	_, rest, ok := strings.Cut(body, "<"+tag+">")
	if !ok {
		return ""
	}
	value, _, ok := strings.Cut(rest, "</"+tag+">")
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func stripTaskOutputHint(text, output string) (string, string) {
	text = strings.TrimSpace(text)
	if output == "" {
		return text, ""
	}
	line, rest, _ := strings.Cut(text, "\n")
	if strings.TrimSpace(line) == "Read the output file to retrieve the result: "+output {
		return strings.TrimSpace(rest), ""
	}
	return text, output
}

// stripContextBlocks removes complete, recognized leading context blocks,
// retaining any human text that follows. Incomplete or unknown markup stays
// visible instead of silently turning a real conversation into an empty one.
func stripContextBlocks(text string, tags []string) string {
	text = strings.TrimSpace(text)
	for {
		stripped := false
		for _, tag := range tags {
			open, close := "<"+tag+">", "</"+tag+">"
			if !strings.HasPrefix(text, open) {
				continue
			}
			if end := strings.Index(text[len(open):], close); end >= 0 {
				text = strings.TrimSpace(text[len(open)+end+len(close):])
				stripped = true
			}
			break
		}
		if !stripped {
			return text
		}
	}
}

func claudeUserText(raw json.RawMessage) string {
	var msg apiMessage
	if json.Unmarshal(raw, &msg) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(msg.Content, &s) == nil {
		text, _ := claudeHumanText(s, "")
		return text
	}
	var blocks []contentBlock
	if json.Unmarshal(msg.Content, &blocks) == nil {
		var texts []string
		var taskOutput string
		for _, b := range blocks {
			if b.Type == "text" {
				var text string
				text, taskOutput = claudeHumanText(b.Text, taskOutput)
				if text != "" {
					texts = append(texts, text)
				}
			} else {
				taskOutput = ""
			}
		}
		return strings.Join(texts, "\n\n")
	}
	return ""
}
