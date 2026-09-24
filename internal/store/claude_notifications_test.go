package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Claude emits these user-role records without isMeta. Keep the automatic
// instruction outside the XML: dropping only the wrapper still leaks it.
const claudeTaskNotification = `<task-notification>
<task-id>task-123</task-id>
<tool-use-id>toolu_test</tool-use-id>
<output-file>/tmp/claude-test/tasks/task-123.output</output-file>
<status>completed</status>
<summary>Background command "Compile background assets" completed (exit code 0)</summary>
</task-notification>`

const claudeTaskTrailer = "Read the output file to retrieve the result: /tmp/claude-test/tasks/task-123.output"

func writeClaudeUserRecords(t *testing.T, contents ...any) string {
	t.Helper()
	var lines []string
	for _, content := range contents {
		data, err := json.Marshal(map[string]any{
			"type": "user",
			"message": map[string]any{
				"role": "user", "content": content,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(data))
	}
	path := filepath.Join(t.TempDir(), "notifications.jsonl")
	writeFile(t, path, strings.Join(lines, "\n")+"\n")
	return path
}

func claudeTextBlocks(texts ...string) []map[string]string {
	var blocks []map[string]string
	for _, text := range texts {
		blocks = append(blocks, map[string]string{"type": "text", "text": text})
	}
	return blocks
}

func assertClaudeUserText(t *testing.T, path string, want []string) Session {
	t.Helper()
	meta, err := ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.UserMessages != len(want) || meta.TotalMessages != len(want) {
		t.Errorf("message counts = %d user, %d total; want %d each", meta.UserMessages, meta.TotalMessages, len(want))
	}
	first := ""
	if len(want) != 0 {
		first = want[0]
	}
	if meta.FirstPrompt != first {
		t.Errorf("FirstPrompt = %q, want %q", meta.FirstPrompt, first)
	}
	if len(want) == 0 && meta.Title != "" {
		t.Errorf("notification-only title = %q, want empty", meta.Title)
	}
	session := Session{Path: path, Agent: AgentClaude}
	session.Apply(meta)
	if session.Empty() != (len(want) == 0) {
		t.Errorf("Empty = %v, want %v", session.Empty(), len(want) == 0)
	}
	transcript, err := ParseTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(transcript.Messages) != len(want) {
		t.Errorf("transcript = %+v, want %d human messages", transcript.Messages, len(want))
	} else {
		for i, text := range want {
			if transcript.Messages[i].Kind != KindUser || transcript.Messages[i].Text != text {
				t.Errorf("message %d = %+v, want human text %q", i, transcript.Messages[i], text)
			}
		}
	}
	return session
}

func TestClaudeTaskNotificationsAreNotHumanMessages(t *testing.T) {
	const human = "Please update the application"
	for _, format := range []string{"string", "text-block", "split-text-block"} {
		for _, trailer := range []bool{false, true} {
			name := format + "/notification"
			notification := claudeTaskNotification
			if trailer {
				name += "-with-trailer"
				notification += "\n" + claudeTaskTrailer
			}
			t.Run(name, func(t *testing.T) {
				var humanContent, notificationContent any = human, notification
				if format == "text-block" {
					humanContent = claudeTextBlocks(human)
					notificationContent = claudeTextBlocks(notification)
				}
				if format == "split-text-block" {
					humanContent = claudeTextBlocks(human)
					blocks := claudeTextBlocks(claudeTaskNotification)
					if trailer {
						blocks = append(blocks, claudeTextBlocks(claudeTaskTrailer)...)
					}
					notificationContent = blocks
				}
				for _, empty := range []bool{false, true} {
					name := "after-human"
					contents := []any{humanContent, notificationContent}
					want := []string{human}
					if empty {
						name = "notification-only"
						contents = []any{notificationContent}
						want = nil
					}
					t.Run(name, func(t *testing.T) {
						path := writeClaudeUserRecords(t, contents...)
						session := assertClaudeUserText(t, path, want)
						index := testIndex(t)
						if err := index.EnsureSession(path, func() (Transcript, error) { return ParseTranscript(path) }); err != nil {
							t.Fatal(err)
						}
						for _, query := range []string{"assets", "task-123", "retrieve"} {
							if hits, indexed := index.Search(query, []Session{session}); len(hits) != 0 || indexed != 1 {
								t.Errorf("synthetic text matched %q: hits=%v indexed=%d", query, hits, indexed)
							}
						}
						if hits, indexed := index.Search("application", []Session{session}); len(hits) != len(want) || indexed != 1 {
							t.Errorf("human search: hits=%v indexed=%d, want %d hits", hits, indexed, len(want))
						}
					})
				}
			})
		}
	}
}

func TestClaudeTaskNotificationPreservesAdjacentHumanText(t *testing.T) {
	const human = "<request>Fix the actual parser issue</request>"
	notification := claudeTaskNotification + "\n" + claudeTaskTrailer
	for _, tc := range []struct {
		name    string
		content any
		want    string
	}{
		{"notification-before-human-block", claudeTextBlocks(notification, human), human},
		{"notification-after-human-block", claudeTextBlocks(human, notification), human},
		{"human-after-notification", claudeTaskNotification + "\n" + human, human},
		{"human-after-automatic-trailer", notification + "\n" + human, human},
		{"split-notification-and-trailer", claudeTextBlocks(claudeTaskNotification, claudeTaskTrailer, human), human},
		{"human-clears-trailer-context", claudeTextBlocks(claudeTaskNotification, human, claudeTaskTrailer), human + "\n\n" + claudeTaskTrailer},
		{"consumed-trailer-clears-context", claudeTextBlocks(notification, claudeTaskTrailer), claudeTaskTrailer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeClaudeUserRecords(t, tc.content)
			assertClaudeUserText(t, path, []string{tc.want})
		})
	}
}

func TestClaudeTaskNotificationRebuildsVersionTwoSearchCache(t *testing.T) {
	const human = "Please update the application"
	notification := claudeTaskNotification + "\n" + claudeTaskTrailer
	path := writeClaudeUserRecords(t, human, notification)
	index := testIndex(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Version 2 cached the synthetic notification as a human message, and
	// source mtime/size do not change when upgrading the extraction rules.
	legacy := fmt.Sprintf("2\t%s\t%d\t%d\n%s\n\x1e\n%s", path, info.ModTime().UnixNano(), info.Size(), human, notification)
	if err := os.WriteFile(index.cacheFile(path), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := false
	if err := index.EnsureSession(path, func() (Transcript, error) {
		parsed = true
		return ParseTranscript(path)
	}); err != nil {
		t.Fatal(err)
	}
	if !parsed {
		t.Error("version 2 cache was reused without re-extracting the session")
	}
	sessions := []Session{{Path: path, Agent: AgentClaude}}
	for _, query := range []string{"assets", "retrieve"} {
		if hits, indexed := index.Search(query, sessions); len(hits) != 0 || indexed != 1 {
			t.Errorf("legacy notification still searchable for %q: hits=%v indexed=%d", query, hits, indexed)
		}
	}
	if hits, indexed := index.Search("application", sessions); len(hits) != 1 || indexed != 1 {
		t.Errorf("human prompt missing after cache rebuild: hits=%v indexed=%d", hits, indexed)
	}
}

func TestClaudeTaskNotificationLookalikesRemainHumanText(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{"html", "<div class='broken'>Hello</div>\nPlease fix this component"},
		{"xml", "<request>Fix this XML parser</request>"},
		{"ordinary-task-notification-xml", "<task-notification>Design this XML element</task-notification>"},
		{"incomplete-notification", strings.TrimSuffix(claudeTaskNotification, "\n</task-notification>")},
		{"missing-task-id", "<task-notification><status>completed</status></task-notification>"},
		{"missing-status", "<task-notification><task-id>example</task-id></task-notification>"},
		{"fenced-example", "```xml\n" + claudeTaskNotification + "\n```\nExplain this format"},
		{"quoted-example", "> " + strings.ReplaceAll(claudeTaskNotification, "\n", "\n> ")},
		{"prose-before-example", "Explain this notification:\n" + claudeTaskNotification},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, content := range []any{tc.text, claudeTextBlocks(tc.text)} {
				path := writeClaudeUserRecords(t, content)
				assertClaudeUserText(t, path, []string{tc.text})
			}
		})
	}
}
