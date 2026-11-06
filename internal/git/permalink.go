package git

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Web links (GITX-29): the page of a remote, a line at a commit and a
// commit. GitHub's link form is used, and GitLab's on a host whose name
// has "gitlab" in it.

// WebBase turns a remote URL (https, ssh:// or scp-style user@host:path)
// into https://host/path, without the user, a token, the port or .git.
// A local path or file:// URL has no web page (ok=false).
func WebBase(remote string) (string, bool) {
	remote = strings.TrimSpace(remote)
	var host, path string
	switch {
	case strings.HasPrefix(remote, "https://"), strings.HasPrefix(remote, "http://"), strings.HasPrefix(remote, "ssh://"):
		u, err := url.Parse(remote)
		if err != nil || u.Hostname() == "" {
			return "", false
		}
		host, path = u.Hostname(), u.Path
	case strings.Contains(remote, ":") && !strings.Contains(remote, "://"):
		h, p, _ := strings.Cut(remote, ":")
		if i := strings.LastIndexByte(h, '@'); i >= 0 {
			h = h[i+1:]
		}
		if len(h) < 2 || strings.ContainsAny(h, "/\\") { // C:\dir is a Windows path
			return "", false
		}
		host, path = h, p
	default:
		return "", false
	}
	path = strings.Trim(strings.TrimSuffix(strings.Trim(path, "/"), ".git"), "/")
	if path == "" {
		return "", false
	}
	return "https://" + host + "/" + path, true
}

// isGitLab reports whether base is a GitLab page.
func isGitLab(base string) bool {
	u, err := url.Parse(base)
	return err == nil && strings.Contains(strings.ToLower(u.Hostname()), "gitlab")
}

// LineLink is the web link of lines from..to of path at sha.
func LineLink(base, sha, path string, from, to int) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	blob := "/blob/"
	if isGitLab(base) {
		blob = "/-/blob/"
	}
	link := base + blob + sha + "/" + strings.Join(segs, "/") + "#L" + strconv.Itoa(from)
	if to > from {
		if isGitLab(base) {
			link += "-" + strconv.Itoa(to)
		} else {
			link += "-L" + strconv.Itoa(to)
		}
	}
	return link
}

// CommitLink is the web link of a commit.
func CommitLink(base, sha string) string {
	if isGitLab(base) {
		return base + "/-/commit/" + sha
	}
	return base + "/commit/" + sha
}

// RemoteWeb is the web page of origin, or of the only remote.
func RemoteWeb(dir string) (string, error) {
	out, err := runOut(dir, "git", "remote")
	if err != nil {
		return "", err
	}
	names := strings.Fields(out)
	name := ""
	for _, n := range names {
		if n == "origin" {
			name = n
		}
	}
	if name == "" && len(names) == 1 {
		name = names[0]
	}
	if name == "" {
		return "", errors.New("no remote named origin")
	}
	u, err := runOut(dir, "git", "remote", "get-url", name)
	if err != nil {
		return "", err
	}
	base, ok := WebBase(u)
	if !ok {
		return "", errors.New("the remote " + name + " has no web page")
	}
	return base, nil
}

// OnRemote reports whether a remote-tracking branch contains sha.
func OnRemote(dir, sha string) bool {
	out, err := runOut(dir, "git", "branch", "-r", "--contains", sha)
	return err == nil && strings.TrimSpace(out) != ""
}
