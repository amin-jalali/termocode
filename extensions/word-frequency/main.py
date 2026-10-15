#!/usr/bin/env python3
"""word-frequency — a sample termocode process extension (Tier 2).

A sidebar panel with the most used words of the file you open or save,
and a status item with its word count. It runs as its own Python
process and talks JSON-RPC with termocode (see termocode_ext.py).
"""
import collections
import os
import re

import termocode_ext as tc

WORD = re.compile(r"[^\W\d_]{3,}", re.UNICODE)
TOP = 30
last_path = None


def show(path, text):
    words = [w.lower() for w in WORD.findall(text)]
    counts = collections.Counter(words).most_common(TOP)
    lines = [
        "{{accent bold}}" + tc.escape(os.path.basename(path)) + "{{/}}",
        "{{muted}}%d words, %d different{{/}}" % (len(words), len(set(words))),
        "",
    ]
    if not counts:
        lines.append("{{muted}}No words here.{{/}}")
    width = max((len(w) for w, _ in counts), default=0)
    for word, n in counts:
        lines.append("%s  {{number}}%d{{/}}" % (tc.escape(word.ljust(width)), n))
    tc.set_panel("main", lines)
    tc.set_status("words", "{{muted}}%d words{{/}}" % len(words))


def load(path):
    global last_path
    last_path = path

    def done(text, error):
        if error:
            tc.log("cannot read %s: %s" % (path, error.get("message")))
            return
        show(path, text or "")

    tc.read_file(path, done)


def on_event(params):
    if params.get("event") in ("open", "save") and params.get("path"):
        load(params["path"])


def refresh(args):
    if last_path:
        load(last_path)
    else:
        tc.toast("Word Frequency: open or save a file first")


def on_select(params):
    text = (params.get("text") or "").split()
    if text and last_path and params.get("row", 0) > 3:
        tc.toast("“%s” is used %s times" % (text[0], text[-1]))


def start():
    tc.register_panel("main", "Word Frequency", "W")
    tc.set_panel("main", ["{{muted}}Open or save a file.{{/}}"])
    tc.register_status_item("words", "", "refresh")
    tc.register_command("refresh", "Word Frequency: Refresh", refresh)
    tc.on("event/notify", on_event)
    tc.on("panel/select", on_select)


if __name__ == "__main__":
    tc.run(start)
