package git

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// CommitFile is one file a commit changed.
type CommitFile struct {
	Path, OldPath string // OldPath: the name before a rename
	Status        byte   // 'A', 'M', 'D', 'R' (copies show as 'A', type changes as 'M')
	Add, Del      int
	Binary        bool
}

// CommitInfo is a commit for the commit view: meta (no e-mail), parents,
// refs and the changed files with their counts. CommitDetail.Files / Add
// / Del hold the totals.
type CommitInfo struct {
	CommitDetail
	FullHash string
	Parents  []string // short hashes
	Refs     []Ref
	Files    []CommitFile
	Commits  int // CommitFilesBetween: commits in the range
}

// CommitFiles loads a commit's meta and changed files in one git call. A
// merge is diffed against its first parent (as the hover card counts it).
func CommitFiles(dir, hash string) (CommitInfo, error) {
	const format = "--format=%h%x1f%H%x1f%p%x1f%an%x1f%ad%x1f%at%x1f%D%x1f%s%x1f%b%x1e"
	out, err := runOut(dir, "git", "show", "--no-color", "--no-ext-diff", "--decorate=full",
		"--date=format-local:%Y-%m-%d %H:%M", format, "--raw", "--numstat", "-z", "-M",
		"--first-parent", "-m", hash, "--")
	if err != nil {
		return CommitInfo{}, fmt.Errorf("git show %s: %w", hash, err)
	}
	meta, tail, _ := strings.Cut(out, "\x1e")
	var c CommitInfo
	f := strings.Split(meta, "\x1f")
	if len(f) < 9 {
		return CommitInfo{}, fmt.Errorf("git show %s: unexpected output", hash)
	}
	c.Hash, c.FullHash, c.Parents, c.Author = f[0], f[1], strings.Fields(f[2]), f[3]
	c.DateAbs, c.DateRel = f[4], relativeDate(f[5])
	c.Refs, _ = parseDecorations(f[6])
	c.Subject, c.Body = f[7], strings.TrimSpace(f[8])
	c.Files = parseRawNumstat(tail)
	c.CommitDetail.Files = len(c.Files)
	for _, cf := range c.Files {
		c.Add += cf.Add
		c.Del += cf.Del
	}
	return c, nil
}

// CommitFilesBetween lists the files that changed from a to b (`git diff
// a b`) with their counts; Commits is the number of commits in a..b.
func CommitFilesBetween(dir, a, b string) (CommitInfo, error) {
	out, err := runOut(dir, "git", "diff", "--no-color", "--no-ext-diff", "--raw", "--numstat", "-z", "-M", a, b, "--")
	if err != nil {
		return CommitInfo{}, fmt.Errorf("git diff %s %s: %w", a, b, err)
	}
	var c CommitInfo
	c.Files = parseRawNumstat(out)
	c.CommitDetail.Files = len(c.Files)
	for _, cf := range c.Files {
		c.Add += cf.Add
		c.Del += cf.Del
	}
	if n, err := runOut(dir, "git", "rev-list", "--count", a+".."+b, "--"); err == nil {
		c.Commits, _ = strconv.Atoi(strings.TrimSpace(n))
	}
	return c, nil
}

// parseRawNumstat reads `--raw --numstat -z` output: raw records
// (":modes shas STATUS\0path\0[new\0]") then numstat records
// ("add\tdel\tpath\0" or, for a rename, "add\tdel\t\0old\0new\0").
func parseRawNumstat(s string) []CommitFile {
	tok := strings.Split(s, "\x00")
	var files []CommitFile
	idx := map[string]int{}
	for i := 0; i < len(tok); i++ {
		t := strings.TrimLeft(tok[i], "\n")
		switch {
		case t == "":
			continue
		case strings.HasPrefix(t, ":"):
			fs := strings.Fields(t)
			if len(fs) < 5 || i+1 >= len(tok) {
				continue
			}
			st := fs[len(fs)-1]
			cf := CommitFile{Status: st[0], Path: tok[i+1]}
			i++
			if (st[0] == 'R' || st[0] == 'C') && i+1 < len(tok) {
				cf.OldPath, cf.Path = cf.Path, tok[i+1]
				i++
			}
			switch cf.Status {
			case 'C':
				cf.Status, cf.OldPath = 'A', ""
			case 'T':
				cf.Status = 'M'
			}
			if _, seen := idx[cf.Path]; seen {
				continue
			}
			idx[cf.Path] = len(files)
			files = append(files, cf)
		default:
			parts := strings.SplitN(t, "\t", 3)
			if len(parts) < 3 {
				continue
			}
			p := parts[2]
			if p == "" && i+2 < len(tok) { // rename: old, new follow
				p = tok[i+2]
				i += 2
			}
			k, ok := idx[p]
			if !ok {
				continue
			}
			if parts[0] == "-" {
				files[k].Binary = true
				continue
			}
			files[k].Add, _ = strconv.Atoi(parts[0])
			files[k].Del, _ = strconv.Atoi(parts[1])
		}
	}
	return files
}

// isMerge says whether hash has more than one parent.
func isMerge(dir, hash string) bool {
	out, err := runOut(dir, "git", "rev-list", "--parents", "-n1", hash, "--")
	return err == nil && len(strings.Fields(out)) > 2
}

// Revert makes a commit that undoes hash (`git revert --no-edit`; a merge
// is reverted against its first parent). On a conflict it returns the
// conflicted paths with git's error.
func Revert(dir, hash string) ([]string, error) {
	return pickLike(dir, "revert", hash)
}

// CherryPick applies hash on top of HEAD (`git cherry-pick`). On a
// conflict it returns the conflicted paths with git's error.
func CherryPick(dir, hash string) ([]string, error) {
	return pickLike(dir, "cherry-pick", hash)
}

func pickLike(dir, verb, hash string) ([]string, error) {
	args := []string{verb, "--no-edit"}
	if isMerge(dir, hash) {
		args = append(args, "-m", "1")
	}
	cmd := exec.Command("git", append(args, hash)...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil, nil
	}
	msg := strings.TrimSpace(string(out))
	var conflicts []string
	if u, e := runOut(dir, "git", "diff", "--name-only", "--diff-filter=U"); e == nil {
		conflicts = strings.Fields(u)
	}
	if len(conflicts) > 0 {
		return conflicts, fmt.Errorf("%s stopped: %d conflict(s)", verb, len(conflicts))
	}
	if msg == "" {
		msg = err.Error()
	}
	return nil, fmt.Errorf("%s", msg)
}
