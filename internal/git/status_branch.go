package git

import (
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// BranchStatus is the result of StatusBranch.
type BranchStatus struct {
	Branch Branch
	Files  []FileStatus
	// OID is the full hash of HEAD; empty before the first commit.
	OID string
}

// StatusBranch returns the branch, the ahead/behind counts and the
// changed files with one `git status --porcelain=v2 --branch -z`. It
// gives the same answer as GetBranch + Status, which start three or
// four git processes; the background poll runs it every 2 s.
func StatusBranch(dir string) (BranchStatus, error) {
	cmd := spawn.Command("git", "status", "--porcelain=v2", "--branch", "-z")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return BranchStatus{}, err
	}
	bs := parseStatusV2(string(out))
	if strings.HasPrefix(bs.Branch.Name, "(detached ") {
		// The short hash grows with the repo (core.abbrev=auto); ask git.
		if short, err := runOut(dir, "git", "rev-parse", "--short", "HEAD"); err == nil {
			bs.Branch.Name = "(detached " + strings.TrimSpace(short) + ")"
		}
	}
	return bs, nil
}

// parseStatusV2 parses `git status --porcelain=v2 --branch -z` output.
// Codes use the v1 letters ('.' becomes ' ') so callers see FileStatus
// values just like Status gives.
func parseStatusV2(out string) BranchStatus {
	var bs BranchStatus
	var head string
	recs := strings.Split(out, "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if len(rec) < 2 {
			continue
		}
		switch rec[0] {
		case '#':
			key, val, _ := strings.Cut(rec[2:], " ")
			switch key {
			case "branch.oid":
				if val != "(initial)" {
					bs.OID = val
				}
			case "branch.head":
				head = val
			case "branch.ab":
				a, b, _ := strings.Cut(val, " ")
				bs.Branch.Ahead, _ = strconv.Atoi(strings.TrimPrefix(a, "+"))
				bs.Branch.Behind, _ = strconv.Atoi(strings.TrimPrefix(b, "-"))
			}
		case '?':
			bs.Files = append(bs.Files, FileStatus{Code: "??", Path: rec[2:]})
		case '1', '2', 'u':
			// "1 XY sub mH mI mW hH hI path", "2 … Xscore path" (then
			// the old path as the next record), "u … h1 h2 h3 path".
			n := map[byte]int{'1': 8, '2': 9, 'u': 10}[rec[0]]
			f := strings.SplitN(rec, " ", n+1)
			if len(f) <= n {
				continue
			}
			code := strings.ReplaceAll(f[1], ".", " ")
			bs.Files = append(bs.Files, FileStatus{Code: code, Path: f[n]})
			if rec[0] == '2' {
				i++ // the old path
			}
		}
	}
	switch {
	case head != "(detached)":
		bs.Branch.Name = head
	case bs.OID != "":
		bs.Branch.Name = "(detached " + shortOID(bs.OID) + ")"
	}
	return bs
}

// shortOID abbreviates a hash; StatusBranch replaces it with git's own
// short form.
func shortOID(oid string) string {
	if len(oid) > 7 {
		return oid[:7]
	}
	return oid
}
