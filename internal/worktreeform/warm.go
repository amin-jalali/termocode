package worktreeform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ── Warm setup copies and the setup's env (CMP-96) ───────────────────────
//
// "warm": {"paths": [...], "key": [...]} in .termocode/worktrees.json
// keeps a copy of what the setup made (node_modules, a build cache) in
// <git dir>/termocode/warm/<key>/ after a setup that worked. A new
// worktree whose key is the same gets that copy and skips the setup. The
// key is the config hash, the setup lines, the paths and the bytes of the
// key files (lock files) of the new worktree, so a changed lock file runs
// the setup again. Every path stays inside the worktree.
//
// "env": {"NAME": "value"} adds plain variables to the setup. A value
// with ${secret:…} is dropped: secrets never come from a repo file.

// WarmConfig is the "warm" part of Config.
type WarmConfig struct {
	Paths []string `json:"paths,omitempty"` // what the setup makes, relative to the worktree
	Key   []string `json:"key,omitempty"`   // files whose bytes decide when a copy is stale
}

// WarmOn says whether the config asks for warm copies.
func (c Config) WarmOn() bool { return c.Warm != nil && len(c.Warm.Paths) > 0 && len(c.Setup) > 0 }

// WarmMax caps the size of one warm copy (a var: tests).
var WarmMax int64 = 4 << 30

// WarmKeep is how many warm copies a repository keeps.
const WarmKeep = 3

// ErrWarmTooBig: the copy would be larger than WarmMax.
var ErrWarmTooBig = errors.New("the warm copy is larger than 4 GB")

// warmDone marks a finished copy.
const warmDone = ".termocode-warm"

