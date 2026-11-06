package git

import (
	"errors"
	"strings"
)

// GITX-39: the small git leftovers (docs-local/plans/git-gap.md).

// errBadAuthor is the error for an author that is not "Name <e-mail>".
var errBadAuthor = errors.New(`type the author as Name <e-mail>`)

// ParseAuthor checks an author for --author: "Name <e-mail>", a name
// that does not start with "-", an e-mail with an "@". It returns the
// author with the spaces tidied.
func ParseAuthor(s string) (string, error) {
	s = strings.TrimSpace(s)
	lt := strings.LastIndex(s, "<")
	if lt < 0 || !strings.HasSuffix(s, ">") {
		return "", errBadAuthor
	}
	name := strings.Join(strings.Fields(s[:lt]), " ")
	email := strings.TrimSpace(s[lt+1 : len(s)-1])
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "<>\n") ||
		!strings.Contains(email, "@") || strings.ContainsAny(email, "<> \t\n") {
		return "", errBadAuthor
	}
	return name + " <" + email + ">", nil
}

// CommitPaths commits only paths, as they are on disk (git commit
// --only): new files are added and deleted ones removed first; whatever
// else is staged stays staged and out of the commit.
func CommitPaths(dir, message string, paths []string, o CommitOptions) error {
	if len(paths) == 0 {
		return errors.New("no files to commit")
	}
	args, err := commitArgs(message, o)
	if err != nil {
		return err
	}
	if err := runGit(dir, append([]string{"add", "-A", "--"}, paths...)...); err != nil {
		return err
	}
	return commitRun(dir, append(append(args, "--only", "--"), paths...))
}
