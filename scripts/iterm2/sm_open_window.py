#!/usr/bin/env python3
"""sm → iTerm2 bridge.

Listens for sm's custom control sequences (OSC 1337 Custom=id=sm:<base64>)
and opens a native iTerm2 window running the agent: over ssh it dials back
into the host, and for a local sm (empty host) it types the command straight
into a fresh local shell. Terminal output is untrusted input: every payload
field is validated against a strict pattern before anything runs (the host
must start with an alphanumeric and ssh gets an explicit `--` end-of-options
marker, so a host can never be parsed as a flag; the PATH-prepend hint is
honored only for the ssh form). The only command shapes ever executed are a
`cd`+`tmux`+`claude|codex` line — locally, or via `ssh -t -- <host>`.

Install: ~/Library/Application Support/iTerm2/Scripts/AutoLaunch/ and enable
Settings → General → Magic → "Enable Python API".
"""
import base64
import json
import re
import shlex

import iterm2

HOST_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._@-]{0,127}$")
NAME_RE = re.compile(r"^sm(-[a-z0-9]+)+$")
AGENTS = ("claude", "codex")
ARG_RE = re.compile(r"^[A-Za-z0-9._/=@-]{1,256}$")
BINDIR_RE = re.compile(r"^/[A-Za-z0-9._@/-]{0,512}$")

windows = {}


def remote_command(spec):
    """Build the validated command, or None to reject the payload.

    An empty host means sm runs on THIS machine (no ssh involved): the
    command is typed directly into the new window's local shell.
    """
    host = spec.get("host", "")
    name = spec.get("name", "")
    dir_ = spec.get("dir", "")
    argv = spec.get("argv") or []
    if host and not HOST_RE.match(host):
        return None, None, None
    if spec.get("attach"):
        if not NAME_RE.match(name):
            return None, None, None
        # Hard single quotes, not shlex.quote: "=" is in shlex's safe set,
        # but an unquoted =word triggers zsh's equals expansion remotely.
        return host, host + "|" + name, "exec tmux attach-session -t '=" + name + "'"
    if not argv or argv[0] not in AGENTS or not all(ARG_RE.match(a) for a in argv):
        return None, None, None
    # dir is free-form (any real path is legal) so it is quoted rather than
    # pattern-matched — but the line is typed into a live shell, where raw
    # control bytes could drive the line editor even inside quotes.
    if any(ord(ch) < 0x20 or ord(ch) == 0x7F for ch in dir_):
        return None, None, None
    inner = " ".join(shlex.quote(a) for a in argv)
    # The remote end of a fresh ssh runs with sshd's bare PATH, and tmux
    # panes it creates inherit that PATH — the agent would be "command not
    # found". sm resolves the agent's directory in the user's real
    # environment and sends it as bindir; prepend it remotely. LOCAL
    # execution deliberately ignores bindir: a fresh local shell already has
    # the user's normal PATH, and honoring a forged bindir here would let
    # terminal output steer which local binary "claude" resolves to.
    path = ""
    bindir = spec.get("bindir", "")
    if bindir and host:
        if not BINDIR_RE.match(bindir):
            return None, None, None
        path = "export PATH=" + shlex.quote(bindir) + ':"$PATH" && '
    if spec.get("tmux"):
        if not NAME_RE.match(name):
            return None, None, None
        cmd = "{p}cd {d} && exec tmux new-session -A -s {n} -c {d} {i}".format(
            p=path, d=shlex.quote(dir_), n=shlex.quote(name), i=inner)
    else:
        cmd = "{p}cd {d} && exec {i}".format(p=path, d=shlex.quote(dir_), i=inner)
    # Dedupe key: tracked launches have a unique tmux name; untracked ones
    # must include dir, or every untracked "new session" (bare agent argv)
    # would collide on one window across all projects.
    # host joins the dedupe key so a local and a remote launch with the same
    # tmux name (or dir+argv) never focus each other's window.
    return host, host + "|" + (name or (dir_ + " " + inner)), cmd


async def handle(connection, payload):
    spec = json.loads(base64.b64decode(payload))
    print("[sm] payload:", json.dumps(spec, sort_keys=True))
    host, key, cmd = remote_command(spec)
    if not cmd:
        print("[sm] rejected by validation")
        return
    old = windows.get(key)
    if old is not None:
        # async_activate on a closed window does not raise — verify against
        # the live app state, or a closed window would eat every relaunch.
        app = await iterm2.async_get_app(connection)
        if app.get_window_by_id(old.window_id) is not None:
            await old.async_activate()
            print("[sm] focused existing window for", key)
            return
        print("[sm] window for", key, "is gone; opening a new one")
        windows.pop(key, None)
    if host:
        line = "ssh -t -- {h} {c}".format(h=shlex.quote(host), c=shlex.quote(cmd))
    else:
        line = cmd  # local sm: run directly in the new window's shell
    print("[sm] running:", line)
    # Open a plain shell window and type the command into it: the user's own
    # shell does the parsing (not iTerm2's tokenizer) and supplies the usual
    # environment (ssh agent etc.); on failure the window stays open showing
    # the error instead of flashing closed. The leading space keeps it out of
    # histories configured with HIST_IGNORE_SPACE.
    win = await iterm2.Window.async_create(connection)
    if win is None:
        print("[sm] window creation failed")
        return
    # The Window object returned by async_create may not carry tab/session
    # data yet; re-fetch it from the app state before typing into it.
    session = win.current_tab.current_session if win.current_tab else None
    if session is None:
        app = await iterm2.async_get_app(connection)
        fresh = app.get_window_by_id(win.window_id)
        if fresh is not None and fresh.current_tab is not None:
            session = fresh.current_tab.current_session
    if session is None:
        print("[sm] no session in new window", win.window_id)
        return
    await session.async_send_text(" " + line + "\n")
    windows[key] = win


async def main(connection):
    print("[sm] bridge listening (identity 'sm')")
    async with iterm2.CustomControlSequenceMonitor(
            connection, "sm", r"^(.+)$") as mon:
        while True:
            match = await mon.async_get()
            try:
                await handle(connection, match.group(1))
            except Exception as e:
                print("[sm] dropped payload:", e)  # never kill the listener


iterm2.run_forever(main)
