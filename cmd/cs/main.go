package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dukechain2333/claude-sessions/internal/ui"
)

var version = "0.1.0"

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cs:", err)
		os.Exit(1)
	}
	projectsDir := flag.String("projects-dir", filepath.Join(home, ".claude", "projects"), "Claude Code projects directory")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("cs", version)
		return
	}
	p := tea.NewProgram(ui.New(*projectsDir), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "cs:", err)
		os.Exit(1)
	}
}
