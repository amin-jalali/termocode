package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ── Launch configurations (Group D) ──────────────────────────────────────
//
// The CONFIGURATIONS section lists, in order:
//
//  1. entries from <workspace>/.termocode/launch.json — VSCode shape
//     ({"configurations": [ {name, type, request, program, …} ]}; // and
//     /* */ comments plus trailing commas are allowed);
//  2. auto-detected entries from the workspace shape (go.mod, package.json,
//     pyproject.toml, Cargo.toml, …) — ported from mobocode's
//     run_config.dart (detectRunConfigs).
//
// A config whose type is "shell" is a plain run command (no debugger): it
// runs in a new terminal tab. Every other type is a DAP adapter type and is
// handed to nvim-dap as-is (Lua resolves aliases: go→delve, node→pwa-node).
// nvim-dap expands ${file}, ${workspaceFolder}, … itself.

const launchJSONRel = ".termocode/launch.json"

// launchConfig is one runnable / debuggable configuration.
type launchConfig struct {
	Name    string
	Type    string // DAP adapter type, or "shell"
	Request string // launch | attach
	Program string
	Command string // shell configs: the command line
	Cwd     string
	Source  string         // "launch.json" | "auto"
	Raw     map[string]any // full object, passed to dap.run
}

// Debuggable reports whether the config starts a DAP session.
func (c launchConfig) Debuggable() bool { return c.Type != "" && c.Type != "shell" }

// RunCommand is the shell command for "Run Without Debugging": the shell
// command itself, or the language's plain run for a debug config.
func (c launchConfig) RunCommand() string {
	if c.Type == "shell" {
		return c.Command
	}
	prog := c.Program
	if prog == "" {
		prog = "${file}"
	}
	switch c.Type {
	case "go", "delve":
		return "go run " + shellQuote(prog)
	case "python", "debugpy":
		return "python3 " + shellQuote(prog)
	case "node", "pwa-node":
		return "node " + shellQuote(prog)
	}
	return ""
}

// expandLaunchVars substitutes the VSCode variables a run command may use.
func expandLaunchVars(s, root, file string) string {
	dir := filepath.Dir(file)
	base := filepath.Base(file)
	r := strings.NewReplacer(
		"${workspaceFolder}", root,
		"${workspaceRoot}", root,
		"${file}", file,
		"${fileDirname}", dir,
		"${fileBasename}", base,
		"${fileBasenameNoExtension}", strings.TrimSuffix(base, filepath.Ext(base)),
		"${cwd}", root,
	)
	return r.Replace(s)
}

// stripJSONC removes // and /* */ comments (outside strings) and trailing
// commas before } or ], so VSCode-style launch.json files parse.
func stripJSONC(src []byte) []byte {
	var out []byte
	inStr, esc := false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr {
			out = append(out, c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				out = append(out, '\n')
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i++
			continue
		}
		out = append(out, c)
	}
	// Trailing commas: ",<ws>}" / ",<ws>]" → drop the comma.
	res := make([]byte, 0, len(out))
	inStr, esc = false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inStr {
			res = append(res, c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
		}
		if c == ',' {
			j := i + 1
			for j < len(out) && (out[j] == ' ' || out[j] == '\t' || out[j] == '\n' || out[j] == '\r') {
				j++
			}
			if j < len(out) && (out[j] == '}' || out[j] == ']') {
				continue
			}
		}
		res = append(res, c)
	}
	return res
}

// parseLaunchJSON decodes a launch.json body into configs.
func parseLaunchJSON(data []byte) ([]launchConfig, error) {
	var doc struct {
		Configurations []map[string]any `json:"configurations"`
	}
	if err := json.Unmarshal(stripJSONC(data), &doc); err != nil {
		return nil, err
	}
	var out []launchConfig
	for i, raw := range doc.Configurations {
		c := launchConfig{Raw: raw, Source: "launch.json"}
		c.Name, _ = raw["name"].(string)
		c.Type, _ = raw["type"].(string)
		c.Request, _ = raw["request"].(string)
		c.Program, _ = raw["program"].(string)
		c.Command, _ = raw["command"].(string)
		c.Cwd, _ = raw["cwd"].(string)
		if c.Name == "" {
			c.Name = fmt.Sprintf("Configuration %d", i+1)
		}
		if c.Type == "" {
			if c.Command != "" {
				c.Type = "shell"
			} else {
				continue
			}
		}
		if c.Request == "" {
			c.Request = "launch"
			raw["request"] = "launch"
		}
		out = append(out, c)
	}
	return out, nil
}

