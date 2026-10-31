package git

import "strings"

// HeadSHA is the full sha of HEAD in dir.
func HeadSHA(dir string) (string, error) {
	out, err := gitOut(dir, "rev-parse", "--verify", "-q", "HEAD")
	return strings.TrimSpace(out), err
}
