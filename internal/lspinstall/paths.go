package lspinstall

import (
	"os"
	"path/filepath"
	"strings"
)

// ToolsDirEnv overrides the tools root (used by tests).
const ToolsDirEnv = "TERMOCODE_TOOLS_DIR"

// ToolsDir is the managed tools root:
// $TERMOCODE_TOOLS_DIR, else $XDG_DATA_HOME/termocode/tools, else
// ~/.local/share/termocode/tools.
func ToolsDir() (string, error) {
	if v := os.Getenv(ToolsDirEnv); v != "" {
		return v, nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "termocode", "tools"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "termocode", "tools"), nil
}

// BinDir is tools/bin — the shim dir that goes on nvim's PATH.
func BinDir() (string, error) {
	root, err := ToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin"), nil
}

// ToolDir is tools/<name>.
func ToolDir(name string) (string, error) {
	root, err := ToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name), nil
}

// PrependPath returns a copy of env whose PATH starts with dir. A dir that
// is already the first PATH entry is not added twice. A missing PATH entry
// is created.
func PrependPath(env []string, dir string) []string {
	out := make([]string, 0, len(env)+1)
	found := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			found = true
			cur := strings.TrimPrefix(kv, "PATH=")
			parts := filepath.SplitList(cur)
			if len(parts) > 0 && parts[0] == dir {
				out = append(out, kv)
				continue
			}
			if cur == "" {
				out = append(out, "PATH="+dir)
			} else {
				out = append(out, "PATH="+dir+string(os.PathListSeparator)+cur)
			}
			continue
		}
		out = append(out, kv)
	}
	if !found {
		out = append(out, "PATH="+dir)
	}
	return out
}

// EnvWithToolsBin is os.Environ() with tools/bin prepended to PATH. Falls
// back to the plain environment when the tools dir can't be resolved.
func EnvWithToolsBin() []string {
	env := os.Environ()
	bin, err := BinDir()
	if err != nil {
		return env
	}
	return PrependPath(env, bin)
}
