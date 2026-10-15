package ext

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// PythonHelper is the path, inside extensions.FS, of the Python helper
// every Python process extension copies (termocode_ext.py).
const PythonHelper = "word-frequency/termocode_ext.py"

// ScaffoldProcess creates <dir>/<name>/ as a Python process extension:
// extension.json with "run", main.py and the termocode_ext.py helper
// (copied from helpers, normally extensions.FS). It returns the path of
// main.py and never overwrites.
func ScaffoldProcess(dir, name string, helpers fs.FS) (string, error) {
	name = strings.TrimSpace(name)
	if !ValidName(name) {
		return "", fmt.Errorf("invalid name %q: use letters, digits, _ . - (max 64)", name)
	}
	helper, err := fs.ReadFile(helpers, PythonHelper)
	if err != nil {
		return "", err
	}
	root := filepath.Join(dir, name)
	if _, err := os.Stat(root); err == nil {
		return "", ErrExists
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	manifest := fmt.Sprintf(`{
  "name": %q,
  "version": "0.1.0",
  "description": "",
  "run": { "command": "python3", "args": ["-B", "main.py"] },
  "runtime": "python",
  "activationEvents": ["onStartup"]
}
`, name)
	mainPath := filepath.Join(root, "main.py")
	for _, f := range []struct {
		path string
		body []byte
	}{
		{filepath.Join(root, ManifestFile), []byte(manifest)},
		{filepath.Join(root, "termocode_ext.py"), helper},
		{mainPath, []byte(strings.ReplaceAll(scaffoldPython, "NAME", name))},
	} {
		if err := os.WriteFile(f.path, f.body, 0o644); err != nil {
			return "", err
		}
	}
	return mainPath, nil
}

const scaffoldPython = `#!/usr/bin/env python3
"""NAME — a termocode process extension (Tier 2).

termocode runs this program (see "run" in extension.json) and talks
JSON-RPC with it over stdin/stdout; termocode_ext.py does the protocol.
Any language works the same way: docs/extensions.md, "Process extensions".

Never print() to stdout: it is the protocol. Use tc.log() or stderr;
both show in "Extensions: Show Log". After editing, run
"Extensions: Reload" from the command palette (F1).
"""
import termocode_ext as tc

saves = 0


def hello(args):
    # A palette entry: "Ext: Say Hello". Bind a key in keymap.json with
    #   { "alt+h": "ext:NAME.hello" }
    def answered(value):
        if value:
            tc.toast("Hello, " + value + "!")

    tc.prompt("Hello", "Your name:", answered)


def on_event(params):
    # Events: "open" and "save", with the file's path.
    global saves
    if params.get("event") == "save":
        saves += 1
        tc.set_status("saves", "{{muted}}%d saves{{/}}" % saves)
        tc.set_panel("main", [
            "{{accent bold}}NAME{{/}}",
            "",
            "Last saved: " + tc.escape(params.get("path", "")),
            "{{muted}}Press Enter or click a line.{{/}}",
        ])


def on_select(params):
    tc.toast("Row %d: %s" % (params.get("row", 0), params.get("text", "")))


def start():
    tc.register_command("hello", "Say Hello", hello)
    # A status-bar item; a click runs the command.
    tc.register_status_item("saves", "{{muted}}0 saves{{/}}", "hello")
    # A sidebar panel with its own activity-bar icon. Lines use the theme
    # markup {{token}}...{{/}} (accent, muted, error, bold, ...).
    tc.register_panel("main", "NAME", "N")
    tc.set_panel("main", ["{{accent bold}}NAME{{/}}", "", "{{muted}}Save a file.{{/}}"])
    tc.on("event/notify", on_event)
    tc.on("panel/select", on_select)


if __name__ == "__main__":
    tc.run(start)
`
