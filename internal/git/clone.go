package git

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Clone support for the dashboard "Clone repository" card.
//
// Auth model: git never gets a terminal (termocode owns it), so the first
// attempt runs with GIT_TERMINAL_PROMPT=0 — the user's credential helper (or
// ssh-agent) still works, but nothing can prompt on the TTY. When git reports
// that it needed credentials, the caller asks the user for a token and
// retries with GIT_ASKPASS pointing at a tiny throw-away script that echoes
// the token from the child's environment. The token is never written to disk
// and never embedded in the remote URL; it is kept only if the user's own
// git credential helper chooses to store it.

// ErrCloneAuth is returned (wrapped) when the clone failed because git needed
// credentials it could not get.
var ErrCloneAuth = errors.New("authentication required")

// CloneProgress is one progress update parsed from `git clone --progress`.
type CloneProgress struct {
	Phase   string // e.g. "Receiving objects"
	Percent int    // 0..100, or -1 when the line has no percentage
}

// CloneOptions configures Clone.
type CloneOptions struct {
	URL      string
	Dest     string
	Username string // used for git's "Username for …" prompt; default "x-access-token"
	Token    string // when set, answered for git's "Password for …" prompt via GIT_ASKPASS
	// OnProgress, when non-nil, receives parsed progress updates. It is
	// called from the goroutine running Clone.
	OnProgress func(CloneProgress)
}

var progressRe = regexp.MustCompile(`^(?:remote:\s*)?([A-Za-z][A-Za-z ]+?):\s+(\d{1,3})%`)

// ParseCloneProgress parses one progress fragment from git's stderr, e.g.
// "Receiving objects:  45% (450/1000), 1.20 MiB | 2.00 MiB/s".
func ParseCloneProgress(line string) (CloneProgress, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return CloneProgress{}, false
	}
	if m := progressRe.FindStringSubmatch(line); m != nil {
		pct, _ := strconv.Atoi(m[2])
		if pct > 100 {
			pct = 100
		}
		return CloneProgress{Phase: strings.TrimSpace(m[1]), Percent: pct}, true
	}
	if strings.HasPrefix(line, "Cloning into") {
		return CloneProgress{Phase: "Cloning", Percent: -1}, true
	}
	return CloneProgress{}, false
}

// IsAuthFailure reports whether git's stderr says it needed credentials.
func IsAuthFailure(stderr string) bool {
	s := strings.ToLower(stderr)
	for _, needle := range []string{
		"terminal prompts disabled",
		"could not read username",
		"could not read password",
		"authentication failed",
		"invalid username or password",
		"http basic: access denied",
		"the requested url returned error: 401",
		"the requested url returned error: 403",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// RepoNameFromURL derives the folder name git would pick for a clone URL:
// the last path segment without ".git". Works for https://, ssh://, scp-like
// "git@host:owner/repo.git" and local paths. Returns "" when nothing usable.
func RepoNameFromURL(url string) string {
	u := strings.TrimSpace(url)
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, ".git")
	u = strings.TrimRight(u, "/")
	if i := strings.LastIndexAny(u, "/:"); i >= 0 {
		u = u[i+1:]
	}
	if u == "" || u == "." || u == ".." {
		return ""
	}
	return u
}

// HostFromURL returns the host part of an https/ssh/scp-like URL ("" for
// local paths).
func HostFromURL(url string) string {
	u := strings.TrimSpace(url)
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
		if j := strings.IndexByte(u, '/'); j >= 0 {
			u = u[:j]
		}
		if j := strings.LastIndexByte(u, '@'); j >= 0 {
			u = u[j+1:]
		}
		if j := strings.IndexByte(u, ':'); j >= 0 {
			u = u[:j]
		}
		return u
	}
	// scp-like: [user@]host:path
	if j := strings.IndexByte(u, ':'); j > 0 && !strings.ContainsAny(u[:j], "/\\") {
		h := u[:j]
		if k := strings.LastIndexByte(h, '@'); k >= 0 {
			h = h[k+1:]
		}
		return h
	}
	return ""
}

