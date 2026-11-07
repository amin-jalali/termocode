package git

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
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

// IsShallow reports a shallow repository (a clone with --depth).
func IsShallow(dir string) bool {
	out, err := runOut(dir, "git", "rev-parse", "--is-shallow-repository")
	return err == nil && strings.TrimSpace(out) == "true"
}

// Unshallow fetches the rest of the history of a shallow repository.
func Unshallow(dir string) error {
	if out, err := remoteCmd(dir, "fetch", "--unshallow").CombinedOutput(); err != nil {
		return cmdError(err, out)
	}
	return nil
}

// StagedCRLF names the staged files whose added lines end in CRLF while
// the file at HEAD has no CRLF (or is new): Windows line endings that the
// commit would bring in. Binary files are left out (git shows no lines).
func StagedCRLF(dir string) ([]string, error) {
	out, err := runCombined(dir, "git", "-c", "core.quotePath=false", "diff", "--cached", "-U0", "--no-color", "--no-ext-diff", "--diff-filter=ACMR")
	if err != nil {
		return nil, err
	}
	var paths []string
	seen := map[string]bool{}
	path := ""
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			path = ""
		case strings.HasPrefix(l, "+++ "):
			p := strings.TrimSuffix(strings.TrimPrefix(l, "+++ "), "\t")
			path = ""
			if p != "/dev/null" {
				path = strings.TrimPrefix(unquotePath(p), "b/")
			}
		case strings.HasPrefix(l, "+") && path != "" && strings.HasSuffix(l, "\r") && !seen[path]:
			seen[path] = true
			if old, err := runOut(dir, "git", "cat-file", "-p", "HEAD:"+path); err == nil && strings.Contains(old, "\r\n") {
				continue
			}
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// lfsPointerMax is the size above which a file is never an LFS pointer.
const lfsPointerMax = 1024

// ParseLFSPointer reads a Git LFS pointer file: its oid and the size of
// the real file.
func ParseLFSPointer(data []byte) (oid string, size int64, ok bool) {
	if len(data) > lfsPointerMax || !bytes.HasPrefix(data, []byte("version https://git-lfs.github.com/spec/")) {
		return "", 0, false
	}
	hasSize := false
	for _, l := range strings.Split(string(data), "\n") {
		k, v, _ := strings.Cut(l, " ")
		switch k {
		case "oid":
			oid = v
		case "size":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				return "", 0, false
			}
			size, hasSize = n, true
		}
	}
	if oid == "" || !hasSize {
		return "", 0, false
	}
	return oid, size, true
}

// LFSFile is a tracked file that .gitattributes sends to Git LFS.
type LFSFile struct {
	Path    string
	Pointer bool  // only the pointer is on disk: the real file is not downloaded
	Size    int64 // of the real file (from the pointer), else on disk
}

// LFSFiles lists the tracked files with the filter=lfs attribute, in
// path order.
func LFSFiles(dir string) ([]LFSFile, error) {
	ls, err := runCombined(dir, "git", "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	if ls == "" {
		return nil, nil
	}
	cmd := spawn.Command("git", "check-attr", "-z", "--stdin", "filter")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(ls)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, cmdError(err, []byte(stderr.String()))
	}
	f := strings.Split(string(out), "\x00")
	var files []LFSFile
	for i := 0; i+2 < len(f); i += 3 {
		if f[i+2] != "lfs" {
			continue
		}
		lf := LFSFile{Path: f[i]}
		p := filepath.Join(dir, filepath.FromSlash(f[i]))
		if st, err := os.Stat(p); err == nil {
			lf.Size = st.Size()
			if st.Size() <= lfsPointerMax {
				if b, err := os.ReadFile(p); err == nil {
					if _, n, ok := ParseLFSPointer(b); ok {
						lf.Pointer, lf.Size = true, n
					}
				}
			}
		}
		files = append(files, lf)
	}
	sort.Slice(files, func(a, b int) bool { return files[a].Path < files[b].Path })
	return files, nil
}

// LatestTag is the newest tag reachable from HEAD ("" when none).
func LatestTag(dir string) string {
	out, err := runOut(dir, "git", "describe", "--tags", "--abbrev=0")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// SubjectsSince is the subjects of the commits on HEAD after from (all
// of them when from is ""), newest first, merges left out, at most max.
func SubjectsSince(dir, from string, max int) ([]string, error) {
	rng := "HEAD"
	if strings.TrimSpace(from) != "" {
		full, err := ResolveCommit(dir, from)
		if err != nil {
			return nil, err
		}
		rng = full + "..HEAD"
	}
	out, err := runCombined(dir, "git", "log", "--no-merges", "--format=%s", "-n", strconv.Itoa(max), rng, "--")
	if err != nil {
		return nil, err
	}
	var subjects []string
	for _, s := range strings.Split(strings.TrimSpace(out), "\n") {
		if s = strings.TrimSpace(s); s != "" {
			subjects = append(subjects, s)
		}
	}
	return subjects, nil
}
