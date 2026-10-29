package git

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/clock"
)

// RefKind says what a ref on a commit is.
type RefKind uint8

const (
	RefLocal  RefKind = iota // a local branch (main)
	RefRemote                // a remote-tracking branch (origin/main)
	RefTag                   // a tag (v1.2)
)

// Ref is one branch or tag name on a commit.
type Ref struct {
	Name string
	Kind RefKind
}

// GraphCommit is one row of the Source Control GRAPH (no --graph art: the
// sidebar draws one node per commit).
type GraphCommit struct {
	Hash, FullHash string
	Parents        []string // short hashes
	Subject        string
	Author         string // %an; the e-mail is never loaded for the list
	Committed      int64  // committer date, unix seconds
	Refs           []Ref  // local, then tags, then remote; "HEAD -> " dropped
	IsHead         bool
	IsMerge        bool
	Unpushed       bool // on HEAD but not on its upstream
}

// Commits lists limit commits of HEAD's history after skipping skip,
// newest first, and says whether more exist. An empty repository is not
// an error. Commits on HEAD that its upstream lacks are marked Unpushed.
func Commits(dir string, skip, limit int) ([]GraphCommit, bool, error) {
	if limit <= 0 {
		limit = 200
	}
	const format = "--format=%h%x1f%H%x1f%p%x1f%s%x1f%an%x1f%ct%x1f%D%x1e"
	out, err := runOut(dir, "git", "log", "--decorate=full", format,
		fmt.Sprintf("-n%d", limit+1), fmt.Sprintf("--skip=%d", skip), "HEAD", "--")
	if err != nil {
		if !hasCommits(dir) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var cs []GraphCommit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		f := strings.Split(rec, "\x1f")
		if len(f) < 7 {
			continue
		}
		c := GraphCommit{Hash: f[0], FullHash: f[1], Parents: strings.Fields(f[2]), Subject: f[3], Author: f[4]}
		c.Committed, _ = strconv.ParseInt(f[5], 10, 64)
		c.IsMerge = len(c.Parents) > 1
		c.Refs, c.IsHead = parseDecorations(f[6])
		cs = append(cs, c)
	}
	more := len(cs) > limit
	if more {
		cs = cs[:limit]
	}
	if up := unpushed(dir); len(up) > 0 {
		for i := range cs {
			cs[i].Unpushed = up[cs[i].FullHash]
		}
	}
	return cs, more, nil
}

// hasCommits says whether HEAD points at a commit.
func hasCommits(dir string) bool {
	_, err := runOut(dir, "git", "rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

// unpushed is the set of full hashes on HEAD but not on its upstream
// (empty without an upstream).
func unpushed(dir string) map[string]bool {
	out, err := runOut(dir, "git", "rev-list", "-n500", "@{upstream}..HEAD")
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, h := range strings.Fields(out) {
		set[h] = true
	}
	return set
}

// parseDecorations reads a --decorate=full %D list ("HEAD -> refs/heads/
// main, tag: refs/tags/v1, refs/remotes/origin/main").
func parseDecorations(d string) ([]Ref, bool) {
	var local, tags, remote []Ref
	head := false
	for _, p := range strings.Split(d, ", ") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == "HEAD" {
			head = true
			continue
		}
		if rest, ok := strings.CutPrefix(p, "HEAD -> "); ok {
			head, p = true, rest
		}
		p = strings.TrimPrefix(p, "tag: ")
		switch {
		case strings.HasPrefix(p, "refs/heads/"):
			local = append(local, Ref{Name: strings.TrimPrefix(p, "refs/heads/"), Kind: RefLocal})
		case strings.HasPrefix(p, "refs/tags/"):
			tags = append(tags, Ref{Name: strings.TrimPrefix(p, "refs/tags/"), Kind: RefTag})
		case strings.HasPrefix(p, "refs/remotes/"):
			name := strings.TrimPrefix(p, "refs/remotes/")
			if strings.HasSuffix(name, "/HEAD") {
				continue // origin/HEAD only repeats origin/main
			}
			remote = append(remote, Ref{Name: name, Kind: RefRemote})
		}
	}
	return append(append(local, tags...), remote...), head
}

// GroupLabel names the GRAPH group a commit made at epoch falls in:
// Today, Yesterday, This week (the 7 days up to today), then the month
// ("October"; "December 2025" for another year). Uses clock.Now.
func GroupLabel(epoch int64) string {
	now := clock.Now()
	t := time.Unix(epoch, 0).In(now.Location())
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, x.Location()) }
	days := int(day(now).Sub(day(t)).Hours()/24 + 0.5)
	switch {
	case days <= 0:
		return "Today"
	case days == 1:
		return "Yesterday"
	case days < 7:
		return "This week"
	case t.Year() == now.Year():
		return t.Month().String()
	}
	return fmt.Sprintf("%s %d", t.Month(), t.Year())
}

// Initials are two letters for an author name: the first letters of the
// first and last words, or the first two of a single word. A wide (CJK)
// first letter stands alone. No name gives "??".
func Initials(name string) string {
	w := strings.Fields(name)
	if len(w) == 0 {
		return "??"
	}
	first := []rune(w[0])
	if runewidth.RuneWidth(first[0]) > 1 {
		return string(first[0])
	}
	var out []rune
	if len(w) > 1 {
		out = []rune{first[0], []rune(w[len(w)-1])[0]}
	} else {
		out = first[:min(2, len(first))]
	}
	for i := range out {
		out[i] = unicode.ToUpper(out[i])
	}
	return string(out)
}

// CompactAge is a commit time as a 2–3 cell badge ("now", "5m", "2h",
// "3d", "2w", "4mo", "1y"), against clock.Now.
func CompactAge(epoch int64) string {
	if epoch >= clock.Now().Unix() {
		return "now" // clock skew: a commit "from the future"
	}
	return compactAge(relativeDate(strconv.FormatInt(epoch, 10)))
}

// CommitMessage is a commit's whole message (subject, blank line, body).
func CommitMessage(dir, hash string) (string, error) {
	out, err := runOut(dir, "git", "log", "-1", "--format=%B", hash, "--")
	return strings.TrimRight(out, "\n"), err
}
