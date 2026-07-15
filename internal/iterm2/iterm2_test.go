package iterm2

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func decode(t *testing.T, seq string) Launch {
	t.Helper()
	const pre = "\x1b]1337;Custom=id=sm:"
	if !strings.HasPrefix(seq, pre) || !strings.HasSuffix(seq, "\a") {
		t.Fatalf("sequence framing wrong: %q", seq)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(seq, pre), "\a"))
	if err != nil {
		t.Fatal(err)
	}
	var l Launch
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestSequenceRoundTrips(t *testing.T) {
	in := Launch{Host: "myhost", Dir: "/x/alpha", Name: "sm-claude-s1",
		Argv: []string{"claude", "--resume", "s1"}, Tmux: true}
	got := decode(t, Sequence(in, false))
	if got.Host != in.Host || got.Dir != in.Dir || got.Name != in.Name ||
		!got.Tmux || got.Attach || len(got.Argv) != 3 || got.Argv[0] != "claude" {
		t.Errorf("round trip = %+v", got)
	}
}

func TestSequencePassthroughEnvelope(t *testing.T) {
	plain := Sequence(Launch{Host: "h", Attach: true, Name: "sm-claude-s1"}, false)
	wrapped := Sequence(Launch{Host: "h", Attach: true, Name: "sm-claude-s1"}, true)
	if !strings.HasPrefix(wrapped, "\x1bPtmux;") || !strings.HasSuffix(wrapped, "\x1b\\") {
		t.Fatalf("passthrough framing wrong: %q", wrapped)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(wrapped, "\x1bPtmux;"), "\x1b\\")
	if strings.ReplaceAll(body, "\x1b\x1b", "\x1b") != plain {
		t.Errorf("ESC doubling wrong: %q vs %q", body, plain)
	}
}
