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
  "open_in": { "mode": "current", "iterm2": { "ssh": "" } },
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
	ITerm2SSH   string // ssh destination for iTerm2 native-window launches ("" = disabled)
}

// Default is the built-in configuration used when no file (or no key) is set.
func Default() Config {
	return Config{
		TmuxEnabled: false,
		Claude:      AgentColors{Light: "#C15F3C", Dark: "#D97757"},
		Codex:       AgentColors{Light: "#0A7C66", Dark: "#10A37F"},
		View:        "list",
		OpenIn:      OpenInCurrent,
		ITerm2SSH:   "",
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
	View *string `json:"view"`
	// open_in is either the bare mode string ("current"/"window") or an
	// object {mode, iterm2:{ssh}}; parseOpenIn handles both.
	OpenIn json.RawMessage `json:"open_in"`
	Tmux   *struct {
		Enabled bool `json:"enabled"`
	} `json:"tmux"`
	Colors *struct {
		Claude *fileColors `json:"claude"`
		Codex  *fileColors `json:"codex"`
	} `json:"colors"`
}

// fileOpenIn is open_in's object form.
type fileOpenIn struct {
	Mode   *string `json:"mode"`
	ITerm2 *struct {
		SSH *string `json:"ssh"`
	} `json:"iterm2"`
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
	parseOpenIn(&cfg, f.OpenIn)
	return cfg, nil
}

// parseOpenIn applies open_in's two accepted shapes: the bare mode string
// ("current"/"window") as shorthand, or {mode, iterm2:{ssh}}. Unknown modes
// and malformed values keep the defaults, matching the other keys.
func parseOpenIn(cfg *Config, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == OpenInCurrent || s == OpenInWindow {
			cfg.OpenIn = s
		}
		return
	}
	var o fileOpenIn
	if err := json.Unmarshal(raw, &o); err != nil {
		return
	}
	if o.Mode != nil && (*o.Mode == OpenInCurrent || *o.Mode == OpenInWindow) {
		cfg.OpenIn = *o.Mode
	}
	if o.ITerm2 != nil && o.ITerm2.SSH != nil {
		cfg.ITerm2SSH = *o.ITerm2.SSH
	}
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

// ValidHex reports whether s is a #RRGGBB hex color — the same rule Load
// applies to color keys.
func ValidHex(s string) bool {
	return hexRE.MatchString(s)
}

// saveFile mirrors DefaultFileJSON's canonical shape. Save always writes
// the object form of open_in and every known key; the parser knows no
// other keys, so a full rewrite loses nothing.
type saveFile struct {
	View   string     `json:"view"`
	OpenIn saveOpenIn `json:"open_in"`
	Tmux   saveTmux   `json:"tmux"`
	Colors saveColors `json:"colors"`
}

type saveOpenIn struct {
	Mode   string     `json:"mode"`
	ITerm2 saveITerm2 `json:"iterm2"`
}

type saveITerm2 struct {
	SSH string `json:"ssh"`
}

type saveTmux struct {
	Enabled bool `json:"enabled"`
}

type saveColors struct {
	Claude saveAgentColors `json:"claude"`
	Codex  saveAgentColors `json:"codex"`
}

type saveAgentColors struct {
	Light string `json:"light"`
	Dark  string `json:"dark"`
}

// Save writes cfg to path in the canonical config.json shape, creating
// parent directories. It rewrites the whole file; user formatting and
// shorthand forms are normalized away.
func Save(path string, cfg Config) error {
	f := saveFile{
		View:   cfg.View,
		OpenIn: saveOpenIn{Mode: cfg.OpenIn, ITerm2: saveITerm2{SSH: cfg.ITerm2SSH}},
		Tmux:   saveTmux{Enabled: cfg.TmuxEnabled},
		Colors: saveColors{
			Claude: saveAgentColors{Light: cfg.Claude.Light, Dark: cfg.Claude.Dark},
			Codex:  saveAgentColors{Light: cfg.Codex.Light, Dark: cfg.Codex.Dark},
		},
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
