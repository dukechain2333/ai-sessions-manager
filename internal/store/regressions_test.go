package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func promptFixture(t *testing.T, agent Agent, texts ...string) (Provider, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.jsonl")
	var record any
	var provider Provider
	if agent == AgentClaude {
		var blocks []map[string]string
		for _, text := range texts {
			blocks = append(blocks, map[string]string{"type": "text", "text": text})
		}
		record = map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": blocks}}
		provider = NewClaudeProvider(dir)
	} else {
		var blocks []codexContent
		for _, text := range texts {
			blocks = append(blocks, codexContent{Type: "input_text", Text: text})
		}
		record = map[string]any{"type": "response_item", "payload": codexPayload{Type: "message", Role: "user", Content: blocks}}
		provider = NewCodexProvider(dir)
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(data)+"\n")
	return provider, path
}

func TestMarkupPromptsRemainVisibleAndSearchable(t *testing.T) {
	for _, agent := range []Agent{AgentClaude, AgentCodex} {
		for _, prompt := range []string{
			"<div class='broken'>Hello</div>\nPlease fix this component",
			"<request>fix this XML parser</request>",
			"< 5 is the limit; fix the comparison",
		} {
			t.Run(string(agent)+"/"+prompt[:4], func(t *testing.T) {
				p, path := promptFixture(t, agent, prompt)
				meta, err := p.ParseMetadata(path)
				if err != nil {
					t.Fatal(err)
				}
				s := Session{Path: path, Agent: agent}
				s.Apply(meta)
				if s.Empty() || meta.UserMessages != 1 || meta.FirstPrompt != prompt {
					t.Fatalf("real prompt was dropped: %+v", meta)
				}
				tr, err := p.ParseTranscript(path)
				if err != nil || len(tr.Messages) != 1 || tr.Messages[0].Text != prompt {
					t.Fatalf("transcript = %+v, err = %v", tr, err)
				}
				ix := testIndex(t)
				if err := ix.EnsureSession(path, func() (Transcript, error) { return p.ParseTranscript(path) }); err != nil {
					t.Fatal(err)
				}
				if hits, indexed := ix.Search("fix", []Session{s}); len(hits) != 1 || indexed != 1 {
					t.Fatalf("markup prompt not searchable: hits=%v indexed=%d", hits, indexed)
				}
			})
		}
	}
}

func TestRecognizedContextKeepsFollowingHumanPrompt(t *testing.T) {
	for _, tc := range []struct {
		agent   Agent
		context string
	}{
		{AgentClaude, "<system-reminder>injected context</system-reminder>"},
		{AgentCodex, "<environment_context>injected context</environment_context>"},
		{AgentCodex, "# AGENTS.md instructions for /tmp\n\n<INSTRUCTIONS>repo rules</INSTRUCTIONS>\n<environment_context>cwd</environment_context>"},
		{AgentCodex, "# AGENTS.md instructions\n\n<INSTRUCTIONS>multi-environment rules</INSTRUCTIONS>"},
	} {
		t.Run(string(tc.agent)+"/"+tc.context[:10], func(t *testing.T) {
			p, path := promptFixture(t, tc.agent, tc.context)
			meta, err := p.ParseMetadata(path)
			if err != nil || meta.UserMessages != 0 || meta.Title != "" {
				t.Fatalf("injected context counted as user text: %+v, %v", meta, err)
			}
			tr, err := p.ParseTranscript(path)
			if err != nil || len(tr.Messages) != 0 {
				t.Fatalf("injected transcript = %+v, %v", tr, err)
			}
			const prompt = "<request>fix the actual issue</request>"
			p, path = promptFixture(t, tc.agent, tc.context+"\n"+prompt)
			meta, err = p.ParseMetadata(path)
			if err != nil || meta.UserMessages != 1 || meta.FirstPrompt != prompt {
				t.Fatalf("context swallowed following human text: %+v, %v", meta, err)
			}
		})
	}
}

