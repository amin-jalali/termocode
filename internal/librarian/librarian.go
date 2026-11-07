// Package librarian reads outside git repositories for the AI (CMP-91):
// a public repository named owner/name (GitHub), host/owner/name or an
// https URL is cloned shallow into a folder under the temp dir, once,
// with no prompts, no hooks, no credentials, https only and symlinks
// checked out as plain files. The copy has no .git; the AI reads it
// with the workspace file tools pointed at it. Old copies are pruned.
package librarian

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Defaults of a Cache.
const (
	DefaultMaxBytes = 300 << 20 // a checked-out copy over this is refused
	DefaultTTL      = 24 * time.Hour
	DefaultTimeout  = 2 * time.Minute // one clone

	markerFile = ".termocode-librarian" // in a finished copy: its commit
)

// Repo is an outside repository.
type Repo struct{ Host, Owner, Name string }

// String is host/owner/name.
func (r Repo) String() string {
	if r.Host == "" {
		return ""
	}
	return r.Host + "/" + r.Owner + "/" + r.Name
}

// URL is the https clone URL.
func (r Repo) URL() string { return "https://" + r.String() + ".git" }

// Parse reads owner/name (GitHub), host/owner/name or an https URL.
// Other protocols, local paths, passwords in the URL, ports, IP
// addresses and local or dot-less host names are refused.
func Parse(s string) (Repo, error) {
	s = strings.TrimSpace(s)
	bad := func(why string) (Repo, error) {
		return Repo{}, errors.New("not an outside repository (" + why + "): use owner/name or https://host/owner/name")
	}
	if s == "" {
		return bad("empty")
	}
	if strings.ContainsAny(s, " \t\r\n\\@?#") {
		return bad("spaces, @, ? or # in it")
	}
	hasScheme := false
	if i := strings.Index(s, "://"); i >= 0 {
		if !strings.EqualFold(s[:i], "https") {
			return bad("only https")
		}
		s, hasScheme = s[i+3:], true
	} else if strings.Contains(s, ":") {
		return bad("only https")
	}
	if !hasScheme && (strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~")) {
		return bad("a local path")
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	var r Repo
	switch {
	case hasScheme || strings.Contains(parts[0], "."):
		if len(parts) < 3 {
			return bad("no owner/name")
		}
		r = Repo{Host: strings.ToLower(parts[0]), Owner: parts[1], Name: parts[2]}
	case len(parts) == 2:
		r = Repo{Host: "github.com", Owner: parts[0], Name: parts[1]}
	default:
		return bad("no owner/name")
	}
	r.Name = strings.TrimSuffix(r.Name, ".git")
	if err := checkHost(r.Host); err != nil {
		return bad(err.Error())
	}
	if !plainName(r.Owner) || !plainName(r.Name) {
		return bad("owner and name are letters, digits, . _ -")
	}
	return r, nil
}

// checkHost lets only a public-looking DNS name through.
func checkHost(h string) error {
	switch {
	case strings.Contains(h, ":"), strings.HasPrefix(h, "["):
		return errors.New("no ports or IP addresses")
	case net.ParseIP(h) != nil:
		return errors.New("no IP addresses")
	case h == "localhost", strings.HasSuffix(h, ".localhost"), strings.HasSuffix(h, ".local"), strings.HasSuffix(h, ".internal"), strings.HasSuffix(h, ".lan"):
		return errors.New("no local hosts")
	case !strings.Contains(h, "."):
		return errors.New("the host needs a domain")
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("not a host name")
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return errors.New("not a host name")
			}
		}
	}
	return nil
}

