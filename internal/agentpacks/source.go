package agentpacks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// FetchTimeout bounds one git fetch of a pack.
var FetchTimeout = 2 * time.Minute

// SplitSource splits "url@version": the version is after the last @ of
// the path part ("git@host:a/b" has none).
func SplitSource(s string) (url, ref string) {
	at, slash := strings.LastIndex(s, "@"), strings.LastIndex(s, "/")
	if at > slash && at > 0 {
		return s[:at], s[at+1:]
	}
	return s, ""
}

// looksGit: a source that is not a local folder but a git address.
func looksGit(s string) bool {
	if strings.Contains(s, "://") {
		return true
	}
	host, _, ok := strings.Cut(s, ":")
	return ok && strings.Contains(host, "@") && !strings.ContainsAny(host, "/\\")
}

// safeRef: a tag, branch or commit name that git can not read as an
// option.
func safeRef(r string) bool {
	if r == "" || strings.HasPrefix(r, "-") || strings.Contains(r, "..") {
		return false
	}
	return strings.Trim(r, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._/-+") == ""
}

// Open reads the pack at source: a local folder as it is, or a git
// address with a pinned version ("url@tag" or "url@commit"), fetched
// into cacheDir and read at exactly that commit.
func Open(ctx context.Context, source, cacheDir string) (Pack, error) {
	source = strings.TrimSpace(source)
	if fi, err := os.Stat(source); err == nil && fi.IsDir() {
		return Load(source)
	}
	if !looksGit(source) {
		return Pack{}, fmt.Errorf("%s is not a folder or a git address", source)
	}
	url, ref := SplitSource(source)
	if ref == "" {
		return Pack{}, fmt.Errorf("pin a version: add @<tag or commit> to %s", source)
	}
	if !safeRef(ref) || strings.HasPrefix(url, "-") {
		return Pack{}, fmt.Errorf("%q is not a tag or commit name", ref)
	}
	sum := sha256.Sum256([]byte(source))
	dest := filepath.Join(cacheDir, "agentpacks-src", hex.EncodeToString(sum[:8]))
	if err := os.RemoveAll(dest); err != nil {
		return Pack{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return Pack{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	if _, err := git(ctx, "", "clone", "--quiet", "--no-checkout", "--", url, dest); err != nil {
		return Pack{}, err
	}
	if _, err := git(ctx, dest, "checkout", "--quiet", "--detach", ref, "--"); err != nil {
		return Pack{}, fmt.Errorf("version %s: %w", ref, err)
	}
	commit, err := git(ctx, dest, "rev-parse", "HEAD")
	if err != nil {
		return Pack{}, err
	}
	p, err := Load(dest)
	p.Source, p.Commit = source, commit
	if p.Version == "local" {
		p.Version = ref
	}
	if p.Name == filepath.Base(dest) {
		p.Name = strings.TrimSuffix(filepath.Base(url), ".git")
	}
	return p, err
}

// git runs git without prompts, hooks or the user's global config.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	pre := []string{"-c", "core.hooksPath=/dev/null", "-c", "advice.detachedHead=false"}
	if dir != "" {
		pre = append(pre, "-C", dir)
	}
	cmd := spawn.CommandContext(ctx, "git", append(pre, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_ASKPASS=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		return "", fmt.Errorf("git %s: %v %s", args[0], err, msg)
	}
	return strings.TrimSpace(string(out)), nil
}
