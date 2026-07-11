package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// codexProvider serves OpenAI Codex sessions under sessionsDir
// (~/.codex/sessions), stored as rollout-<ts>-<uuid>.jsonl files nested
// by date.
type codexProvider struct{ sessionsDir string }

// NewCodexProvider builds the Codex provider for sessionsDir.
func NewCodexProvider(sessionsDir string) Provider {
	return codexProvider{sessionsDir: sessionsDir}
}

func (codexProvider) Agent() Agent   { return AgentCodex }
func (codexProvider) Binary() string { return "codex" }

func (p codexProvider) Available() bool {
	info, err := os.Stat(p.sessionsDir)
	return err == nil && info.IsDir()
}

// Scan walks sessionsDir for rollout-*.jsonl files (skipping any .trash),
// building entries from the filename + mtime only (no file contents).
func (p codexProvider) Scan() ([]Session, error) {
	var sessions []Session
	err := filepath.WalkDir(p.sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != p.sessionsDir {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		s := Session{
			ID:    codexID(name),
			Path:  path,
			Agent: AgentCodex,
		}
		if info, err := d.Info(); err == nil {
			s.LastActivity = info.ModTime()
		}
		sessions = append(sessions, s)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

// codexID extracts the trailing UUID from a rollout filename like
// rollout-2026-06-26T03-52-34-<uuid>.jsonl. Falls back to the whole stem.
func codexID(filename string) string {
	stem := strings.TrimSuffix(filename, ".jsonl")
	stem = strings.TrimPrefix(stem, "rollout-")
	// The UUID is the last 5 dash-separated groups (8-4-4-4-12).
	parts := strings.Split(stem, "-")
	if len(parts) >= 5 {
		return strings.Join(parts[len(parts)-5:], "-")
	}
	return stem
}

type codexRecord struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

type codexMeta struct {
	CWD       string `json:"cwd"`
	Timestamp string `json:"timestamp"`
}

type codexPayload struct {
	Type      string         `json:"type"`
	Role      string         `json:"role"`
	Content   []codexContent `json:"content"`
	Name      string         `json:"name"`      // function_call
	Arguments string         `json:"arguments"` // function_call (JSON string)
}

type codexContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (p codexProvider) ParseMetadata(path string) (Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	var m Meta
	sc := newScanner(f)
	for sc.Scan() {
		var rec codexRecord
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		switch rec.Type {
		case "session_meta":
			var sm codexMeta
			if json.Unmarshal(rec.Payload, &sm) == nil {
				if sm.CWD != "" {
					m.CWD = sm.CWD
				}
				if t, err := time.Parse(time.RFC3339, sm.Timestamp); err == nil {
					m.LastActivity = t
				}
			}
		case "response_item":
			var pl codexPayload
			if json.Unmarshal(rec.Payload, &pl) != nil || pl.Type != "message" {
				continue
			}
			text := codexText(pl)
			switch pl.Role {
			case "assistant":
				if text != "" {
					m.TotalMessages++
				}
			case "user":
				if p := codexRealPrompt(text); p != "" {
					m.TotalMessages++
					m.UserMessages++
					if m.FirstPrompt == "" {
						m.FirstPrompt = p
					}
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

// codexText returns the first non-empty text block of a message payload.
func codexText(pl codexPayload) string {
	for _, c := range pl.Content {
		if strings.TrimSpace(c.Text) != "" {
			return c.Text
		}
	}
	return ""
}

// codexRealPrompt returns text iff it is a human prompt: not harness-injected
// context (which is wrapped in <...>).
func codexRealPrompt(text string) string {
	t := strings.TrimSpace(text)
	if t == "" || strings.HasPrefix(t, "<") {
		return ""
	}
	return t
}

// --- stubs completed in Task 4 ---
func (codexProvider) ParseTranscript(path string) (Transcript, error) { return Transcript{}, nil }
func (codexProvider) Trash(s Session) (string, error)                 { return "", nil }
func (codexProvider) ResumeCommand(s Session) (string, []string) {
	return "codex", []string{"resume", s.ID}
}
func (codexProvider) NewCommand() (string, []string) { return "codex", nil }
