# Auto-create default config.json on first run — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On first run, `sm` writes a default `config.json` to the default config path if none exists, then runs normally; existing files are never touched.

**Architecture:** `internal/config` gains a `DefaultFileJSON` constant (the pretty default text) and `EnsureDefault(path)` (write-if-missing, never overwrite). `cmd/sm/main.go` calls `EnsureDefault` before `Load`, but only when no `--config` override was given; failures are non-fatal.

**Tech Stack:** Go ≥1.24. No new dependencies.

## Global Constraints

- Module `github.com/dukechain2333/ai-sessions-manager`; binary `sm`. Go at `~/.local/go` (prefix shell steps with `export PATH=$HOME/.local/go/bin:$PATH`).
- Written defaults must be behavior-identical to no file: `tmux.enabled=false`; Claude `{Light:"#C15F3C", Dark:"#D97757"}`; Codex `{Light:"#0A7C66", Dark:"#10A37F"}`.
- `EnsureDefault` NEVER overwrites an existing file (user edits are sacred). Dir perms `0o755`, file perms `0o644`.
- Scaffold only at the DEFAULT path — i.e. only when the `--config` flag was not given (`*configPath == ""`). A `--config` path is never auto-created.
- `EnsureDefault` failure is non-fatal: warn to stderr, continue (Load then returns built-in defaults).
- gofmt-clean; `go vet ./...`, `go test ./...` green; commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- After the task: `make install`. DO NOT publish/tag/release. Do NOT bump the `version` var.

---

### Task 1: `DefaultFileJSON` + `EnsureDefault`, wired into `main`

**Files:**
- Modify: `internal/config/config.go` (add `DefaultFileJSON` const + `EnsureDefault` func)
- Modify: `cmd/sm/main.go` (call `EnsureDefault` before `Load`, gated on no `--config`)
- Test: `internal/config/config_test.go` (drift guard + create + no-overwrite)

**Interfaces:**
- Consumes: `config.Default()`, `config.Load(path string) (Config, error)`, `config.Path(override string) (string, error)`.
- Produces: `const DefaultFileJSON string`; `func EnsureDefault(path string) (created bool, err error)`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/config_test.go`:
```go
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
```
(`os`, `filepath`, and `testing` are already imported in `config_test.go`.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/config/ -run 'DefaultFileJSON|EnsureDefault'`
Expected: FAIL (undefined `DefaultFileJSON`, `EnsureDefault`).

- [ ] **Step 3: Add `DefaultFileJSON` and `EnsureDefault`**

In `internal/config/config.go`, add the constant just after the `import (...)` block:
```go
// DefaultFileJSON is the pretty-printed default config written on first run.
// TestDefaultFileJSONParsesToDefault pins it to Default() so it cannot drift.
const DefaultFileJSON = `{
  "tmux": { "enabled": false },
  "colors": {
    "claude": { "light": "#C15F3C", "dark": "#D97757" },
    "codex":  { "light": "#0A7C66", "dark": "#10A37F" }
  }
}
`
```
Add this function (e.g. directly below `Path`):
```go
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
```
(`os`, `errors`, and `filepath` are already imported in `config.go`.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./internal/config/ -run 'DefaultFileJSON|EnsureDefault' -v`
Expected: PASS.

- [ ] **Step 5: Wire `EnsureDefault` into `main`**

In `cmd/sm/main.go`, replace this block:
```go
	path, err := config.Path(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sm:", err)
		os.Exit(1)
	}
	cfg, cfgErr := config.Load(path)
```
with:
```go
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
```

- [ ] **Step 6: Full verification**

Run:
```bash
export PATH=$HOME/.local/go/bin:$PATH
gofmt -w internal/config/config.go internal/config/config_test.go cmd/sm/main.go
gofmt -l internal cmd            # expect empty
go build ./... && go vet ./... && go test ./...
```
Expected: builds; gofmt clean; vet clean; all tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go cmd/sm/main.go
git commit -m "feat(config): scaffold a default config.json on first run

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [ ] **Step 8: Manual check (controller installs after review)**

Confirm a fresh XDG dir gets a config on first run, and an edited one is preserved — WITHOUT touching the real `~/.config/sm/config.json`. `EnsureDefault` runs before the TUI starts, so the file is written even though `sm` then exits immediately for lack of a tty (stdin from `/dev/null`):
```bash
export PATH=$HOME/.local/go/bin:$PATH && make build
TMPX="$(mktemp -d)"
# First run: scaffolds the default config.
XDG_CONFIG_HOME="$TMPX" ./sm </dev/null >/dev/null 2>&1 || true
echo "--- created file ---"; cat "$TMPX/sm/config.json"
# Edit it, run again, confirm it is preserved (never overwritten).
printf '{"tmux":{"enabled":true}}' > "$TMPX/sm/config.json"
XDG_CONFIG_HOME="$TMPX" ./sm </dev/null >/dev/null 2>&1 || true
echo "--- after second run (must be unchanged) ---"; cat "$TMPX/sm/config.json"
rm -rf "$TMPX"
```
Expected: the first `cat` prints the default config (tmux `false`, both color blocks); the second still prints `{"tmux":{"enabled":true}}` (not overwritten). Do NOT `make install` or publish here — the controller installs after review.

---

## Notes for the executor

- Do not bump `version` in `cmd/sm/main.go`; publishing is deferred.
- `EnsureDefault` uses `os.Stat` + `errors.Is(err, os.ErrNotExist)` to distinguish "absent" (create) from a real stat error (surface it). A benign TOCTOU (file appears between stat and write) is acceptable — `WriteFile` would overwrite, but this only runs once at startup and the window is negligible; do not add locking (YAGNI).
