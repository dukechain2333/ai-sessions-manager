package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.TmuxEnabled {
		t.Error("tmux should default off")
	}
	if c.Claude.Light != "#C15F3C" || c.Claude.Dark != "#D97757" {
		t.Errorf("claude default = %+v", c.Claude)
	}
	if c.Codex.Light != "#0A7C66" || c.Codex.Dark != "#10A37F" {
		t.Errorf("codex default = %+v", c.Codex)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should be no error, got %v", err)
	}
	if c != Default() {
		t.Errorf("missing file = %+v, want Default()", c)
	}
}

func TestLoadPartialOverride(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	os.WriteFile(p, []byte(`{"tmux":{"enabled":true},"colors":{"codex":{"dark":"#123456"}}}`), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !c.TmuxEnabled {
		t.Error("tmux.enabled should be true")
	}
	if c.Codex.Dark != "#123456" {
		t.Errorf("codex.dark = %q, want #123456", c.Codex.Dark)
	}
	if c.Codex.Light != "#0A7C66" {
		t.Errorf("codex.light should keep default, got %q", c.Codex.Light)
	}
	if c.Claude != Default().Claude {
		t.Errorf("claude untouched should stay default, got %+v", c.Claude)
	}
}

func TestLoadMalformedReturnsDefaultsAndError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(p, []byte(`{ this is not json `), 0o644)
	c, err := Load(p)
	if err == nil {
		t.Error("malformed JSON should return an error")
	}
	if c != Default() {
		t.Errorf("malformed should fall back to Default(), got %+v", c)
	}
}

func TestLoadInvalidHexFallsBackPerField(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	os.WriteFile(p, []byte(`{"colors":{"claude":{"light":"blue","dark":"#ABCDEF"}}}`), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Claude.Light != "#C15F3C" {
		t.Errorf("invalid light should fall back to default, got %q", c.Claude.Light)
	}
	if c.Claude.Dark != "#ABCDEF" {
		t.Errorf("valid dark should apply, got %q", c.Claude.Dark)
	}
}

func TestPathHonorsXDGAndOverride(t *testing.T) {
	if got, _ := Path("/tmp/my.json"); got != "/tmp/my.json" {
		t.Errorf("override = %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	got, err := Path("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/xdg/sm/config.json" {
		t.Errorf("XDG path = %q, want /xdg/sm/config.json", got)
	}
}

func TestDefaultFileJSONParsesToDefault(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(p, []byte(DefaultFileJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("DefaultFileJSON must be valid loadable JSON: %v", err)
	}
	if c != Default() {
		t.Errorf("DefaultFileJSON loaded as %+v, want Default() %+v", c, Default())
	}
}

func TestEnsureDefaultCreatesWhenMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "sm", "config.json") // parent dirs absent
	created, err := EnsureDefault(p)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("EnsureDefault should report created=true for a missing file")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("file should exist after EnsureDefault: %v", err)
	}
	if string(data) != DefaultFileJSON {
		t.Errorf("written contents = %q, want DefaultFileJSON", string(data))
	}
	c, err := Load(p)
	if err != nil || c != Default() {
		t.Errorf("created file should load as Default(): c=%+v err=%v", c, err)
	}
}

func TestEnsureDefaultNoOpWhenPresent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	custom := `{"tmux":{"enabled":true}}`
	if err := os.WriteFile(p, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err := EnsureDefault(p)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("EnsureDefault must not report created=true for an existing file")
	}
	data, _ := os.ReadFile(p)
	if string(data) != custom {
		t.Errorf("EnsureDefault overwrote an existing file: got %q, want %q", string(data), custom)
	}
}

func TestLoadView(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"view": "tabs"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil || cfg.View != "tabs" {
		t.Fatalf(`view "tabs": cfg.View=%q err=%v`, cfg.View, err)
	}
	if err := os.WriteFile(p, []byte(`{"view": "bogus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(p)
	if err != nil || cfg.View != "list" {
		t.Fatalf(`unknown view must fall back to "list": cfg.View=%q err=%v`, cfg.View, err)
	}
	if def := Default(); def.View != "list" {
		t.Fatalf(`Default().View = %q, want "list"`, def.View)
	}
}

func TestLoadOpenIn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"open_in": "window"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil || cfg.OpenIn != OpenInWindow {
		t.Fatalf(`open_in "window": cfg.OpenIn=%q err=%v`, cfg.OpenIn, err)
	}
	if err := os.WriteFile(p, []byte(`{"open_in": "bogus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(p)
	if err != nil || cfg.OpenIn != OpenInCurrent {
		t.Fatalf(`unknown open_in must fall back to "current": cfg.OpenIn=%q err=%v`, cfg.OpenIn, err)
	}
	if def := Default(); def.OpenIn != OpenInCurrent {
		t.Fatalf(`Default().OpenIn = %q, want "current"`, def.OpenIn)
	}
}

func TestLoadITerm2SSH(t *testing.T) {
	// The retired top-level "iterm2" key must be ignored — it moved under
	// open_in (TestLoadOpenInObjectForm covers the live location).
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"iterm2": {"ssh": "myhost"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil || cfg.ITerm2SSH != "" {
		t.Fatalf(`legacy top-level iterm2 must be inert: cfg.ITerm2SSH=%q err=%v`, cfg.ITerm2SSH, err)
	}
	if def := Default(); def.ITerm2SSH != "" {
		t.Fatalf(`Default().ITerm2SSH = %q, want ""`, def.ITerm2SSH)
	}
}

func TestLoadOpenInObjectForm(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"open_in": {"mode": "window", "iterm2": {"ssh": "myhost"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil || cfg.OpenIn != OpenInWindow || cfg.ITerm2SSH != "myhost" {
		t.Fatalf("object form: OpenIn=%q ITerm2SSH=%q err=%v", cfg.OpenIn, cfg.ITerm2SSH, err)
	}
	if err := os.WriteFile(p, []byte(`{"open_in": {"mode": "bogus"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(p)
	if err != nil || cfg.OpenIn != OpenInCurrent {
		t.Fatalf(`bogus object mode must fall back: OpenIn=%q err=%v`, cfg.OpenIn, err)
	}
	// The bare-string shorthand must keep working.
	if err := os.WriteFile(p, []byte(`{"open_in": "window"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(p)
	if err != nil || cfg.OpenIn != OpenInWindow || cfg.ITerm2SSH != "" {
		t.Fatalf("string shorthand: OpenIn=%q ITerm2SSH=%q err=%v", cfg.OpenIn, cfg.ITerm2SSH, err)
	}
}
