# Native OS windows — the full guide

With `"open_in": "window"` in [`config.json`](../README.md#configuration),
pressing `enter` (resume) or `n` (new session) opens the agent in a **real
terminal window** while `sm` stays exactly where you ran it. This guide
covers the two supported terminals in depth; the
[README section](../README.md#opening-launches-in-new-windows) has the
short version.

Shared behavior, regardless of terminal:

- With `tmux.enabled`, every launch lands in a tracked tmux session named
  `sm-<agent>-<id8>` **on the machine where sm runs** — the usual `●`
  marker, `x` kill, and `enter` re-enter all work. Closing a window is
  fine: the tmux session keeps running, and the next `enter` opens a fresh
  window into it.
- Repeating a launch whose window is still open focuses that window
  instead of opening a duplicate (iTerm2, and Ghostty on macOS).
- Over SSH, each window dials its own fresh connection, so key-based
  (non-interactive) login to the server must already work.
- `sm` itself never wraps into tmux in these modes.

---

## iTerm2 (macOS)

### How it works

Pressing `enter` makes `sm` write an invisible [custom control
sequence](https://iterm2.com/python-api/customcontrol.html) to your
terminal — it travels through SSH like any other output. An AutoLaunch
script inside your local iTerm2 picks it up and opens a native window that
runs the agent: locally it types the command straight into a fresh shell;
over SSH it dials back with
`ssh -t <host> "cd <dir> && tmux new-session -A -s sm-<agent>-<id8> <agent's resume command>"`.

### Setup

**Step 1 — install the bridge (on the Mac, one command):**

```sh
curl -fsSL https://raw.githubusercontent.com/dukechain2333/ai-sessions-manager/main/scripts/install-iterm2.sh | sh
```

(It drops one Python file into
`~/Library/Application Support/iTerm2/Scripts/AutoLaunch/`. No `curl`
available? Copy `scripts/iterm2/sm_open_window.py` there by hand or with
`scp`.)

**Step 2 — two switches in iTerm2 (one-time):**

1. *Settings → General → Magic →* check **Enable Python API**.
2. Menu bar *Scripts → AutoLaunch → sm_open_window.py* to start it now
   (it auto-starts with iTerm2 from then on). The very first run offers to
   download iTerm2's Python runtime — accept it.

You can confirm it is listening under *Scripts → Manage → Console*: pick
`sm_open_window` and look for `[sm] bridge listening`.

**Step 3 — configure `sm`:**

- **Local Mac use:** nothing but the mode —

  ```json
  { "open_in": "window" }
  ```

- **Over SSH** (sm runs on the server, iTerm2 on your Mac): also tell `sm`
  how your Mac reaches the server —

  ```json
  { "open_in": { "mode": "window", "iterm2": { "ssh": "myserver" } } }
  ```

  `iterm2.ssh` is whatever you type after `ssh` on the Mac (an alias from
  `~/.ssh/config`, a hostname, or an IP).

*Settings → General → tmux → Open tmux windows as* does not apply here (no
iTerm2 tmux integration is involved); the bridge always opens windows.

### Troubleshooting

Pressing `enter` does nothing, or windows die instantly:

- Open *Scripts → Manage → Console → sm_open_window*. Every keypress logs a
  `[sm] payload:` and `[sm] running:` line. No lines at all → the sequence
  never reached iTerm2 (see the next two items). Lines but no window → read
  the logged error.
- The script isn't running (restart it under *Scripts → AutoLaunch*) or the
  Python API checkbox is off.
- Over SSH: `iterm2.ssh` missing from config, or `$LC_TERMINAL` not
  reaching the host — check `echo $LC_TERMINAL` prints `iTerm2` there; your
  ssh setup must forward `LC_*` (macOS ssh does by default).
- Running `sm` inside a tmux attach: `sm` auto-enables pane passthrough,
  but a tmux older than 3.3 lacks `allow-passthrough` — run `sm` outside
  tmux.
- The window opens but the agent is "command not found": `sm` sends the
  agent's directory (resolved from its own environment) and the bridge
  prepends it to `PATH` remotely, so this should not happen — if it does,
  check that `sm` itself can run the agent (`enter` works with
  `mode: "current"`).
- Shells: the generated commands are plain POSIX and are tested against
  both zsh and bash login shells on the remote side.

---

## Ghostty (macOS & Linux)

Same behavior as the iTerm2 mechanism, different plumbing: Ghostty has no
script that can react to escape sequences, so `sm` drives it directly when
local, and ships its own tunnel — `sm ssh` — for the remote case.

### Local (sm and Ghostty on the same machine)

Nothing to install. Set the mode and you're done:

```json
{ "open_in": "window" }
```

- **macOS:** windows open through Ghostty's AppleScript support (Ghostty
  **1.3+**). The first launch pops the standard macOS Automation
  permission dialog — click Allow.
- **Linux:** windows open through `ghostty +new-window` (Ghostty **1.2+**,
  GTK build with D-Bus). No window refocus dedupe here — the IPC returns
  no window handle.

### Over SSH: `sm ssh`

Install `sm` on the desktop too (macOS: the
[Homebrew cask](../README.md#install); Linux: any install method), then
connect with

```sh
sm ssh myserver          # instead of: ssh myserver
```

`sm ssh` is plain ssh plus a **window bridge**: it adds a reverse-forwarded
unix socket (random path, mode 0600) and advertises it to the remote shell
as `$LC_SM_BRIDGE`. A window-mode `sm` on the server sends each launch down
that socket, and the helper on your desktop opens a Ghostty window that
dials back with
`ssh -t -- myserver "cd <dir> && tmux new-session -A -s sm-<agent>-<id8> <agent command>"`.
Extra arguments are passed through to ssh and reused by the windows
(`sm ssh -p 2222 myserver` works). On the server side the config only needs

```json
{ "open_in": "window" }
```

— no host to configure: new windows always dial the destination you typed
after `sm ssh`, never anything the server asks for.

Requirements:

- key-based login (each window opens a fresh connection);
- the server's sshd must accept `LC_*` environment variables — the stock
  Debian/Ubuntu `AcceptEnv LANG LC_*` default is enough (the same rule the
  iTerm2 mechanism relies on for `LC_TERMINAL`);
- `tmux` on the server if you also want tracking (`tmux.enabled`).

### Troubleshooting

- "window bridge not reachable — reconnect with `sm ssh`": the shell still
  carries a stale `$LC_SM_BRIDGE` (typical inside a long-lived server-side
  tmux attached from a new connection). Launch from a shell of the current
  `sm ssh` session, or `tmux set-environment -g LC_SM_BRIDGE <new value>`.
- `echo $LC_SM_BRIDGE` empty on the server → sshd rejected the variable;
  add `AcceptEnv LC_*` to `/etc/ssh/sshd_config`.
- `sm ssh` says it is connecting **without** the window bridge → the local
  terminal isn't Ghostty (it checks `$TERM_PROGRAM`), or on Linux the
  `ghostty` binary isn't on `PATH`.
- macOS: if you once denied the Automation prompt, re-enable it under
  *System Settings → Privacy & Security → Automation → Ghostty*.
- Debug log: `SM_BRIDGE_DEBUG=1 sm ssh myserver` prints every payload the
  helper accepts or rejects.

---

## Security model

Both mechanisms treat what arrives at the window-opening side as
**untrusted input** — for iTerm2 that is terminal output, for `sm ssh`
anything writable to the tunnel socket. Every payload field is validated
against strict allowlists (host charset, `sm-` session-name shape,
`claude`/`codex` argv allowlist, absolute safe-charset `PATH` directory,
control-character-free working directory), everything is hard-quoted, and
the only command shapes ever run are the `cd` + `tmux` + agent line —
locally, or via `ssh -t --` with an explicit end-of-options marker.

`sm ssh` adds one tightening of its own: the ssh destination embedded in
new windows is always the one from your own command line; a host name
arriving in a payload is ignored outright. The PATH-prepend hint is only
honored for the ssh form — a local launch never lets a payload steer which
binary runs.
