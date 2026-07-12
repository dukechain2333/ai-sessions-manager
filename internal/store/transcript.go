package store

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

type MsgKind int

const (
	KindUser MsgKind = iota
	KindAssistant
	KindTool
)

type Message struct {
	Kind MsgKind
	Text string
}

type Transcript struct {
	SessionID string
	Messages  []Message
}

// ParseTranscript extracts the human-readable conversation: real user
// prompts, assistant text, and tool calls collapsed to one-liners.
func ParseTranscript(path string) (Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return Transcript{}, err
	}
	defer f.Close()
	var tr Transcript
	sc := newScanner(f)
	for sc.Scan() {
		var rec rawRecord
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		switch rec.Type {
		case "user":
			if p := realPrompt(rec); p != "" {
				tr.Messages = append(tr.Messages, Message{KindUser, p})
			}
		case "assistant":
			var msg apiMessage
			if json.Unmarshal(rec.Message, &msg) != nil {
				continue
			}
			var blocks []contentBlock
			if json.Unmarshal(msg.Content, &blocks) != nil {
				continue
			}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					if s := strings.TrimSpace(b.Text); s != "" {
						tr.Messages = append(tr.Messages, Message{KindAssistant, s})
					}
				case "tool_use":
					tr.Messages = append(tr.Messages, Message{KindTool, summarizeTool(b)})
				}
			}
		}
	}
	return tr, sc.Err()
}

func summarizeTool(b contentBlock) string {
	var input map[string]any
	json.Unmarshal(b.Input, &input)
	for _, k := range []string{"description", "command", "file_path", "pattern", "prompt", "query", "skill", "subject"} {
		if v, ok := input[k].(string); ok && v != "" {
			return b.Name + ": " + Truncate(v, 80)
		}
	}
	return b.Name
}

// TranscriptCache is a small LRU keyed by path+mtime, so an updated
// session file is re-parsed while repeated previews stay instant.
type TranscriptCache struct {
	mu       sync.Mutex
	capacity int
	order    []string
	entries  map[string]Transcript
}

func NewTranscriptCache(capacity int) *TranscriptCache {
	return &TranscriptCache{capacity: capacity, entries: map[string]Transcript{}}
}

func (c *TranscriptCache) Get(path string, parse func() (Transcript, error)) (Transcript, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, err := os.Stat(path)
	if err != nil {
		return Transcript{}, err
	}
	key := fmt.Sprintf("%s|%d", path, st.ModTime().UnixNano())
	if t, ok := c.entries[key]; ok {
		c.touch(key)
		return t, nil
	}
	t, err := parse()
	if err != nil {
		return Transcript{}, err
	}
	c.entries[key] = t
	c.order = append(c.order, key)
	if len(c.order) > c.capacity {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
	return t, nil
}

func (c *TranscriptCache) touch(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(append(c.order[:i:i], c.order[i+1:]...), key)
			return
		}
	}
}
