package git

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Bisect (GITX-19) and branch compare helpers (GITX-24).

// BisectState is where a bisect is.
type BisectState struct {
	Active          bool
	Current         string // short hash of the commit under test
	CurrentSubject  string
	StepsLeft       int // roughly; -1 when git did not say
	Done            bool
	FirstBad        string // full hash, when Done
	FirstBadSubject string
}

func noOption(what, v string) error {
	if strings.TrimSpace(v) == "" || strings.HasPrefix(v, "-") {
		return fmt.Errorf("%s %q is not valid", what, v)
	}
	return nil
}

var (
	bisectStepsRe = regexp.MustCompile(`roughly (\d+) steps?`)
	bisectFirstRe = regexp.MustCompile(`(?m)^([0-9a-f]{40}) is the first bad commit`)
)

// bisectRead fills the state from git's output and HEAD.
func bisectRead(dir, out string) BisectState {
	st := BisectStatus(dir)
	st.StepsLeft = -1
	if m := bisectStepsRe.FindStringSubmatch(out); m != nil {
		st.StepsLeft, _ = strconv.Atoi(m[1])
	}
	if m := bisectFirstRe.FindStringSubmatch(out); m != nil {
		st.Done, st.FirstBad = true, m[1]
		if s, err := runOut(dir, "git", "log", "-1", "--format=%s", m[1], "--"); err == nil {
			st.FirstBadSubject = strings.TrimSpace(s)
		}
	}
	return st
}

// BisectStatus says whether a bisect runs here and which commit is
// checked out for it.
func BisectStatus(dir string) BisectState {
	p, err := runOut(dir, "git", "rev-parse", "--git-path", "BISECT_START")
	if err != nil {
		return BisectState{}
	}
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") {
		p = dir + "/" + p
	}
	if _, err := os.Stat(p); err != nil {
		return BisectState{}
	}
	st := BisectState{Active: true, StepsLeft: -1}
	if s, err := runOut(dir, "git", "log", "-1", "--format=%h%x1f%s", "HEAD", "--"); err == nil {
		st.Current, st.CurrentSubject, _ = strings.Cut(strings.TrimSpace(s), "\x1f")
	}
	return st
}

func bisectCmd(dir string, args ...string) (string, error) {
	cmd := spawn.Command("git", append([]string{"bisect"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), cmdError(err, out)
	}
	return string(out), nil
}

// BisectStart starts a bisect between a bad and a good commit; git checks
// out a commit in the middle.
func BisectStart(dir, bad, good string) (BisectState, error) {
	if err := noOption("commit", bad); err != nil {
		return BisectState{}, err
	}
	if err := noOption("commit", good); err != nil {
		return BisectState{}, err
	}
	out, err := bisectCmd(dir, "start", bad, good, "--")
	if err != nil {
		_, _ = bisectCmd(dir, "reset")
		return BisectState{}, err
	}
	return bisectRead(dir, out), nil
}

// BisectMark answers for the commit under test: good, bad or skip.
func BisectMark(dir, verdict string) (BisectState, error) {
	switch verdict {
	case "good", "bad", "skip":
	default:
		return BisectState{}, fmt.Errorf("%q is not good, bad or skip", verdict)
	}
	out, err := bisectCmd(dir, verdict)
	if err != nil {
		return BisectStatus(dir), err
	}
	return bisectRead(dir, out), nil
}

// BisectRun lets a shell command decide each commit (exit 0 good, 125
// skip, else bad), run in cwd. The bisect stays open on the result.
func BisectRun(dir, cwd, command string) (BisectState, error) {
	if strings.TrimSpace(command) == "" {
		return BisectState{}, errors.New("no command")
	}
	out, err := bisectCmd(dir, "run", "sh", "-c", `cd "$1" && eval "$2"`, "sh", cwd, command)
	st := bisectRead(dir, out)
	if err != nil && !st.Done {
		return st, err
	}
	return st, nil
}

// BisectReset ends the bisect and checks out where it started.
func BisectReset(dir string) error {
	_, err := bisectCmd(dir, "reset")
	return err
}

// RefNames lists local branches, remote branches and tags, sorted.
func RefNames(dir string) ([]string, error) {
	out, err := runCombined(dir, "git", "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasSuffix(l, "/HEAD") {
			names = append(names, l)
		}
	}
	sort.Strings(names)
	return names, nil
}

// MergeBase is the full hash of the commit where a and b split.
func MergeBase(dir, a, b string) (string, error) {
	if err := noOption("ref", a); err != nil {
		return "", err
	}
	if err := noOption("ref", b); err != nil {
		return "", err
	}
	out, err := runCombined(dir, "git", "merge-base", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// WorkTreeDiffFiles lists the tracked files that differ between the
// working tree and ref, sorted.
func WorkTreeDiffFiles(dir, ref string) ([]string, error) {
	if err := noOption("ref", ref); err != nil {
		return nil, err
	}
	out, err := runCombined(dir, "git", "diff", "--name-only", "-z", ref, "--")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	return files, nil
}

// CommitOf is the full hash of a commit, branch or tag name.
func CommitOf(dir, ref string) (string, error) {
	if err := noOption("ref", ref); err != nil {
		return "", err
	}
	out, err := runOut(dir, "git", "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil || strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("no commit %s", ref)
	}
	return strings.TrimSpace(out), nil
}