// WarmKey is the key of the copy for the worktree dir.
func WarmKey(dir, hash string, c Config) (string, error) {
	if c.Warm == nil {
		return "", errors.New("no warm config")
	}
	h := sha256.New()
	fmt.Fprintf(h, "hash %s\n", hash)
	for _, l := range c.Setup {
		fmt.Fprintf(h, "setup %q\n", l)
	}
	for _, p := range c.Warm.Paths {
		fmt.Fprintf(h, "path %q\n", p)
	}
	for _, k := range c.Warm.Key {
		p, ok := inside(dir, strings.TrimSpace(k))
		if !ok {
			return "", fmt.Errorf("warm key %q is outside the worktree", k)
		}
		fmt.Fprintf(h, "key %q\n", k)
		b, err := os.ReadFile(p)
		switch {
		case errors.Is(err, os.ErrNotExist):
			h.Write([]byte("-\n"))
		case err != nil:
			return "", err
		default:
			sum := sha256.Sum256(b)
			h.Write(sum[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// WarmDir is the folder of the copy with key in the git dir common.
func WarmDir(common, key string) string {
	if len(key) > 24 {
		key = key[:24]
	}
	return filepath.Join(common, "termocode", "warm", key)
}

// HasWarm says whether snap holds a finished copy.
func HasWarm(snap string) bool {
	_, err := os.Stat(filepath.Join(snap, warmDone))
	return err == nil
}

// SaveWarm copies paths of the worktree src into snap (replacing an old
// copy) and returns the bytes copied. A path that is not there is left
// out; a path outside src is an error. Nothing is kept on an error.
func SaveWarm(src, snap string, paths []string) (int64, error) {
	tmp := snap + ".tmp-" + strconv.Itoa(os.Getpid())
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return 0, err
	}
	budget := WarmMax
	var kept []string
	for _, rel := range paths {
		rel = strings.TrimSpace(rel)
		from, ok := inside(src, rel)
		if !ok {
			_ = os.RemoveAll(tmp)
			return 0, fmt.Errorf("warm path %q is outside the worktree", rel)
		}
		if _, err := os.Lstat(from); err != nil {
			continue
		}
		to, _ := inside(tmp, rel)
		if err := copyTree(from, to, &budget); err != nil {
			_ = os.RemoveAll(tmp)
			return 0, err
		}
		kept = append(kept, rel)
	}
	if err := os.WriteFile(filepath.Join(tmp, warmDone), []byte(strings.Join(kept, "\n")+"\n"), 0o600); err != nil {
		_ = os.RemoveAll(tmp)
		return 0, err
	}
	_ = os.RemoveAll(snap)
	if err := os.Rename(tmp, snap); err != nil {
		_ = os.RemoveAll(tmp)
		return 0, err
	}
	return WarmMax - budget, nil
}

// RestoreWarm copies the paths of snap into the new worktree dest and
// returns the ones it copied. A path dest has already is kept as it is.
func RestoreWarm(snap, dest string, paths []string) ([]string, error) {
	if !HasWarm(snap) {
		return nil, errors.New("no warm copy")
	}
	budget := WarmMax
	var out []string
	for _, rel := range paths {
		rel = strings.TrimSpace(rel)
		from, ok1 := inside(snap, rel)
		to, ok2 := inside(dest, rel)
		if !ok1 || !ok2 {
			return out, fmt.Errorf("warm path %q is outside the worktree", rel)
		}
		if _, err := os.Lstat(from); err != nil {
			continue
		}
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		if !parentWithin(dest, to) {
			return out, fmt.Errorf("warm path %q leads out of the worktree", rel)
		}
		if err := copyTree(from, to, &budget); err != nil {
			return out, err
		}
		out = append(out, rel)
	}
	return out, nil
}

// parentWithin says whether the folder of p, links followed, is inside
// root (a link in the new worktree must not lead the copy elsewhere).
func parentWithin(root, p string) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	dir := filepath.Dir(p)
	for {
		real, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return within(realRoot, real)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false
		}
		up := filepath.Dir(dir)
		if up == dir {
			return false
		}
		dir = up
	}
}

// PruneWarm keeps the keep newest copies of the git dir common.
func PruneWarm(common string, keep int) {
	base := filepath.Join(common, "termocode", "warm")
	ents, err := os.ReadDir(base)
	if err != nil {
		return
	}
	type aged struct {
		name string
		at   int64
	}
	var all []aged
	for _, e := range ents {
		if !e.IsDir() || strings.Contains(e.Name(), ".tmp-") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		all = append(all, aged{e.Name(), fi.ModTime().UnixNano()})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at > all[j].at })
	for i := keep; i < len(all); i++ {
		_ = os.RemoveAll(filepath.Join(base, all[i].name))
	}
}

// copyTree copies the file, link or folder from to to, keeping modes
// and link targets; budget is the bytes still allowed.
func copyTree(from, to string, budget *int64) error {
	return filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(dst, info.Mode().Perm()|0o700)
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			return os.Symlink(target, dst)
		case info.Mode().IsRegular():
			*budget -= info.Size()
			if *budget < 0 {
				return ErrWarmTooBig
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			return copyFile(p, dst, info.Mode().Perm())
		}
		return nil // sockets, devices, pipes: left out
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// envName says whether s is a shell variable name.
func envName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// envUsable says whether the repo's NAME=value may be used.
func envUsable(name, value string) bool {
	return envName(name) && !strings.Contains(value, "${secret:")
}

// EnvNames are the usable names of the repo's env, sorted.
func EnvNames(env map[string]string) []string {
	var out []string
	for k, v := range env {
		if envUsable(k, v) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// SetupEnv is base with the repo's env: a name base has is replaced in
// place, new names follow, sorted. base is not changed.
func SetupEnv(base []string, env map[string]string) []string {
	out := make([]string, 0, len(base)+len(env))
	done := map[string]bool{}
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if v, ok := env[k]; ok && envUsable(k, v) {
			out = append(out, k+"="+v)
			done[k] = true
			continue
		}
		out = append(out, kv)
	}
	for _, k := range EnvNames(env) {
		if !done[k] {
			out = append(out, k+"="+env[k])
		}
	}
	return out
}