// IsHTTPURL reports whether url uses http(s) (the only transport the token
// prompt can help with).
func IsHTTPURL(url string) bool {
	u := strings.ToLower(strings.TrimSpace(url))
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

// DefaultCloneDest picks the suggested clone target: next to the current
// workspace when the workspace is itself a git repo (cloning *into* a repo is
// rarely wanted), otherwise inside it.
func DefaultCloneDest(cwd string, cwdIsRepo bool, name string) string {
	if name == "" {
		name = "repo"
	}
	base := cwd
	if cwdIsRepo {
		base = filepath.Dir(cwd)
	}
	return filepath.Join(base, name)
}

// ExpandHome replaces a leading "~" with the user's home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// CheckCloneDest returns an error when dest exists and is not an empty
// directory (git refuses to clone into those anyway, but checking first gives
// a clearer message before any network work).
func CheckCloneDest(dest string) error {
	st, err := os.Stat(dest)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s already exists and is not a folder", dest)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty", dest)
	}
	return nil
}

// askpassScript answers git's prompts from the child environment so the
// secret never lands in the script file itself.
const askpassScript = `#!/bin/sh
case "$1" in
  [Uu]sername*) printf '%s\n' "${TERMOCODE_GIT_USER:-x-access-token}" ;;
  *) printf '%s\n' "$TERMOCODE_GIT_TOKEN" ;;
esac
`

// cloneEnv builds the child environment: no TTY prompts, batch-mode ssh
// (unless the user configured their own ssh command), and — when a token is
// given — the askpass hook.
func cloneEnv(base []string, askpass, user, token string) []string {
	env := make([]string, 0, len(base)+5)
	hasSSHCmd := false
	for _, kv := range base {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		switch k {
		case "GIT_TERMINAL_PROMPT", "TERMOCODE_GIT_TOKEN", "TERMOCODE_GIT_USER":
			continue
		case "GIT_ASKPASS", "SSH_ASKPASS":
			if askpass != "" {
				continue
			}
		case "GIT_SSH_COMMAND":
			hasSSHCmd = true
		}
		env = append(env, kv)
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0")
	if !hasSSHCmd {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	if askpass != "" {
		if user == "" {
			user = "x-access-token"
		}
		env = append(env,
			"GIT_ASKPASS="+askpass,
			"TERMOCODE_GIT_USER="+user,
			"TERMOCODE_GIT_TOKEN="+token,
		)
	}
	return env
}

// SplitUserToken splits a "user:token" answer; a bare token keeps user "".
func SplitUserToken(s string) (user, token string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ':'); i > 0 && !strings.ContainsAny(s[:i], " /") {
		return s[:i], s[i+1:]
	}
	return "", s
}

// scanCR is a bufio.SplitFunc that splits on '\r' or '\n' — git redraws its
// progress line with carriage returns.
func scanCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// Clone runs `git clone --progress <url> <dest>`. On failure the error
// carries git's last stderr lines; it wraps ErrCloneAuth when git needed
// credentials.
func Clone(opts CloneOptions) error {
	url := strings.TrimSpace(opts.URL)
	if url == "" {
		return errors.New("empty repository URL")
	}
	if err := CheckCloneDest(opts.Dest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.Dest), 0o755); err != nil {
		return err
	}

	askpass := ""
	if opts.Token != "" {
		dir, err := os.MkdirTemp("", "termocode-askpass-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		askpass = filepath.Join(dir, "askpass.sh")
		if err := os.WriteFile(askpass, []byte(askpassScript), 0o700); err != nil {
			return err
		}
	}

	cmd := exec.Command("git", "clone", "--progress", "--", url, opts.Dest)
	cmd.Env = cloneEnv(os.Environ(), askpass, opts.Username, opts.Token)
	cmd.Stdin = nil
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var tail []string
	sc := bufio.NewScanner(stderr)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	sc.Split(scanCR)
	for sc.Scan() {
		line := sc.Text()
		if p, ok := ParseCloneProgress(line); ok {
			if opts.OnProgress != nil {
				opts.OnProgress(p)
			}
			continue
		}
		if strings.TrimSpace(line) != "" {
			tail = append(tail, strings.TrimSpace(line))
			if len(tail) > 8 {
				tail = tail[1:]
			}
		}
	}
	_, _ = io.Copy(io.Discard, stderr)
	if err := cmd.Wait(); err != nil {
		msg := strings.Join(tail, "\n")
		if IsAuthFailure(msg) {
			return fmt.Errorf("%w: %s", ErrCloneAuth, msg)
		}
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}