func TestCodexContextInSeparateContentBlocks(t *testing.T) {
	const prompt = "Fix the parser race"
	p, path := promptFixture(t, AgentCodex,
		"# AGENTS.md instructions for /tmp\n\n<INSTRUCTIONS>repo conventions</INSTRUCTIONS>",
		"<environment_context>cwd</environment_context>", prompt)
	meta, err := p.ParseMetadata(path)
	if err != nil || meta.UserMessages != 1 || meta.Title != prompt {
		t.Fatalf("metadata = %+v, err = %v", meta, err)
	}
	tr, err := p.ParseTranscript(path)
	if err != nil || len(tr.Messages) != 1 || tr.Messages[0].Text != prompt {
		t.Fatalf("transcript = %+v, err = %v", tr, err)
	}
	for _, text := range []string{
		"# AGENTS.md instructions for /tmp\nPlease write this file",
		"# AGENTS.md instructions for /tmp\n<INSTRUCTIONS>unfinished user text",
	} {
		if got := codexRealPrompt(text); got != text {
			t.Errorf("ordinary/incomplete user text changed: %q", got)
		}
	}
}

func TestClaudeContextInSeparateContentBlocks(t *testing.T) {
	const prompt = "<request>fix the parser</request>"
	p, path := promptFixture(t, AgentClaude,
		"<system-reminder>injected context</system-reminder>", prompt)
	meta, err := p.ParseMetadata(path)
	if err != nil || meta.UserMessages != 1 || meta.FirstPrompt != prompt {
		t.Fatalf("metadata = %+v, err = %v", meta, err)
	}
	tr, err := p.ParseTranscript(path)
	if err != nil || len(tr.Messages) != 1 || tr.Messages[0].Text != prompt {
		t.Fatalf("transcript = %+v, err = %v", tr, err)
	}
}

func TestCodexLastActivityUsesMaximumRecordTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeFile(t, path, `{"type":"session_meta","payload":{"cwd":"/tmp","timestamp":"2026-01-01T10:00:00Z"}}
{"timestamp":"2026-09-24T10:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"resumed today"}]}}
{"timestamp":"2026-09-24T10:01:00Z","type":"event_msg","payload":{"type":"task_complete"}}
{"timestamp":"2026-09-23T10:00:00Z","type":"session_meta","payload":{"timestamp":"2026-01-01T10:00:00Z"}}
{"timestamp":"invalid","type":"event_msg"}
`)
	meta, err := NewCodexProvider(filepath.Dir(path)).ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 24, 10, 1, 0, 0, time.UTC)
	if !meta.LastActivity.Equal(want) {
		t.Fatalf("LastActivity = %v, want %v", meta.LastActivity, want)
	}
}

func TestCodexScanFollowsOnlyExplicitRootSymlink(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, ".real-sessions")
	const name = "rollout-2026-09-24T10-00-00-019f020e-d6ab-7ff2-99b4-c3274454ea14.jsonl"
	writeFile(t, filepath.Join(actual, "2026", "09", "24", name), "{}\n")
	writeFile(t, filepath.Join(actual, ".trash", name), "{}\n")
	outside := filepath.Join(root, "outside")
	writeFile(t, filepath.Join(outside, name), "{}\n")
	if err := os.Symlink(outside, filepath.Join(actual, "nested-link")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "sessions")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	p := NewCodexProvider(link)
	ss, err := p.Scan()
	if err != nil || !p.Available() || len(ss) != 1 {
		t.Fatalf("Available=%v sessions=%+v err=%v", p.Available(), ss, err)
	}
	if _, err := NewCodexProvider(filepath.Join(root, "missing")).Scan(); err == nil {
		t.Error("missing root should produce an error")
	}
}

