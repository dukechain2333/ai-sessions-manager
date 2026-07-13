package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/ai-sessions-manager/internal/config"
	"github.com/dukechain2333/ai-sessions-manager/internal/ui"
)

var version = "0.4.1"

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
	p := tea.NewProgram(ui.New(*projectsDir, *codexDir, cfg), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sm:", err)
		os.Exit(1)
	}
}
