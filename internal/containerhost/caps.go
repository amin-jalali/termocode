package containerhost

import (
	"context"
	"encoding/json"
	"io/fs"
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
		low := strings.ToLower(stderr)
		switch {
		case strings.Contains(low, "chdir"):
			return &fs.PathError{Op: "open", Path: h.cfg.Root, Err: fs.ErrNotExist}
		case (code == 126 || code == 127) && (strings.Contains(low, "not found") || strings.Contains(low, "no such file")):
			return h.probeNoShell(ctx)
		}
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

// probeNoShell fills Caps for an image without sh (distroless): the
// user from the container's config, the arch from its image.
func (h *Host) probeNoShell(ctx context.Context) error {
	h.mu.Lock()
	user, image := h.cuser, h.image
	h.mu.Unlock()
	if h.cfg.User != "" {
		user = h.cfg.User
	}
	c := workspace.Caps{User: user}
	if c.User == "" {
		c.User = "0"
	}
	if image != "" {
		out, _, code, err := h.call(ctx, nil, "image", "inspect", image)
		if err == nil && code == 0 {
			var items []struct{ Architecture string }
			if json.Unmarshal([]byte(out), &items) == nil && len(items) > 0 {
				c.Arch = normArch(items[0].Architecture)
			}
		}
	}
	uid, gid, ok := numericUser(c.User)
	h.mu.Lock()
	h.caps = c
	h.uid, h.gid, h.idKnown = uid, gid, ok
	h.mu.Unlock()
	return nil
}

// numericUser reads "uid" or "uid:gid" ("" = root). A name has no
// numeric id without a shell to ask.
func numericUser(u string) (uid, gid int, ok bool) {
	if u == "" {
		return 0, 0, true
	}
	us, gs, hasG := strings.Cut(u, ":")
	uid, err := strconv.Atoi(us)
	if err != nil {
		return 0, 0, false
	}
	gid = uid
	if hasG {
		if gid, err = strconv.Atoi(gs); err != nil {
			return 0, 0, false
		}
	}
	return uid, gid, true
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
