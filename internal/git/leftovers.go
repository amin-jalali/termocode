package git

import (
	"errors"
	"strconv"
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

// Contributor is one author of the commits on HEAD.
type Contributor struct {
	Name, Email string
	Commits     int
}

// Contributors counts the commits on HEAD by author (git shortlog -sne),
// most commits first.
func Contributors(dir string) ([]Contributor, error) {
	out, err := runCombined(dir, "git", "shortlog", "-sne", "--no-merges", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	var list []Contributor
	for _, l := range strings.Split(out, "\n") {
		n, who, ok := strings.Cut(strings.TrimSpace(l), "\t")
		count, cerr := strconv.Atoi(strings.TrimSpace(n))
		if !ok || cerr != nil {
			continue
		}
		c := Contributor{Name: strings.TrimSpace(who), Commits: count}
		if lt := strings.LastIndex(who, " <"); lt >= 0 && strings.HasSuffix(who, ">") {
			c.Name, c.Email = strings.TrimSpace(who[:lt]), who[lt+2:len(who)-1]
		}
		list = append(list, c)
	}
	return list, nil
}
