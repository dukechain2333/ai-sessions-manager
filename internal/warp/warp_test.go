package warp

import (
	"strings"
	"testing"
)

func TestNewRejectsNonWarp(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("WARP_TERMINAL_SESSION_UUID", "")
	if _, err := New(); err == nil || !strings.Contains(err.Error(), "not Warp") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewAcceptsWarpEnv(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	t.Setenv("WARP_TERMINAL_SESSION_UUID", "")
	o, err := New()
	if err != nil {
		t.Skipf("environment cannot host Warp windows: %v", err) // linux without xdg-open
	}
	if o.dir == "" || !strings.Contains(o.dir, "tab_configs") {
		t.Errorf("dir = %q, want a tab_configs path", o.dir)
	}
}

// tmux rewrites TERM_PROGRAM but panes inherit WARP_*: the UUID marker
// alone must be enough.
func TestNewAcceptsTmuxInheritedMarker(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "tmux")
	t.Setenv("WARP_TERMINAL_SESSION_UUID", "abc-123")
	if _, err := New(); err != nil && strings.Contains(err.Error(), "not Warp") {
		t.Fatalf("UUID marker not honored: %v", err)
	}
}

func TestRenderTabConfig(t *testing.T) {
	got := renderTabConfig(`cd '/d' && exec 'claude' '--resume' 'x1'`)
	want := "name = \"sm\"\n\n[[panes]]\nid = \"main\"\ntype = \"terminal\"\ncommands = [\"cd '/d' && exec 'claude' '--resume' 'x1'\"]\n"
	if got != want {
		t.Errorf("renderTabConfig:\n got %q\nwant %q", got, want)
	}
}

func TestTomlStringEscapes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`plain`, `"plain"`},
		{`say "hi"`, `"say \"hi\""`},
		{`back\slash`, `"back\\slash"`},
		{"tab\there", `"tab\there"`},
		{"nl\nhere", `"nl\nhere"`},
		{"bell\x07here", `"bellhere"`},
		{"del\x7fhere", `"delhere"`},
	}
	for _, c := range cases {
		if got := tomlString(c.in); got != c.want {
			t.Errorf("tomlString(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}
