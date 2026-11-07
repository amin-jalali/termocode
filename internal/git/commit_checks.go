package git

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// The reads behind the before-commit checks (GITX-27): what is staged,
// how big each staged file is, which lines the commit adds, and whether
// the repository has commit hooks.

// StagedFile is one file the next commit adds or changes.
type StagedFile struct {
	Path string // relative to the repository root, forward slashes
	Size int64  // size of the staged blob in bytes
}

// StagedFiles lists the added, copied, changed and renamed files of the
// index with the size of their staged version. Deleted files are left
// out: they add nothing.
func StagedFiles(dir string) ([]StagedFile, error) {
	paths, err := nulList(dir, "diff", "--cached", "--name-only", "-z", "--diff-filter=ACMR")
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	var in strings.Builder
	var keep []string
	for _, p := range paths {
		if strings.ContainsAny(p, "\n\r") {
			continue // cat-file reads one name per line
		}
		keep = append(keep, p)
		in.WriteString(":" + p + "\n")
	}
	cmd := spawn.Command("git", "cat-file", "--batch-check=%(objectsize)")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.Output()
	if err != nil {
		return nil, cmdError(err, out)
	}
	sizes := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	files := make([]StagedFile, 0, len(keep))
	for i, p := range keep {
		f := StagedFile{Path: p}
		if i < len(sizes) {
			f.Size, _ = strconv.ParseInt(strings.TrimSpace(sizes[i]), 10, 64)
		}
		files = append(files, f)
	}
	return files, nil
}

// AddedLine is one line the next commit adds.
type AddedLine struct {
	Path string
	Line int // 1-based line in the staged file
	Text string
}

// StagedAddedLines lists the lines the staged diff adds (binary files
// have none).
func StagedAddedLines(dir string) ([]AddedLine, error) {
	cmd := spawn.Command("git", "-c", "core.quotePath=false", "diff", "--cached", "-U0", "--no-color", "--no-ext-diff", "--diff-filter=ACMR")
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, cmdError(err, []byte(stderr.String()))
	}
	var lines []AddedLine
	path, next := "", 0
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "diff --git "):
			path = ""
		case strings.HasPrefix(l, "+++ "):
			p := strings.TrimSuffix(strings.TrimPrefix(l, "+++ "), "\t") // git ends a name with a space with a tab
			if p == "/dev/null" {
				path = ""
			} else {
				path = strings.TrimPrefix(unquotePath(p), "b/")
			}
		case strings.HasPrefix(l, "--- "):
		case strings.HasPrefix(l, "@@ "):
			next = hunkNewStart(l)
		case strings.HasPrefix(l, "+") && path != "":
			lines = append(lines, AddedLine{Path: path, Line: next, Text: l[1:]})
			next++
		case strings.HasPrefix(l, " "):
			next++
		}
	}
	return lines, sc.Err()
}

// hunkNewStart reads c from "@@ -a,b +c,d @@".
func hunkNewStart(l string) int {
	i := strings.Index(l, " +")
	if i < 0 {
		return 1
	}
	s := l[i+2:]
	if j := strings.IndexAny(s, ", "); j >= 0 {
		s = s[:j]
	}
	n, _ := strconv.Atoi(s)
	return n
}

// unquotePath undoes git's "quoted" path form.
func unquotePath(p string) string {
	if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
		if u, err := strconv.Unquote(p); err == nil {
			return u
		}
	}
	return p
}

// UnstagedPaths lists the tracked files with changes that are not staged.
func UnstagedPaths(dir string) ([]string, error) {
	return nulList(dir, "diff", "--name-only", "-z")
}

// nulList runs git and splits its NUL-separated output.
func nulList(dir string, args ...string) ([]string, error) {
	out, err := runCombined(dir, "git", args...)
	if err != nil {
		return nil, err
	}
	var list []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			list = append(list, p)
		}
	}
	return list, nil
}

// commitHookNames are the hooks a plain `git commit` runs.
var commitHookNames = []string{"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit"}

// HasCommitHooks reports whether a commit runs a hook here (core.hooksPath
// is honoured; the .sample files git ships are not hooks).
func HasCommitHooks(dir string) bool {
	out, err := runOut(dir, "git", "rev-parse", "--git-path", "hooks")
	if err != nil {
		return false
	}
	hooks := strings.TrimSpace(out)
	if hooks == "" {
		return false
	}
	if !filepath.IsAbs(hooks) {
		hooks = filepath.Join(dir, hooks)
	}
	for _, n := range commitHookNames {
		if st, err := os.Stat(filepath.Join(hooks, n)); err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}

// CommitOptions are the switches of CommitOpts.
type CommitOptions struct {
	Amend, Signoff bool
	NoVerify       bool   // skip the pre-commit and commit-msg hooks
	AllowEmpty     bool   // a commit without changes (--allow-empty)
	Author         string // "Name <e-mail>" from ParseAuthor (--author); "" = the user
}

// CommitOpts commits the index. An empty message is an error unless
// Amend keeps the old one.
func CommitOpts(dir, message string, o CommitOptions) error {
	args, err := commitArgs(message, o)
	if err != nil {
		return err
	}
	return commitRun(dir, args)
}

// commitArgs is the git commit command line for message and o.
func commitArgs(message string, o CommitOptions) ([]string, error) {
	args := []string{"commit"}
	if o.Amend {
		args = append(args, "--amend")
	}
	if o.Signoff {
		args = append(args, "--signoff")
	}
	if o.NoVerify {
		args = append(args, "--no-verify")
	}
	if o.AllowEmpty {
		args = append(args, "--allow-empty")
	}
	if o.Author != "" {
		a, err := ParseAuthor(o.Author)
		if err != nil {
			return nil, err
		}
		args = append(args, "--author="+a)
	}
	if strings.TrimSpace(message) == "" {
		if !o.Amend {
			return nil, errEmptyMessage
		}
		args = append(args, "--no-edit")
	} else {
		args = append(args, "-m", message)
	}
	return args, nil
}

func commitRun(dir string, args []string) error {
	if _, ok, err := remoteRun(dir, args...); ok {
		return err
	}
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return cmdError(err, out)
	}
	return nil
}
