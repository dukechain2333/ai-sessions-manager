#!/usr/bin/env python3
"""sm → iTerm2 bridge.

Listens for sm's custom control sequences (OSC 1337 Custom=id=sm:<base64>)
and opens a native iTerm2 window that ssh-es back into the host to run the
agent. Terminal output is untrusted input: every payload field is validated
against a strict pattern before anything runs, and the only command shape
ever executed is `ssh -t <host> <cd+tmux+agent>`.

Install: ~/Library/Application Support/iTerm2/Scripts/AutoLaunch/ and enable
Settings → General → Magic → "Enable Python API".
"""
import base64
import json
import re
import shlex

import iterm2

HOST_RE = re.compile(r"^[A-Za-z0-9._@-]{1,128}$")
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
    return host, name or inner, cmd


async def handle(connection, payload):
    spec = json.loads(base64.b64decode(payload))
    host, key, cmd = remote_command(spec)
    if not cmd:
        return
    old = windows.get(key)
    if old is not None:
        try:
            await old.async_activate()
            return
        except Exception:
            windows.pop(key, None)
    ssh = "/usr/bin/env ssh -t {h} {c}".format(h=shlex.quote(host), c=shlex.quote(cmd))
    win = await iterm2.Window.async_create(connection, command=ssh)
    if win is not None:
        windows[key] = win


async def main(connection):
    async with iterm2.CustomControlSequenceMonitor(
            connection, "sm", r"^(.+)$") as mon:
        while True:
            match = await mon.async_get()
            try:
                await handle(connection, match.group(1))
            except Exception:
                pass  # a bad payload must never kill the listener


iterm2.run_forever(main)