// loadLaunchJSON reads <root>/.termocode/launch.json. A missing file is not
// an error (nil, nil).
func loadLaunchJSON(root string) ([]launchConfig, error) {
	data, err := os.ReadFile(filepath.Join(root, launchJSONRel))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseLaunchJSON(data)
}

// dapConfig builds an auto-detected debug config.
func dapConfig(name, typ, program string, extra map[string]any) launchConfig {
	raw := map[string]any{"name": name, "type": typ, "request": "launch", "program": program}
	for k, v := range extra {
		raw[k] = v
	}
	return launchConfig{Name: name, Type: typ, Request: "launch", Program: program, Source: "auto", Raw: raw}
}

// shellConfig builds an auto-detected run command.
func shellConfig(name, command string) launchConfig {
	return launchConfig{
		Name: name, Type: "shell", Command: command, Cwd: "${workspaceFolder}", Source: "auto",
		Raw: map[string]any{"name": name, "type": "shell", "command": command},
	}
}

// detectLaunchConfigs inspects the workspace root for project markers and
// synthesizes a starter set of configs (port of detectRunConfigs).
func detectLaunchConfigs(root string) []launchConfig {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}
	var out []launchConfig
	if has("go.mod") {
		out = append(out,
			dapConfig("Go: Debug package", "delve", "${workspaceFolder}", nil),
			dapConfig("Go: Debug current file", "delve", "${file}", nil),
			shellConfig("Go: Run package", "go run ."),
			shellConfig("Go: Test", "go test ./..."),
		)
	}
	if has("pyproject.toml") || has("requirements.txt") || has("setup.py") || has("main.py") {
		out = append(out, dapConfig("Python: Debug current file", "python", "${file}",
			map[string]any{"console": "internalConsole"}))
	}
	if has("package.json") {
		out = append(out,
			dapConfig("Node: Debug current file", "pwa-node", "${file}",
				map[string]any{"cwd": "${workspaceFolder}"}),
			shellConfig("npm start", "npm start"),
		)
	}
	if has("Cargo.toml") {
		out = append(out, shellConfig("cargo run", "cargo run"))
	}
	if has("pubspec.yaml") {
		out = append(out, shellConfig("Dart: run", "dart run"))
	}
	if has("Makefile") {
		out = append(out, shellConfig("make", "make"))
	}
	return out
}

// mergeLaunchConfigs puts launch.json entries first and drops auto entries
// whose name a launch.json entry already uses.
func mergeLaunchConfigs(user, auto []launchConfig) []launchConfig {
	seen := map[string]bool{}
	out := make([]launchConfig, 0, len(user)+len(auto))
	for _, c := range user {
		seen[c.Name] = true
		out = append(out, c)
	}
	for _, c := range auto {
		if !seen[c.Name] {
			out = append(out, c)
		}
	}
	return out
}

// launchJSONTemplate is the body written by "Debug: Open launch.json" when
// the file does not exist yet: the detected debug configs as a start.
func launchJSONTemplate(auto []launchConfig) string {
	var cfgs []map[string]any
	for _, c := range auto {
		cfgs = append(cfgs, c.Raw)
	}
	if len(cfgs) == 0 {
		cfgs = []map[string]any{{
			"name": "Run command", "type": "shell", "command": "echo configure me",
		}}
	}
	data, _ := json.MarshalIndent(map[string]any{"version": "0.2.0", "configurations": cfgs}, "", "  ")
	return string(data) + "\n"
}

// configCache refreshes the merged config list at most every 2 seconds
// (stat-based), so the Run view can call it from View cheaply.
type configCache struct {
	root    string
	checked time.Time
	mtime   time.Time
	list    []launchConfig
	err     string
}

func (c *configCache) get(root string, now time.Time) []launchConfig {
	if c.root == root && now.Sub(c.checked) < 2*time.Second {
		return c.list
	}
	var mtime time.Time
	if st, err := os.Stat(filepath.Join(root, launchJSONRel)); err == nil {
		mtime = st.ModTime()
	}
	if c.root == root && mtime.Equal(c.mtime) && !c.checked.IsZero() && now.Sub(c.checked) < 10*time.Second {
		c.checked = now
		return c.list
	}
	c.root, c.checked, c.mtime = root, now, mtime
	user, err := loadLaunchJSON(root)
	c.err = ""
	if err != nil {
		c.err = "launch.json: " + err.Error()
	}
	c.list = mergeLaunchConfigs(user, detectLaunchConfigs(root))
	return c.list
}

// invalidate forces the next get to reload.
func (c *configCache) invalidate() { c.checked = time.Time{} }
