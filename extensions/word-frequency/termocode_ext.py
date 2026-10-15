"""termocode_ext — a tiny helper for termocode process extensions (Tier 2).

termocode starts the extension program and talks JSON-RPC 2.0 with it
over stdin/stdout, framed like LSP ("Content-Length: N\\r\\n\\r\\n" + JSON).
Never print to stdout: stdout is the protocol. Use log() or stderr.
Docs: docs/extensions.md, "Process extensions (Tier 2)".

Only the Python standard library is used. Copy this file next to your
main.py; it is the same in every sample.
"""
import itertools
import json
import sys

_in = sys.stdin.buffer
_out = sys.stdout.buffer
_ids = itertools.count(1)
_waiting = {}  # request id -> callback(result, error)
_commands = {}  # command id -> function(args)
_handlers = {}  # method -> function(params)
name = ""  # the extension name (set by initialize)
workspace = ""  # the workspace root (set by initialize)


def _send(msg):
    msg["jsonrpc"] = "2.0"
    body = json.dumps(msg).encode("utf-8")
    _out.write(b"Content-Length: %d\r\n\r\n" % len(body) + body)
    _out.flush()


def _read():
    length = None
    while True:
        line = _in.readline()
        if not line:
            return None  # termocode is gone: stdin closed
        line = line.strip()
        if not line:
            break
        key, _, value = line.decode("ascii").partition(":")
        if key.strip().lower() == "content-length":
            length = int(value.strip())
    if length is None:
        return None
    return json.loads(_in.read(length).decode("utf-8"))


def notify(method, params=None):
    """Send a notification (no answer)."""
    _send({"method": method, "params": params or {}})


def request(method, params, callback):
    """Send a request; callback(result, error) runs when the answer comes."""
    rid = next(_ids)
    _waiting[rid] = callback
    _send({"id": rid, "method": method, "params": params})


def _reply(rid, result=None, error=None):
    if error is not None:
        _send({"id": rid, "error": {"code": -32000, "message": str(error)}})
    else:
        _send({"id": rid, "result": result})


# ── the termocode API (the same names as the Lua API, where it fits) ──


def register_command(cid, title, run, hint=""):
    """A palette row "Ext: <title>". run(args) is called on execute."""
    full = cid if "." in cid else name + "." + cid
    _commands[full] = run
    notify("registerCommand", {"id": cid, "title": title, "hint": hint})


def register_status_item(sid, text, command=""):
    notify("registerStatusItem", {"id": sid, "text": text, "command": command})


def set_status(sid, text):
    notify("statusItem/update", {"id": sid, "text": text})


def register_panel(pid, title, icon=""):
    notify("registerPanel", {"id": pid, "title": title, "icon": icon})


def escape(text):
    """Text shown as-is inside markup ("{{" would start a style)."""
    return text.replace("{{", "{{{{")


def set_panel(pid, lines):
    """Panel lines use the theme markup: "{{accent}}text{{/}}"."""
    notify("panel/setContent", {"id": pid, "lines": list(lines)})


def toast(message, detail="", level="info"):
    notify("notify", {"message": message, "detail": detail, "level": level})


def preview(title, body):
    notify("preview", {"title": title, "body": body})


def open_file(path, line=0, col=0):
    notify("openFile", {"path": path, "line": line, "col": col})


def log(message):
    notify("log", {"message": str(message)})


def prompt(title, label, callback, value=""):
    """callback(text) gets the answer, or None when cancelled."""
    request("prompt", {"title": title, "label": label, "value": value},
            lambda result, error: callback(result))


def pick(title, items, callback):
    """items: [{"title": ..., "hint": ...}]. callback(n) gets the 1-based
    index of the chosen item, or None."""
    request("pick", {"title": title, "items": items},
            lambda result, error: callback(result))


def read_file(path, callback):
    """callback(text, error). Only files inside the workspace."""
    request("readFile", {"path": path},
            lambda result, error: callback((result or {}).get("text"), error))


def on(method, fn):
    """Handle a notification from termocode: "event/notify" (params
    event, path), "panel/select" (panel, row, text)."""
    _handlers[method] = fn


def run(on_start):
    """Serve until termocode stops us. on_start() registers things."""
    global name, workspace
    while True:
        msg = _read()
        if msg is None:
            return
        method = msg.get("method")
        rid = msg.get("id")
        params = msg.get("params") or {}
        if method is None:  # an answer to one of our requests
            cb = _waiting.pop(rid, None)
            if cb:
                cb(msg.get("result"), msg.get("error"))
            continue
        try:
            if method == "initialize":
                name = params.get("name", "")
                workspace = params.get("workspaceRoot", "")
                _reply(rid, {})
                on_start()
            elif method == "command/execute":
                fn = _commands.get(params.get("id"))
                if fn is None:
                    _reply(rid, error="unknown command " + str(params.get("id")))
                else:
                    fn(params.get("args") or [])
                    _reply(rid)
            elif method == "shutdown":
                _reply(rid)
            elif method == "exit":
                return
            elif method in _handlers:
                _handlers[method](params)
                if rid is not None:
                    _reply(rid)
            elif rid is not None:
                _reply(rid, error="method not found: " + method)
        except Exception as e:  # never die on one bad call
            print("error in %s: %r" % (method, e), file=sys.stderr)
            if rid is not None and method != "initialize":
                _reply(rid, error=e)
