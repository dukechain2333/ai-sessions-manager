package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/ai-sessions-manager/internal/config"
	"github.com/dukechain2333/ai-sessions-manager/internal/tmux"
	"github.com/dukechain2333/ai-sessions-manager/internal/ui"
)

var version = "0.4.2"

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sm:", err)
		os.Exit(1)
	}
	projectsDir := flag.String("projects-dir", filepath.Join(home, ".claude", "projects"), "Claude Code projects directory")
	codexDir := flag.String("codex-dir", filepath.Join(home, ".codex", "sessions"), "Codex sessions directory")
	configPath := flag.String("config", "", "path to config.json (default: ~/.config/sm/config.json)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("sm", version)
		return
	}
	path, err := config.Path(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sm:", err)
		os.Exit(1)
	}
	// On first run, scaffold a default config at the default location so the
	// user has a file to find and edit. Only at the default path — an explicit
	// --config path is the user's to manage. Failure is non-fatal.
	if *configPath == "" {
		if _, err := config.EnsureDefault(path); err != nil {
			fmt.Fprintln(os.Stderr, "sm: config:", err, "(using defaults)")
		}
	}
	cfg, cfgErr := config.Load(path)
	if cfgErr != nil {
		fmt.Fprintln(os.Stderr, "sm: config:", cfgErr, "(using defaults)")
	}
	// open_in "window" wants sm living inside tmux — the windows it opens
	// land in the attached session. Started outside tmux, sm replaces itself
	// with a tmux client on its own session (creating the session or a fresh
	// sm window as needed; a live detached workspace is reattached). Any
	// failure falls through to a normal run: ui.New shows the tmux-missing
	// dialog and downgrades, and the in-app launch check still guards $TMUX.
	if cfg.OpenIn == config.OpenInWindow && os.Getenv("TMUX") == "" &&
		!(cfg.ITerm2SSH != "" && os.Getenv("LC_TERMINAL") == "iTerm2") {
		if tmuxPath, err := exec.LookPath("tmux"); err == nil {
			if self, err := os.Executable(); err == nil {
				cwd, _ := os.Getwd()
				exists, winID := tmux.SelfState()
				selfCmd := append([]string{self}, os.Args[1:]...)
				argv := append([]string{"tmux"}, tmux.SelfWrapArgs(selfCmd, cwd, exists, winID)...)
				_ = syscall.Exec(tmuxPath, argv, os.Environ())
			}
		}
	}
	p := tea.NewProgram(ui.New(*projectsDir, *codexDir, cfg), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sm:", err)
		os.Exit(1)
	}
}
