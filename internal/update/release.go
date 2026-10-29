// Package update finds a newer termocode release on GitHub and, for a
// binary the user owns (installed by scripts/install.sh or copied by
// hand), replaces it in place: download over HTTPS, check the SHA-256
// against the release's checksums.txt, take only the termocode binary
// out of the tarball, run it once (--version), then swap it in with an
// atomic rename and keep the old one as termocode.old for one rollback.
//
// Binaries that a package manager owns (Homebrew, pacman / AUR, go
// install) are never replaced: the caller shows the command to run.
// Nothing here needs root.
package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Repo is the GitHub repository releases come from.
const Repo = "amin-jalali/termocode"

// LatestURL is the GitHub API address of the newest full release (drafts
// and pre-releases are left out by GitHub).
const LatestURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

// maxReleaseJSON caps the API answer: a release is a few KB.
const maxReleaseJSON = 1 << 20

// Release is the part of a GitHub release termocode uses.
type Release struct {
	Tag        string  `json:"tag_name"`
	Name       string  `json:"name"`
	HTMLURL    string  `json:"html_url"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset is one file of a release.
type Asset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}

// ParseRelease reads a GitHub API release answer. Drafts, pre-releases
// and tags that are not versions are errors.
func ParseRelease(r io.Reader) (Release, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxReleaseJSON+1))
	if err != nil {
		return Release{}, err
	}
	if len(b) > maxReleaseJSON {
		return Release{}, errors.New("release answer is too large")
	}
	var rel Release
	if err := json.Unmarshal(b, &rel); err != nil {
		return Release{}, fmt.Errorf("release answer: %w", err)
	}
	switch {
	case rel.Draft:
		return Release{}, errors.New("release is a draft")
	case rel.Prerelease:
		return Release{}, errors.New("release is a pre-release")
	case !looksLikeTag(rel.Tag):
		return Release{}, fmt.Errorf("release tag %q is not a version", rel.Tag)
	}
	return rel, nil
}

// looksLikeTag reports whether tag is "v" and a digit at least.
func looksLikeTag(tag string) bool {
	return strings.HasPrefix(tag, "v") && len(tag) > 1 && tag[1] >= '0' && tag[1] <= '9'
}
