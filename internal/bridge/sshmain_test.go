package bridge

import (
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestBridgeSSHArgsBypassesMultiplexing(t *testing.T) {
	got := bridgeSSHArgs("/tmp/sm-bridge-ab.sock", "/l/helper.sock", "myserver", []string{"-p", "2222"})
	want := []string{
		"-o", "ControlPath=none",
		"-R", "/tmp/sm-bridge-ab.sock:/l/helper.sock",
		"-o", "StreamLocalBindUnlink=yes",
		"-o", "StreamLocalBindMask=0177",
		"-o", "SetEnv=LC_SM_BRIDGE=/tmp/sm-bridge-ab.sock",
		"-p", "2222",
		"myserver",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("args =\n %q\nwant\n %q", got, want)
	}
}

func TestDesktopOpenerSelection(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "")
	t.Setenv("WARP_TERMINAL_SESSION_UUID", "")
	if _, _, err := desktopOpener(); err == nil || !strings.Contains(err.Error(), "Ghostty or Warp") {
		t.Fatalf("no-terminal err = %v", err)
	}
	if runtime.GOOS != "darwin" {
		// Positive cases need the opener binaries Linux CI lacks.
		t.Skip("positive selection cases are darwin-only")
	}
	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	if open, term, err := desktopOpener(); err != nil || term != "Warp tabs" || open == nil {
		t.Fatalf("warp pick: term=%q err=%v", term, err)
	}
	t.Setenv("TERM_PROGRAM", "ghostty")
	if open, term, err := desktopOpener(); err != nil || term != "Ghostty windows" || open == nil {
		t.Fatalf("ghostty pick: term=%q err=%v", term, err)
	}
}
