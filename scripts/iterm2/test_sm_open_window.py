"""Offline tests for the sm → iTerm2 bridge script.

No iTerm2 needed: a stub `iterm2` module models just enough of the Python
API (the CreateTab / SendText / Activate RPCs) to drive `handle()` end to
end. The stub reproduces the behavior of the module bundled with iTerm2
3.7 (iterm2 2.22): a Window returned by `Window.async_create` — and the
one re-fetched from App state — has `current_tab` = None for about a
second after creation, because the focus notification that would fill it
in is dropped while the creation-triggered refresh is in flight. The
bridge must therefore never rely on `current_tab`.

Run: python3 -m unittest discover -s scripts/iterm2
"""
import asyncio
import base64
import importlib.util
import json
import os
import shlex
import sys
import types
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))


class _Status:
    OK = 0
    BAD_IDENTIFIER = 1
    SESSION_NOT_FOUND = 1
    INVALID_PROFILE_NAME = 1

    @staticmethod
    def Value(name):
        return getattr(_Status, name)

    @staticmethod
    def Name(value):
        return {0: "OK", 1: "ERROR"}[value]


class _Resp:
    """A protobuf-ish response: attribute access down one level."""
    def __init__(self, **fields):
        for k, v in fields.items():
            setattr(self, k, types.SimpleNamespace(**v))


class FakeITerm2:
    """State of the fake app: windows that exist, text typed, focus calls."""
    def __init__(self):
        self.next_id = 0
        self.windows = {}      # window_id -> session_id
        self.typed = []        # (session_id, text)
        self.activated = []    # window_id
        self.create_tab_calls = 0

    def _new_ids(self):
        self.next_id += 1
        return "pty-%d" % self.next_id, "sess-%d" % self.next_id

    # Objects mirroring iterm2 2.22 right after creation: tabs are populated
    # but the focus-tracked current_tab is not.
    def window_object(self, window_id):
        if window_id not in self.windows:
            return None
        session = types.SimpleNamespace(session_id=self.windows[window_id])
        tab = types.SimpleNamespace(tab_id="t-" + window_id, sessions=[session],
                                    current_session=None)
        return types.SimpleNamespace(window_id=window_id, tabs=[tab],
                                     current_tab=None)


def install_stub(state):
    mod = types.ModuleType("iterm2")
    rpc = types.ModuleType("iterm2.rpc")
    pb2 = types.ModuleType("iterm2.api_pb2")

    async def async_create_tab(connection, profile=None, window=None,
                               index=None, command=None,
                               profile_customizations=None, **kw):
        state.create_tab_calls += 1
        wid, sid = state._new_ids()
        state.windows[wid] = sid
        return _Resp(create_tab_response={"status": _Status.OK,
                                          "window_id": wid, "session_id": sid})

    async def async_send_text(connection, session, text, suppress_broadcast):
        ok = session in state.windows.values()
        if ok:
            state.typed.append((session, text))
        return _Resp(send_text_response={
            "status": _Status.OK if ok else _Status.SESSION_NOT_FOUND})

    async def async_activate(connection, select_session, select_tab,
                             order_window_front, session_id=None, tab_id=None,
                             window_id=None, activate_app_opts=None):
        ok = window_id in state.windows
        if ok:
            state.activated.append(window_id)
        return _Resp(activate_response={
            "status": _Status.OK if ok else _Status.BAD_IDENTIFIER})

    rpc.async_create_tab = async_create_tab
    rpc.async_send_text = async_send_text
    rpc.async_activate = async_activate
    pb2.CreateTabResponse = types.SimpleNamespace(Status=_Status)
    pb2.SendTextResponse = types.SimpleNamespace(Status=_Status)
    pb2.ActivateResponse = types.SimpleNamespace(Status=_Status)

    class Window:
        @staticmethod
        async def async_create(connection, profile=None, command=None,
                               profile_customizations=None):
            resp = await async_create_tab(connection)
            return state.window_object(resp.create_tab_response.window_id)

    class App:
        def get_window_by_id(self, window_id):
            return state.window_object(window_id)

    async def async_get_app(connection, create_if_needed=True):
        return App()

    class CustomControlSequenceMonitor:
        def __init__(self, *a, **k):
            pass

    mod.rpc = rpc
    mod.api_pb2 = pb2
    mod.Window = Window
    mod.async_get_app = async_get_app
    mod.CustomControlSequenceMonitor = CustomControlSequenceMonitor
    mod.run_forever = lambda main: None  # importing the script must not block
    sys.modules["iterm2"] = mod
    sys.modules["iterm2.rpc"] = rpc
    sys.modules["iterm2.api_pb2"] = pb2
    return mod