func TestTrashRetainsEveryGeneration(t *testing.T) {
	for _, agent := range []Agent{AgentClaude, AgentCodex} {
		t.Run(string(agent), func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "-project", "session.jsonl")
			p := NewClaudeProvider(dir)
			if agent == AgentCodex {
				p = NewCodexProvider(dir)
			}
			s := Session{Path: src, Slug: "-project", Agent: agent}
			seen := map[string]bool{}
			for _, content := range []string{"original history", "continuation one", "continuation two"} {
				writeFile(t, src, content)
				dest, err := p.Trash(s)
				if err != nil {
					t.Fatal(err)
				}
				if seen[dest] || filepath.Base(dest) != filepath.Base(src) {
					t.Fatalf("trash must preserve the filename in a unique location: %s", dest)
				}
				seen[dest] = true
				got, err := os.ReadFile(dest)
				if err != nil || string(got) != content {
					t.Fatalf("archive content = %q, err = %v", got, err)
				}
			}
			contents := map[string]bool{}
			for dest := range seen {
				data, err := os.ReadFile(dest)
				if err != nil {
					t.Fatal(err)
				}
				contents[string(data)] = true
			}
			if len(contents) != 3 {
				t.Fatalf("previous histories were overwritten: %v", contents)
			}
			if _, err := os.Stat(src); !os.IsNotExist(err) {
				t.Fatalf("source remains after successful trash: %v", err)
			}
		})
	}
}

func TestConcurrentTrashCollisionNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, ".trash", "same.jsonl")
	const count = 16
	type result struct {
		path string
		err  error
	}
	results := make(chan result, count)
	start := make(chan struct{})
	for i := 0; i < count; i++ {
		src := filepath.Join(root, fmt.Sprintf("source-%d.jsonl", i))
		writeFile(t, src, fmt.Sprintf("history-%d", i))
		go func() {
			<-start
			path, err := moveToTrash(src, dest)
			results <- result{path, err}
		}()
	}
	close(start)
	contents := map[string]bool{}
	for i := 0; i < count; i++ {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		data, err := os.ReadFile(r.path)
		if err != nil {
			t.Fatal(err)
		}
		contents[string(data)] = true
	}
	if len(contents) != count {
		t.Fatalf("concurrent deletes retained %d/%d histories", len(contents), count)
	}
}

func TestTrashCollisionPreservesDestinationSymlink(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "session.jsonl")
	target := filepath.Join(root, "existing-history.jsonl")
	dest := filepath.Join(root, ".trash", "session.jsonl")
	writeFile(t, source, "new history")
	writeFile(t, target, "existing history")
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, dest); err != nil {
		t.Fatal(err)
	}
	actual, err := moveToTrash(source, dest)
	if err != nil || actual == dest {
		t.Fatalf("collision destination = %q, err = %v", actual, err)
	}
	info, err := os.Lstat(dest)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("existing symlink replaced: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "existing history" {
		t.Fatalf("symlink target changed: %q, %v", data, err)
	}
}

func TestTrashFailureKeepsSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "session.jsonl")
	blocked := filepath.Join(root, "not-a-directory")
	writeFile(t, source, "keep this history")
	writeFile(t, blocked, "existing file")
	if _, err := moveToTrash(source, filepath.Join(blocked, "session.jsonl")); err == nil {
		t.Fatal("invalid trash directory should fail")
	}
	data, err := os.ReadFile(source)
	if err != nil || string(data) != "keep this history" {
		t.Fatalf("failed trash changed source: %q, %v", data, err)
	}
}

func TestEnrichResultsCarrySnapshotPathOnSuccessAndError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "good.jsonl")
	writeFile(t, path, `{"type":"user","cwd":"/tmp","message":{"content":"hello"}}`+"\n")
	sessions := []Session{
		{Path: path, Agent: AgentClaude},
		{Path: filepath.Join(dir, "missing.jsonl"), Agent: AgentClaude},
		{Path: filepath.Join(dir, "unknown.jsonl"), Agent: Agent("unknown")},
	}
	original := append([]Session(nil), sessions...)
	ch := make(chan EnrichResult, len(sessions))
	Enrich(sessions, []Provider{NewClaudeProvider(dir)}, 2, ch)
	for i := range sessions {
		sessions[i].Path = "changed after snapshot"
	}
	for r := range ch {
		if r.Path != original[r.Index].Path {
			t.Errorf("result identity = %q, want %q", r.Path, original[r.Index].Path)
		}
		if (r.Err != nil) != (r.Index != 0) {
			t.Errorf("index %d error = %v", r.Index, r.Err)
		}
	}
}

