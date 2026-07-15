// Package config loads sm's optional config.json (tmux toggle and per-agent
// colors). Every key is optional; missing or invalid values fall back to the
// built-in defaults.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

// OpenIn values: where resume/new-session launches the agent.
const (
	OpenInCurrent = "current" // suspend sm, run in the current terminal (default)
	OpenInWindow  = "window"  // open a new tmux window; sm stays on screen
)

// DefaultFileJSON is the pretty-printed default config written on first run.
// TestDefaultFileJSONParsesToDefault pins it to Default() so it cannot drift.
const DefaultFileJSON = `{
  "view": "list",
  "open_in": "current",
  "tmux": { "enabled": false },
  "colors": {
    "claude": { "light": "#C15F3C", "dark": "#D97757" },
    "codex":  { "light": "#0A7C66", "dark": "#10A37F" }
  }
}
`

// AgentColors is one agent's light/dark accent hex.
type AgentColors struct{ Light, Dark string }

// Config is the resolved configuration (defaults already filled in).
type Config struct {
	TmuxEnabled bool
	Claude      AgentColors
	Codex       AgentColors
	View        string // startup view mode: "list" (mixed) or "tabs" (per-agent)
	OpenIn      string // where launches open: "current" (this terminal) or "window" (new tmux window)
}

// Default is the built-in configuration used when no file (or no key) is set.
func Default() Config {
	return Config{
		TmuxEnabled: false,
		Claude:      AgentColors{Light: "#C15F3C", Dark: "#D97757"},
		Codex:       AgentColors{Light: "#0A7C66", Dark: "#10A37F"},
		View:        "list",
		OpenIn:      OpenInCurrent,
	}
}

// Path resolves the config path: override if non-empty, else
// $XDG_CONFIG_HOME/sm/config.json, else ~/.config/sm/config.json.
func Path(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "sm", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "sm", "config.json"), nil
}

// EnsureDefault writes DefaultFileJSON to path when no file exists there,
// creating parent directories. It never overwrites an existing file, so a
// user's edited config is always preserved. created reports whether a file
// was written.
func EnsureDefault(path string) (created bool, err error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil // already exists — never overwrite
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err // e.g. permission error stat-ing the path
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(DefaultFileJSON), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

type fileColors struct {
	Light string `json:"light"`
	Dark  string `json:"dark"`
}

type fileConfig struct {
	View   *string `json:"view"`
	OpenIn *string `json:"open_in"`
	Tmux   *struct {
		Enabled bool `json:"enabled"`
	} `json:"tmux"`
	Colors *struct {
		Claude *fileColors `json:"claude"`
		Codex  *fileColors `json:"codex"`
	} `json:"colors"`
}

var hexRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Load reads and validates the config at path. A missing file returns
// Default() and nil. Malformed JSON returns Default() and an error so the
// caller can surface one dialog. Missing/invalid fields keep their defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	var f fileConfig
	if err := json.Unmarshal(data, &f); err != nil {
		return Default(), err
	}
	if f.Tmux != nil {
		cfg.TmuxEnabled = f.Tmux.Enabled
	}
	if f.Colors != nil {
		applyColors(&cfg.Claude, f.Colors.Claude)
		applyColors(&cfg.Codex, f.Colors.Codex)
	}
	if f.View != nil && (*f.View == "list" || *f.View == "tabs") {
		cfg.View = *f.View
	}
	if f.OpenIn != nil && (*f.OpenIn == OpenInCurrent || *f.OpenIn == OpenInWindow) {
		cfg.OpenIn = *f.OpenIn
	}
	return cfg, nil
}

// applyColors overrides dst with any present, valid hex fields in src.
func applyColors(dst *AgentColors, src *fileColors) {
	if src == nil {
		return
	}
	if hexRE.MatchString(src.Light) {
		dst.Light = src.Light
	}
	if hexRE.MatchString(src.Dark) {
		dst.Dark = src.Dark
	}
}
