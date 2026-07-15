package tmux

import (
	"reflect"
	"testing"
)

func TestShort(t *testing.T) {
	if got := Short("ABCD1234-9c8f-43a7"); got != "abcd1234" {
		t.Errorf("Short = %q, want abcd1234", got)
	}
	if got := Short("s1"); got != "s1" {
		t.Errorf("Short short id = %q, want s1", got)
	}
}

func TestName(t *testing.T) {
	if got := Name("claude", "abcd1234"); got != "sm-claude-abcd1234" {
		t.Errorf("Name = %q", got)
	}
}

func TestPending(t *testing.T) {
	n := PendingName("codex", 42)
	if n != "sm-codex-pending-42" {
		t.Errorf("PendingName = %q", n)
	}
	if !IsPending(n) {
		t.Error("IsPending should be true for a pending name")
	}
	if IsPending("sm-codex-abcd1234") {
		t.Error("IsPending should be false for a normal name")
	}
	if got := PendingAgent(n); got != "codex" {
		t.Errorf("PendingAgent = %q, want codex", got)
	}
}

func TestPendingNonce(t *testing.T) {
	got, ok := PendingNonce(PendingName("codex", 42))
	if !ok || got != 42 {
		t.Errorf("PendingNonce = %d, %v; want 42, true", got, ok)
	}
	if _, ok := PendingNonce("sm-codex-abcd1234"); ok {
		t.Error("PendingNonce should not parse a non-pending name")
	}
	if _, ok := PendingNonce("sm-codex-pending-nope"); ok {
		t.Error("PendingNonce should not parse a non-numeric nonce")
	}
}

func TestResumeArgs(t *testing.T) {
	got := ResumeArgs("sm-claude-s1", "/x/alpha", "claude", []string{"--resume", "s1"})
	want := []string{"new-session", "-A", "-s", "sm-claude-s1", "-c", "/x/alpha", "claude", "--resume", "s1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResumeArgs = %v", got)
	}
}

func TestNewArgs(t *testing.T) {
	got := NewArgs("sm-codex-pending-42", "/x/beta", "codex", nil)
	want := []string{"new-session", "-s", "sm-codex-pending-42", "-c", "/x/beta", "codex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewArgs = %v", got)
	}
}

func TestWindowArgs(t *testing.T) {
	got := WindowArgs("sm-claude-s1", "/x/alpha", "claude", []string{"--resume", "s1"})
	want := []string{"new-window", "-c", "/x/alpha", "-n", "sm-claude-s1", "claude", "--resume", "s1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WindowArgs named = %v", got)
	}
	got = WindowArgs("", "/x/beta", "codex", nil)
	want = []string{"new-window", "-c", "/x/beta", "codex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WindowArgs unnamed = %v", got)
	}
}

func TestParseList(t *testing.T) {
	out := "sm-claude-s1\nother-session\nsm-codex-pending-9\n\n"
	got := parseList(out)
	if !got["sm-claude-s1"] || !got["sm-codex-pending-9"] {
		t.Errorf("parseList missing sm- names: %v", got)
	}
	if got["other-session"] {
		t.Error("parseList should drop non-sm names")
	}
	if len(got) != 2 {
		t.Errorf("parseList size = %d, want 2", len(got))
	}
}