func plainName(s string) bool {
	if s == "" || len(s) > 100 || s[0] == '.' || s[0] == '-' {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// CheckRef lets a plain branch or tag name through ("" = the default
// branch).
func CheckRef(ref string) error {
	if ref == "" {
		return nil
	}
	if len(ref) > 100 || ref[0] == '-' || ref[0] == '/' || strings.Contains(ref, "..") || strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".lock") {
		return errors.New("not a branch or tag name: " + ref)
	}
	for _, c := range ref {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '.' && c != '_' && c != '-' && c != '/' {
			return errors.New("not a branch or tag name: " + ref)
		}
	}
	return nil
}

// GitFunc runs git with args in dir and returns its output.
type GitFunc func(ctx context.Context, dir string, args ...string) (string, error)

// Cache is where the copies live.
type Cache struct {
	Root     string  // "" = DefaultRoot()
	Git      GitFunc // nil = the real git
	Now      func() time.Time
	MaxBytes int64         // 0 = DefaultMaxBytes
	TTL      time.Duration // 0 = DefaultTTL
}

// DefaultRoot is the folder of the copies: under the temp dir, one per
// user.
func DefaultRoot() string {
	return filepath.Join(os.TempDir(), "termocode-librarian-"+strconv.Itoa(os.Getuid()))
}

func (c Cache) root() string {
	if c.Root == "" {
		return DefaultRoot()
	}
	return c.Root
}

func (c Cache) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// Dir is the copy's folder.
func (c Cache) Dir(r Repo, ref string) string {
	name := r.Name
	if ref != "" {
		name += "@" + strings.ReplaceAll(ref, "/", "_")
	}
	return filepath.Join(c.root(), r.Host, r.Owner, name)
}

// Have is the copy's folder when it was fetched (and not pruned).
func (c Cache) Have(r Repo, ref string) (string, bool) {
	if r.Host == "" || CheckRef(ref) != nil {
		return "", false
	}
	dir := c.Dir(r, ref)
	if _, err := os.Stat(filepath.Join(dir, markerFile)); err != nil {
		return "", false
	}
	return dir, true
}

// Fetched is the result of Fetch.
type Fetched struct {
	Dir    string
	Commit string // the full hash cloned
	Cached bool   // an earlier copy was used
}

// Fetch clones the repository (or returns the copy made before).
func (c Cache) Fetch(ctx context.Context, r Repo, ref string) (Fetched, error) {
	if r.Host == "" {
		return Fetched{}, errors.New("no repository")
	}
	if err := CheckRef(ref); err != nil {
		return Fetched{}, err
	}
	c.Prune()
	if dir, ok := c.Have(r, ref); ok {
		b, _ := os.ReadFile(filepath.Join(dir, markerFile))
		return Fetched{Dir: dir, Commit: strings.TrimSpace(string(b)), Cached: true}, nil
	}
	dir := c.Dir(r, ref)
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return Fetched{}, err
	}
	_ = os.Chmod(c.root(), 0o700)
	tmp, err := os.MkdirTemp(filepath.Dir(dir), filepath.Base(dir)+".part-")
	if err != nil {
		return Fetched{}, err
	}
	done := false
	defer func() {
		if !done {
			_ = os.RemoveAll(tmp)
		}
	}()
	git := c.Git
	if git == nil {
		git = realGit
	}
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	args := []string{"-c", "core.symlinks=false", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
		"-c", "core.hooksPath=/dev/null", "-c", "credential.helper=", "-c", "submodule.recurse=false",
		"clone", "--quiet", "--depth", "1", "--single-branch", "--no-tags"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", r.URL(), tmp)
	if _, err := git(ctx, filepath.Dir(dir), args...); err != nil {
		return Fetched{}, fmt.Errorf("could not clone %s: %w", r, err)
	}
	out, err := git(ctx, tmp, "rev-parse", "HEAD")
	if err != nil {
		return Fetched{}, fmt.Errorf("could not read the commit of %s: %w", r, err)
	}
	commit := strings.TrimSpace(out)
	_ = os.RemoveAll(filepath.Join(tmp, ".git"))
	limit := c.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	if n := dirSize(tmp, limit); n > limit {
		return Fetched{}, fmt.Errorf("%s is too big (over %d MB checked out)", r, limit>>20)
	}
	if err := os.WriteFile(filepath.Join(tmp, markerFile), []byte(commit+"\n"), 0o600); err != nil {
		return Fetched{}, err
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return Fetched{}, err
	}
	done = true
	return Fetched{Dir: dir, Commit: commit}, nil
}

// dirSize adds the file sizes under dir, stopping once over limit.
func dirSize(dir string, limit int64) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			n += fi.Size()
		}
		if n > limit {
			return filepath.SkipAll
		}
		return nil
	})
	return n
}

// Prune removes copies fetched more than the TTL ago and parts of
// clones that never finished; it returns how many copies went.
func (c Cache) Prune() int {
	ttl := c.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	cut := c.now().Add(-ttl)
	gone := 0
	hosts, _ := os.ReadDir(c.root())
	for _, h := range hosts {
		owners, _ := os.ReadDir(filepath.Join(c.root(), h.Name()))
		for _, o := range owners {
			base := filepath.Join(c.root(), h.Name(), o.Name())
			copies, _ := os.ReadDir(base)
			for _, cp := range copies {
				dir := filepath.Join(base, cp.Name())
				if strings.Contains(cp.Name(), ".part-") {
					if fi, err := cp.Info(); err == nil && fi.ModTime().Before(c.now().Add(-time.Hour)) {
						_ = os.RemoveAll(dir)
					}
					continue
				}
				fi, err := os.Stat(filepath.Join(dir, markerFile))
				if err != nil || fi.ModTime().Before(cut) {
					if os.RemoveAll(dir) == nil && err == nil {
						gone++
					}
				}
			}
		}
	}
	return gone
}

// realGit runs git with a small environment: no termocode secrets, no
// user or system git config (credential helpers, URL rewrites), no
// prompts.
func realGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := spawn.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	env := []string{"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_ASKPASS=", "SSH_ASKPASS=", "GCM_INTERACTIVE=never"}
	for _, k := range []string{"PATH", "HOME", "TMPDIR", "LANG", "HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "NO_PROXY", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR", "SYSTEMROOT"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		if msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		return "", err
	}
	return string(out), nil
}
