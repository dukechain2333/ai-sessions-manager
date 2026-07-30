package warp

import (
	"os"
	"path/filepath"
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

type call struct {
	name string
	args []string
}

func opener(goos, dir string, run func(name string, args ...string) (string, error)) *Opener {
	return &Opener{goos: goos, dir: dir, run: run}
}

func TestOpenWritesConfigAndDeliversURI(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tab_configs") // must not pre-exist
	var got call
	o := opener("darwin", dir, func(name string, args ...string) (string, error) {
		got = call{name, args}
		return "", nil
	})
	line := `cd '/d' && exec 'claude'`
	if err := o.Open("ignored-key", line); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "sm.toml"))
	if err != nil {
		t.Fatalf("sm.toml not written: %v", err)
	}
	if want := renderTabConfig(line); string(b) != want {
		t.Errorf("sm.toml = %q, want %q", b, want)
	}
	if got.name != "open" || len(got.args) != 1 || got.args[0] != "warp://tab_config/sm?new_window=true" {
		t.Errorf("ran %q %q", got.name, got.args)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "sm-*"))
	if len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestOpenLinuxUsesXdgOpen(t *testing.T) {
	var got call
	o := opener("linux", filepath.Join(t.TempDir(), "tab_configs"), func(name string, args ...string) (string, error) {
		got = call{name, args}
		return "", nil
	})
	if err := o.Open("k", "line"); err != nil {
		t.Fatal(err)
	}
	if got.name != "xdg-open" || got.args[0] != "warp://tab_config/sm?new_window=true" {
		t.Errorf("ran %q %q", got.name, got.args)
	}
}

func TestOpenSurfacesOpenerError(t *testing.T) {
	o := opener("darwin", filepath.Join(t.TempDir(), "tc"), func(string, ...string) (string, error) {
		return "", os.ErrNotExist
	})
	if err := o.Open("k", "line"); err == nil || !strings.Contains(err.Error(), "warp window:") {
		t.Fatalf("err = %v", err)
	}
}