func TestResolveSlugPrunesLongNonexistentPrefixes(t *testing.T) {
	root := t.TempDir()
	leaf := strings.Repeat("part-", 36) + "leaf"
	want := filepath.Join(root, "worktrees", leaf)
	if err := os.MkdirAll(want, 0o700); err != nil {
		t.Fatal(err)
	}
	result := make(chan string, 1)
	go func() { result <- ResolveSlug(root, "-worktrees-"+leaf) }()
	select {
	case got := <-result:
		if got != want {
			t.Fatalf("ResolveSlug = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long slug resolution enumerated nonexistent partitions")
	}
}

func TestSlugResolutionCacheIsSharedAndLimitedToOnePass(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "project-name")
	if err := os.Mkdir(want, 0o700); err != nil {
		t.Fatal(err)
	}
	resolve := newSlugResolver(root)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := resolve("-project-name"); got != want {
				t.Errorf("cached resolution = %q", got)
			}
		}()
	}
	wg.Wait()
	if err := os.Remove(want); err != nil {
		t.Fatal(err)
	}
	if got := resolve("-project-name"); got != want {
		t.Errorf("one pass unexpectedly repeated filesystem resolution: %q", got)
	}
	if got := newSlugResolver(root)("-project-name"); got != "" {
		t.Errorf("new pass reused stale resolution: %q", got)
	}
}

func TestSearchIndexRebuildsLegacyParserCache(t *testing.T) {
	p, path := promptFixture(t, AgentCodex, "<request>fix the issue</request>")
	ix := testIndex(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := fmt.Sprintf("%s\t%d\t%d\n", path, info.ModTime().UnixNano(), info.Size())
	if err := os.WriteFile(ix.cacheFile(path), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ix.EnsureSession(path, func() (Transcript, error) { return p.ParseTranscript(path) }); err != nil {
		t.Fatal(err)
	}
	msgs, fresh := ix.Messages(path)
	if !fresh || len(msgs) != 1 || msgs[0] != "<request>fix the issue</request>" {
		t.Fatalf("legacy extraction was reused: messages=%v fresh=%v", msgs, fresh)
	}
}

func appendPrompt(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(f, "{\"type\":\"user\",\"message\":{\"content\":%q}}\n", text)
	closeErr := f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func TestSearchIndexRetriesAnAppendDuringParsing(t *testing.T) {
	p, path := promptFixture(t, AgentClaude, "first message")
	ix := testIndex(t)
	calls := 0
	err := ix.EnsureSession(path, func() (Transcript, error) {
		calls++
		if calls == 1 {
			appendPrompt(t, path, "appended while parsing")
		}
		return p.ParseTranscript(path)
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs, fresh := ix.Messages(path)
	if calls != 2 || !fresh || len(msgs) != 2 {
		t.Fatalf("calls=%d fresh=%v messages=%v", calls, fresh, msgs)
	}
}

func TestSearchIndexRepeatedChangesKeepPreviousCache(t *testing.T) {
	p, path := promptFixture(t, AgentClaude, "original history")
	ix := testIndex(t)
	if err := ix.EnsureSession(path, func() (Transcript, error) { return p.ParseTranscript(path) }); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(ix.cacheFile(path))
	if err != nil {
		t.Fatal(err)
	}
	appendPrompt(t, path, "invalidate original cache")
	calls := 0
	err = ix.EnsureSession(path, func() (Transcript, error) {
		calls++
		appendPrompt(t, path, "concurrent append")
		return p.ParseTranscript(path)
	})
	if err == nil || !strings.Contains(err.Error(), "changed while indexing") || calls != 3 {
		t.Fatalf("calls=%d error=%v; want bounded retries and explicit failure", calls, err)
	}
	after, readErr := os.ReadFile(ix.cacheFile(path))
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed extraction replaced previous cache: %v", readErr)
	}
	files, err := os.ReadDir(ix.Dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("failed extraction left temporary files: %v, %v", files, err)
	}
}
