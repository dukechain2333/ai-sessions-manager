package ghostty

import (
	"errors"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}

func opener(goos string, run func(name string, args ...string) (string, error)) *Opener {
	return &Opener{windows: map[string]string{}, goos: goos, run: run}
}

func TestOpenLinuxUsesIPC(t *testing.T) {
	var got call
	o := opener("linux", func(name string, args ...string) (string, error) {
		got = call{name, args}
		return "", nil
	})
	if err := o.Open("k", "cd '/d' && exec 'claude'"); err != nil {
		t.Fatal(err)
	}
	if got.name != "ghostty" {
		t.Fatalf("ran %q", got.name)
	}
	want := []string{"+new-window", "-e", "/bin/sh", "-c", "cd '/d' && exec 'claude'"}
	if len(got.args) != len(want) {
		t.Fatalf("args = %q", got.args)
	}
	for i := range want {
		if got.args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, got.args[i], want[i])
		}
	}
}

func TestOpenDarwinDedupes(t *testing.T) {
	var calls []call
	focusResult := "ok"
	o := opener("darwin", func(name string, args ...string) (string, error) {
		calls = append(calls, call{name, args})
		if strings.Contains(args[1], "new window") { // openScript
			return "1234\n", nil
		}
		return focusResult, nil // focusScript
	})
	if err := o.Open("k", "line"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].name != "osascript" {
		t.Fatalf("first open: %v", calls)
	}
	// typed line carries the history-hiding leading space
	if typed := calls[0].args[2]; typed != " line" {
		t.Fatalf("typed = %q", typed)
	}
	// second launch with the same key focuses instead of reopening
	if err := o.Open("k", "line"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[1].args[2] != "1234" {
		t.Fatalf("expected focus of window 1234: %v", calls[1:])
	}
	// window gone: reopen and remember the new id
	focusResult = "gone"
	if err := o.Open("k", "line"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 || !strings.Contains(calls[3].args[1], "new window") {
		t.Fatalf("expected reopen after gone: %d calls", len(calls))
	}
}

func TestOpenDarwinErrorSurfaced(t *testing.T) {
	o := opener("darwin", func(string, ...string) (string, error) {
		return "", errors.New("execution error: Ghostty got an error")
	})
	if err := o.Open("k", "line"); err == nil || !strings.Contains(err.Error(), "Ghostty") {
		t.Fatalf("err = %v", err)
	}
}
