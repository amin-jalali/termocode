// Package shellint is the shell integration of the integrated terminal
// (competitive plan T1): small bash / zsh / fish scripts that print
// OSC 133 (prompt start, command start with its text, command end with
// the exit code) and OSC 7 (the current folder), how to start each shell
// so it loads them on top of the user's own rc files, and a parser for
// the sequences.
//
// The scripts never change how the shell behaves: each one first loads
// the file the shell would have loaded anyway (~/.bashrc, $ZDOTDIR/.zshrc,
// fish's own config), then adds a prompt hook. Shells without a script
// (sh, dash, nu, …) start exactly as before.
package shellint

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

//go:embed scripts
var scripts embed.FS

// EnvMarker is set in the shell's environment while termocode loads the
// integration; the scripts unset it again.
const EnvMarker = "TERMOCODE_SHELL_INTEGRATION"

// Dir is the folder the scripts are written to:
// $XDG_DATA_HOME/termocode/shell, else ~/.local/share/termocode/shell.
// "" when no home folder is known.
func Dir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "termocode", "shell")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "termocode", "shell")
}

// installed maps an embedded file to its place under Dir. zsh only reads
// dot files, which go:embed does not take by default.
var installed = map[string]string{
	"scripts/termocode.bash":                         "termocode.bash",
	"scripts/zsh/zshenv":                             "zsh/.zshenv",
	"scripts/zsh/zprofile":                           "zsh/.zprofile",
	"scripts/zsh/zshrc":                              "zsh/.zshrc",
	"scripts/zsh/zlogin":                             "zsh/.zlogin",
	"scripts/fish/fish/vendor_conf.d/termocode.fish": "fish/fish/vendor_conf.d/termocode.fish",
}

// Install writes the scripts under dir. A file that already has the
// right content is not touched, so a second call costs a few reads.
func Install(dir string) error {
	if dir == "" {
		return fs.ErrNotExist
	}
	for src, dst := range installed {
		want, err := scripts.ReadFile(src)
		if err != nil {
			return err
		}
		p := filepath.Join(dir, filepath.FromSlash(dst))
		if have, err := os.ReadFile(p); err == nil && bytes.Equal(have, want) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		tmp := p + ".tmp"
		if err := os.WriteFile(tmp, want, 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, p); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	return nil
}

// Script returns an embedded script by its installed path (tests).
func Script(installedPath string) string {
	for src, dst := range installed {
		if dst == installedPath {
			b, _ := scripts.ReadFile(src)
			return string(b)
		}
	}
	return ""
}

// Spec is how to start one shell with the integration: the argv and the
// variables to add to its environment.
type Spec struct {
	Argv []string
	Env  map[string]string
}

// Supported reports whether a shell (path or name) has a script.
func Supported(shell string) bool {
	switch shellName(shell) {
	case "bash", "zsh", "fish":
		return true
	}
	return false
}

func shellName(shell string) string {
	return strings.TrimPrefix(path.Base(filepath.ToSlash(shell)), "-")
}

// For returns how to start shell (a path like /bin/zsh) with the scripts
// installed in dir. ok is false for shells without a script, and when
// the script files are missing: then the caller starts the shell as
// before. getenv reads the current environment (os.Getenv).
func For(shell, dir string, getenv func(string) string) (Spec, bool) {
	if dir == "" || shell == "" {
		return Spec{}, false
	}
	have := func(rel string) bool {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		return err == nil
	}
	env := map[string]string{EnvMarker: "1"}
	switch shellName(shell) {
	case "bash":
		if !have("termocode.bash") {
			return Spec{}, false
		}
		return Spec{Argv: []string{shell, "--init-file", filepath.Join(dir, "termocode.bash")}, Env: env}, true
	case "zsh":
		if !have("zsh/.zshrc") || !have("zsh/.zshenv") {
			return Spec{}, false
		}
		if z := getenv("ZDOTDIR"); z != "" {
			env["TERMOCODE_ZDOTDIR"] = z
		} else {
			env["TERMOCODE_ZDOTDIR"] = getenv("HOME")
			env["TERMOCODE_ZDOTDIR_UNSET"] = "1"
		}
		env["ZDOTDIR"] = filepath.Join(dir, "zsh")
		return Spec{Argv: []string{shell}, Env: env}, true
	case "fish":
		if !have("fish/fish/vendor_conf.d/termocode.fish") {
			return Spec{}, false
		}
		old := getenv("XDG_DATA_DIRS")
		env["TERMOCODE_XDG_DATA_DIRS"] = old
		if old == "" {
			old = "/usr/local/share:/usr/share" // the XDG default
		}
		env["XDG_DATA_DIRS"] = filepath.Join(dir, "fish") + string(os.PathListSeparator) + old
		return Spec{Argv: []string{shell}, Env: env}, true
	}
	return Spec{}, false
}
