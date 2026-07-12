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
