package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// .gitignore actions and the git identity (GITX-30).

// AddIgnore appends pattern to the .gitignore at the top of the repo
// (made when missing). A pattern already there is not added again.
func AddIgnore(dir, pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || strings.ContainsAny(pattern, "\r\n") {
		return errors.New("an ignore pattern is one line of text")
	}
	p := filepath.Join(dir, ".gitignore")
	old, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, l := range strings.Split(string(old), "\n") {
		if strings.TrimSpace(l) == pattern {
			return nil
		}
	}
	add := pattern + "\n"
	if len(old) > 0 && old[len(old)-1] != '\n' {
		add = "\n" + add
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(add)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// IsTracked reports a file git tracks (in the index).
func IsTracked(dir, rel string) bool {
	out, err := runOut(dir, "git", "ls-files", "--error-unmatch", "--", rel)
	return err == nil && strings.TrimSpace(out) != ""
}

// Untrack stops tracking rel and keeps the file on disk (git rm --cached).
func Untrack(dir, rel string) error {
	_, err := runCombined(dir, "git", "rm", "-q", "--cached", "--", rel)
	return err
}

// Identity is the name and e-mail git puts on a new commit here ("" when
// git does not know one).
func Identity(dir string) (name, email string) {
	out, err := runOut(dir, "git", "var", "GIT_AUTHOR_IDENT")
	if err != nil {
		return "", ""
	}
	s := strings.TrimSpace(out)
	lt, gt := strings.Index(s, " <"), strings.Index(s, ">")
	if lt < 0 || gt < lt {
		return "", ""
	}
	return s[:lt], s[lt+2 : gt]
}

// SetIdentity sets user.name and user.email for this repository, or for
// all repositories (global).
func SetIdentity(dir, name, email string, global bool) error {
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	if name == "" || strings.ContainsAny(name, "\r\n<>") {
		return errors.New("type a name on one line")
	}
	if !strings.Contains(email, "@") || strings.ContainsAny(email, " \r\n<>") {
		return errors.New("type an e-mail address like ann@example.com")
	}
	scope := "--local"
	if global {
		scope = "--global"
	}
	if _, err := runCombined(dir, "git", "config", scope, "user.name", name); err != nil {
		return err
	}
	_, err := runCombined(dir, "git", "config", scope, "user.email", email)
	return err
}
