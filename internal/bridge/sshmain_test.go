package bridge

import (
	"runtime"
	"strings"
	"testing"
)

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