def load_bridge():
    spec = importlib.util.spec_from_file_location(
        "sm_open_window", os.path.join(HERE, "sm_open_window.py"))
    bridge = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(bridge)
    return bridge


def payload(spec):
    return base64.b64encode(json.dumps(spec).encode()).decode()


class HandleTests(unittest.TestCase):
    def setUp(self):
        self.state = FakeITerm2()
        install_stub(self.state)
        self.bridge = load_bridge()

    def tearDown(self):
        for name in ("iterm2", "iterm2.rpc", "iterm2.api_pb2"):
            sys.modules.pop(name, None)

    def handle(self, spec):
        asyncio.run(self.bridge.handle(None, payload(spec)))

    def test_types_command_into_the_created_session(self):
        # The regression: with current_tab unset the old script bailed out
        # ("no session in new window") and the window sat at a bare prompt.
        self.handle({"host": "", "name": "sm-claude-abc12345", "attach": True})
        self.assertEqual(self.state.create_tab_calls, 1)
        self.assertEqual(self.state.typed, [
            ("sess-1", " exec tmux attach-session -t '=sm-claude-abc12345'\n")])

    def test_second_launch_in_same_process_also_types(self):
        # Two different launches back to back — the shape that failed live.
        self.handle({"host": "", "name": "sm-claude-abc12345", "attach": True})
        self.handle({"host": "", "name": "sm-codex-def67890", "attach": True})
        self.assertEqual([s for s, _ in self.state.typed], ["sess-1", "sess-2"])

    def test_ssh_form_dials_back(self):
        self.handle({"host": "myserver", "dir": "/home/me/proj", "name": "sm-claude-abc12345",
                     "argv": ["claude", "--resume", "abc"], "tmux": True, "bindir": "/usr/local/bin"})
        sid, text = self.state.typed[0]
        self.assertEqual(sid, "sess-1")
        self.assertTrue(text.startswith(" ssh -t -- myserver "), text)
        inner = shlex.split(text)[-1]  # the single-quoted remote command
        self.assertEqual(
            inner,
            'export PATH=/usr/local/bin:"$PATH" && cd /home/me/proj && '
            'exec tmux new-session -A -s sm-claude-abc12345 -c /home/me/proj claude --resume abc')

    def test_relaunch_refocuses_live_window_without_opening_another(self):
        spec = {"host": "", "name": "sm-claude-abc12345", "attach": True}
        self.handle(spec)
        self.handle(spec)
        self.assertEqual(self.state.create_tab_calls, 1)
        self.assertEqual(self.state.activated, ["pty-1"])
        self.assertEqual(len(self.state.typed), 1)

    def test_relaunch_after_window_closed_opens_a_new_one(self):
        spec = {"host": "", "name": "sm-claude-abc12345", "attach": True}
        self.handle(spec)
        del self.state.windows["pty-1"]  # user closed it
        self.handle(spec)
        self.assertEqual(self.state.create_tab_calls, 2)
        self.assertEqual([s for s, _ in self.state.typed], ["sess-1", "sess-2"])

    def test_rejected_payload_opens_nothing(self):
        self.handle({"host": "-oProxyCommand=evil", "name": "sm-claude-abc12345", "attach": True})
        self.handle({"host": "", "dir": "/tmp", "argv": ["rm", "-rf", "/"]})
        self.assertEqual(self.state.create_tab_calls, 0)
        self.assertEqual(self.state.typed, [])


if __name__ == "__main__":
    unittest.main()
