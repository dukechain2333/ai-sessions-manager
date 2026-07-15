#!/usr/bin/env python3
"""sm → iTerm2 bridge.

Listens for sm's custom control sequences (OSC 1337 Custom=id=sm:<base64>)
and opens a native iTerm2 window that ssh-es back into the host to run the
agent. Terminal output is untrusted input: every payload field is validated
against a strict pattern before anything runs (the host must start with an
alphanumeric and ssh gets an explicit `--` end-of-options marker, so a host
can never be parsed as a flag), and the only command shape ever executed is
`ssh -t -- <host> <cd+tmux+agent>`.

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

windows = {}


def remote_command(spec):
    """Build the validated remote command, or None to reject the payload."""
    host = spec.get("host", "")
    name = spec.get("name", "")
    dir_ = spec.get("dir", "")
    argv = spec.get("argv") or []
    if not HOST_RE.match(host):
        return None, None, None
    if spec.get("attach"):
        if not NAME_RE.match(name):
            return None, None, None
        return host, name, "exec tmux attach-session -t " + shlex.quote("=" + name)
    if not argv or argv[0] not in AGENTS or not all(ARG_RE.match(a) for a in argv):
        return None, None, None
    inner = " ".join(shlex.quote(a) for a in argv)
    if spec.get("tmux"):
        if not NAME_RE.match(name):
            return None, None, None
        cmd = "cd {d} && exec tmux new-session -A -s {n} -c {d} {i}".format(
            d=shlex.quote(dir_), n=shlex.quote(name), i=inner)
    else:
        cmd = "cd {d} && exec {i}".format(d=shlex.quote(dir_), i=inner)
    # Dedupe key: tracked launches have a unique tmux name; untracked ones
    # must include dir, or every untracked "new session" (bare agent argv)
    # would collide on one window across all projects.
    return host, name or (dir_ + " " + inner), cmd


async def handle(connection, payload):
    spec = json.loads(base64.b64decode(payload))
    print("[sm] payload:", json.dumps(spec, sort_keys=True))
    host, key, cmd = remote_command(spec)
    if not cmd:
        print("[sm] rejected by validation")
        return
    old = windows.get(key)
    if old is not None:
        try:
            await old.async_activate()
            print("[sm] focused existing window for", key)
            return
        except Exception:
            windows.pop(key, None)
    ssh = "ssh -t -- {h} {c}".format(h=shlex.quote(host), c=shlex.quote(cmd))
    print("[sm] running:", ssh)
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
    await session.async_send_text(" " + ssh + "\n")
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
