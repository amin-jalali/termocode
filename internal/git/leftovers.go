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
