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
	// WalkDir deliberately does not follow symlinks. Resolve the explicitly
	// configured root, while still leaving nested links untraversed.
	root, err := filepath.EvalSymlinks(p.sessionsDir)
	if err != nil {
		return nil, err
	}
	var sessions []Session
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != root {
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
		if t, err := time.Parse(time.RFC3339, rec.Timestamp); err == nil && t.After(m.LastActivity) {
			m.LastActivity = t
		}
		switch rec.Type {
		case "session_meta":
			var sm codexMeta
			if json.Unmarshal(rec.Payload, &sm) == nil {
				if sm.CWD != "" {
					m.CWD = sm.CWD
				}
				if t, err := time.Parse(time.RFC3339, sm.Timestamp); err == nil && t.After(m.LastActivity) {
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
				if p := codexUserText(pl); p != "" {
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

// codexUserText handles context and human text in separate content blocks.
func codexUserText(pl codexPayload) string {
	var parts []string
	for _, c := range pl.Content {
		if text := codexRealPrompt(c.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// codexRealPrompt removes known context wrappers, including AGENTS.md's
// Markdown heading, without rejecting arbitrary HTML/XML user prompts.
func codexRealPrompt(text string) string {
	t := strings.TrimSpace(text)
	for {
		before := t
		t = stripContextBlocks(t, []string{
			"environment_context", "turn_aborted", "subagent_notification",
			"recommended_plugins", "user_shell_command",
		})
		heading, body, ok := strings.Cut(t, "\n")
		if ok && (strings.TrimSpace(heading) == "# AGENTS.md instructions" || strings.HasPrefix(heading, "# AGENTS.md instructions for ")) {
			body = strings.TrimSpace(body)
			if strings.HasPrefix(body, "<INSTRUCTIONS>") && strings.Contains(body, "</INSTRUCTIONS>") {
				t = stripContextBlocks(body, []string{"INSTRUCTIONS"})
			}
		}
		if t == before {
			return t
		}
	}
}

// ParseTranscript extracts the human-readable conversation: real user
// prompts, assistant text, and tool (function_call) one-liners. Developer
// messages, reasoning, and recognized context wrappers are excluded.
func (codexProvider) ParseTranscript(path string) (Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return Transcript{}, err
	}
	defer f.Close()
	var tr Transcript
	sc := newScanner(f)
	for sc.Scan() {
		var rec codexRecord
		if json.Unmarshal(sc.Bytes(), &rec) != nil || rec.Type != "response_item" {
			continue
		}
		var pl codexPayload
		if json.Unmarshal(rec.Payload, &pl) != nil {
			continue
		}
		switch pl.Type {
		case "message":
			text := strings.TrimSpace(codexText(pl))
			switch pl.Role {
			case "user":
				if p := codexUserText(pl); p != "" {
					tr.Messages = append(tr.Messages, Message{KindUser, p})
				}
			case "assistant":
				if text != "" {
					tr.Messages = append(tr.Messages, Message{KindAssistant, text})
				}
			}
		case "function_call":
			tr.Messages = append(tr.Messages, Message{KindTool, codexTool(pl)})
		}
	}
	return tr, sc.Err()
}

// codexTool renders a function_call as "name: <first arg or cmd>".
func codexTool(pl codexPayload) string {
	var args map[string]any
	json.Unmarshal([]byte(pl.Arguments), &args)
	for _, k := range []string{"cmd", "command", "description", "path", "query", "input"} {
		if v, ok := args[k].(string); ok && v != "" {
			return pl.Name + ": " + Truncate(v, 80)
		}
	}
	return pl.Name
}

// Trash moves a rollout file into <sessionsDir>/.trash/ (never rm).
func (p codexProvider) Trash(s Session) (string, error) {
	dest := filepath.Join(p.sessionsDir, ".trash", filepath.Base(s.Path))
	return moveToTrash(s.Path, dest)
}
func (codexProvider) ResumeCommand(s Session) (string, []string) {
	return "codex", []string{"resume", s.ID}
}
func (codexProvider) NewCommand() (string, []string) { return "codex", nil }
