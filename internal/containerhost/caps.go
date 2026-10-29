package containerhost

import (
	"context"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/workspace"
)

// KnownTools are the commands the probe looks for in the image.
var KnownTools = []string{
	"git", "rg", "grep", "find", "tar", "bash", "make",
	"go", "gopls", "node", "npm", "npx", "python3", "pip", "cargo", "rust-analyzer",
	"clangd", "java", "ruby", "dotnet", "php",
}

// probeScript prints what the image offers, one key=value per line.
// Every path and tool name comes in as a positional argument.
const probeScript = `lib=$1; lib64=$2; shift 2
printf 'uid=%s\n' "$(id -u 2>/dev/null)"
printf 'gid=%s\n' "$(id -g 2>/dev/null)"
printf 'user=%s\n' "$(id -un 2>/dev/null)"
printf 'arch=%s\n' "$(uname -m 2>/dev/null)"
libc=
for f in "$lib"/ld-musl-*; do [ -e "$f" ] && libc=musl; done
if [ -z "$libc" ]; then
  for f in "$lib"/ld-linux* "$lib64"/ld-linux* "$lib"/*-linux-gnu/libc.so.6; do [ -e "$f" ] && libc=glibc; done
fi
printf 'libc=%s\n' "$libc"
for t in "$@"; do command -v "$t" >/dev/null 2>&1 && printf 'tool=%s\n' "$t"; done
exit 0`

// Caps is what the container offers, from the probe New ran.
func (h *Host) Caps() workspace.Caps {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.caps
	c.Tools = append([]string(nil), c.Tools...)
	return c
}

// Owner is the uid and gid commands run as (ok false when unknown).
func (h *Host) Owner() (uid, gid int, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.uid, h.gid, h.idKnown
}

// HasTool reports whether the probe found name in the image.
func (h *Host) HasTool(name string) bool {
	for _, t := range h.Caps().Tools {
		if t == name {
			return true
		}
	}
	return false
}

// probe runs the probe script in the container: user, arch, libc and
// the known tools. Nothing is written in the container.
func (h *Host) probe(ctx context.Context) error {
	argv := append([]string{"sh", "-c", probeScript, "sh", "/lib", "/lib64"}, KnownTools...)
	out, stderr, code, err := h.call(ctx, nil, h.execArgs("-i", "", argv)...)
	if err != nil {
		return err
	}
	if code != 0 {
		return h.engineErr("probe", h.cfg.Root, stderr, code)
	}
	c := workspace.Caps{HasShell: true}
	uid, gid := -1, -1
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "uid":
			if n, err := strconv.Atoi(v); err == nil {
				uid = n
			}
		case "gid":
			if n, err := strconv.Atoi(v); err == nil {
				gid = n
			}
		case "user":
			c.User = v
		case "arch":
			c.Arch = normArch(v)
		case "libc":
			c.Libc = v
		case "tool":
			c.Tools = append(c.Tools, v)
		}
	}
	if c.User == "" && uid >= 0 {
		c.User = strconv.Itoa(uid)
	}
	h.mu.Lock()
	h.caps = c
	if uid >= 0 && gid >= 0 {
		h.uid, h.gid, h.idKnown = uid, gid, true
	}
	h.mu.Unlock()
	return nil
}

// normArch spells uname -m like Go and the engine do.
func normArch(a string) string {
	switch a {
	case "x86_64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	case "armv7l", "armv6l":
		return "arm"
	case "i386", "i686":
		return "386"
	}
	return a
}
