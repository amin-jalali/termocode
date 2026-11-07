package containerhost

import (
	"context"
	"io"
	"strings"
)

// Language servers for a container workspace (plan RC, phase 5). The
// server runs in the image when the image has it (`exec -i`); else a
// local one reads a bind mount or the local mirror of the code. Nothing
// is installed or written in the container.

// lspFindScript prints "name=path" for each command found: on PATH,
// then in the project's own tool folders and the user's Go and Cargo
// bins. Root and names come in as positional arguments.
const lspFindScript = `root=$1; shift
for t in "$@"; do
  p=$(command -v "$t" 2>/dev/null)
  case "$p" in /*) ;; *) p= ;; esac
  if [ -z "$p" ]; then
    for d in "$root/node_modules/.bin" "$root/.venv/bin" "$root/venv/bin" "$HOME/go/bin" "$HOME/.cargo/bin" "$HOME/.local/bin"; do
      if [ -x "$d/$t" ]; then p="$d/$t"; break; fi
    done
  fi
  [ -n "$p" ] && printf '%s=%s\n' "$t" "$p"
done
exit 0`

// FindCommands looks for the given commands in the image and returns
// name → absolute path for each one found. An image without a shell
// has none of them (empty map, no error).
func (h *Host) FindCommands(ctx context.Context, names []string) (map[string]string, error) {
	found := map[string]string{}
	if len(names) == 0 || !h.Caps().HasShell {
		return found, nil
	}
	argv := append([]string{"sh", "-c", lspFindScript, "sh", h.cfg.Root}, names...)
	out, stderr, code, err := h.call(ctx, nil, h.execArgs("-i", "", argv)...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		h.noteStopped(code, stderr)
		return nil, h.engineErr("find", h.cfg.Root, stderr, code)
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && want[k] && strings.HasPrefix(v, "/") {
			found[k] = v
		}
	}
	return found, nil
}

// LSPArgv is the local command line that runs a language server in the
// container on stdio: `exec -i -w <root> [-u user] <c> argv…` (no tty,
// so the LSP stream stays clean).
func (h *Host) LSPArgv(argv []string) []string {
	return h.argv(h.execArgs("-i", "", argv)...)
}

// TreeTar streams the tar of the whole workspace root (`cp <c>:<root> -`)
// for the local mirror. Every name starts with the root's base name.
// stop ends the stream early.
func (h *Host) TreeTar(ctx context.Context) (io.ReadCloser, func(), error) {
	return h.cfg.X.Stream(ctx, "", h.argv(h.cpOutArgs(h.cfg.Root, true)...)...)
}
