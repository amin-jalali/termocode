package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RecentMessages lists the full messages of the last commits on HEAD,
// newest first, each message once. A folder with no commits has none.
func RecentMessages(dir string, n int) ([]string, error) {
	if n <= 0 {
		n = 20
	}
	out, err := runOut(dir, "git", "log", fmt.Sprintf("-n%d", n*3), "--format=%B%x1e")
	if err != nil {
		return nil, nil
	}
	var msgs []string
	seen := map[string]bool{}
	for _, rec := range strings.Split(out, "\x1e") {
		m := strings.TrimSpace(rec)
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		msgs = append(msgs, m)
		if len(msgs) == n {
			break
		}
	}
	return msgs, nil
}

// CommitTemplate is the text of the commit.template file without its
// comment lines ("" when none is set). A relative path is taken from the
// repository root, "~/" from the home folder.
func CommitTemplate(dir string) (string, error) {
	out, err := runOut(dir, "git", "config", "--get", "commit.template")
	p := strings.TrimSpace(out)
	if err != nil || p == "" {
		return "", nil
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, rest)
	} else if !filepath.IsAbs(p) {
		top, err := runOut(dir, "git", "rev-parse", "--show-toplevel")
		if err != nil {
			return "", err
		}
		p = filepath.Join(strings.TrimSpace(top), p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("commit.template: %w", err)
	}
	return CleanMessage(string(b)), nil
}

// CleanMessage is a message as git keeps it with --cleanup=strip, except
// that trailing spaces stay: comment lines and everything below the
// scissors line go, blank lines at both ends are trimmed.
func CleanMessage(s string) string {
	var keep []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(l, "# ") && strings.Contains(l, ">8") {
			break
		}
		if strings.HasPrefix(l, "#") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.TrimRight(strings.TrimLeft(strings.Join(keep, "\n"), "\n"), "\n")
}

// Authors lists "Name <email>" of the people who committed in the last n
// commits of HEAD, newest first, one row per email.
func Authors(dir string, n int) []string {
	if n <= 0 {
		n = 500
	}
	out, err := runOut(dir, "git", "log", fmt.Sprintf("-n%d", n), "--format=%aN <%aE>")
	if err != nil {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		i := strings.LastIndex(l, "<")
		if i < 0 {
			continue
		}
		email := strings.ToLower(l[i:])
		if seen[email] {
			continue
		}
		seen[email] = true
		names = append(names, l)
	}
	return names
}

// AddTrailer appends "key: value" to the message's trailer block (after a
// blank line when there is none yet). A trailer already there is kept
// once.
func AddTrailer(msg, key, value string) string {
	line := key + ": " + value
	msg = strings.TrimRight(msg, "\n \t")
	lines := strings.Split(msg, "\n")
	for _, l := range lines {
		if strings.EqualFold(strings.TrimSpace(l), line) {
			return msg
		}
	}
	last := lines[len(lines)-1]
	if len(lines) > 1 && isTrailer(last) {
		return msg + "\n" + line
	}
	return msg + "\n\n" + line
}

func isTrailer(l string) bool {
	k, _, ok := strings.Cut(l, ": ")
	return ok && k != "" && !strings.ContainsAny(k, " \t")
}

// ChangesDiff is the staged diff (staged) or every change against HEAD,
// with the names of untracked files, cut at max bytes (cut reports it).
func ChangesDiff(dir string, staged bool, max int) (diff string, cut bool, err error) {
	args := []string{"diff", "--no-color", "--no-ext-diff", "--stat", "--patch"}
	if staged {
		args = append(args, "--cached")
	} else if hasCommits(dir) {
		args = append(args, "HEAD")
	}
	out, err := runCombined(dir, "git", args...)
	if err != nil {
		return "", false, err
	}
	if !staged {
		if un, err := runOut(dir, "git", "-c", "core.quotepath=off", "ls-files", "--others", "--exclude-standard"); err == nil && strings.TrimSpace(un) != "" {
			out += "\nNew files not tracked yet:\n" + un
		}
	}
	if max > 0 && len(out) > max {
		cutAt := strings.LastIndexByte(out[:max], '\n')
		if cutAt < 0 {
			cutAt = max
		}
		return out[:cutAt], true, nil
	}
	return out, false, nil
}
