package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// FileStatus represents one entry from `git status --porcelain=v1`.
type FileStatus struct {
	// Two-character status code: index status + worktree status.
	// Common values: "M ", " M", "MM", "A ", " A", "??", "D ", " D", "R "
	Code string
	// Path (post-rename if renamed).
	Path string
}

// Staged returns true if the index column is non-empty/non-?.
func (s FileStatus) Staged() bool {
	if s.Code == "" {
		return false
	}
	c := s.Code[0]
	return c != ' ' && c != '?'
}

// Unstaged returns true if the worktree column is non-empty/non-?.
func (s FileStatus) Unstaged() bool {
	if len(s.Code) < 2 {
		return false
	}
	c := s.Code[1]
	return c != ' ' && c != '?'
}

// Untracked returns true if the file is untracked (??).
func (s FileStatus) Untracked() bool {
	return s.Code == "??"
}

// Branch describes the current branch and its position vs upstream.
type Branch struct {
	Name   string
	Ahead  int
	Behind int
}

// IsRepo reports whether dir is inside a git work tree.
func IsRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// Status runs `git status --porcelain=v1` in dir and returns parsed entries.
func Status(dir string) ([]FileStatus, error) {
	cmd := exec.Command("git", "status", "--porcelain=v1")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []FileStatus
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		code := line[:2]
		path := line[3:]
		// Rename: "old -> new" — keep new name.
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		files = append(files, FileStatus{Code: code, Path: path})
	}
	return files, nil
}

// GetBranch returns current branch name and ahead/behind counts vs upstream.
func GetBranch(dir string) (Branch, error) {
	name, err := runOut(dir, "git", "branch", "--show-current")
	if err != nil {
		return Branch{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		// detached head
		out, _ := runOut(dir, "git", "rev-parse", "--short", "HEAD")
		name = strings.TrimSpace(out)
		if name != "" {
			name = "(detached " + name + ")"
		}
	}

	var ahead, behind int
	out, err := runOut(dir, "git", "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	if err == nil {
		_, _ = fmt.Sscanf(strings.TrimSpace(out), "%d\t%d", &behind, &ahead)
	}

	return Branch{Name: name, Ahead: ahead, Behind: behind}, nil
}

func runOut(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

// Stage adds path to the index (`git add -- path`). For untracked files this
// stages the new file; for modified files it stages the working-tree diff.
func Stage(dir, path string) error {
	cmd := exec.Command("git", "add", "--", path)
	cmd.Dir = dir
	return cmd.Run()
}

// Unstage removes path from the index without touching the working tree
// (`git restore --staged -- path`).
func Unstage(dir, path string) error {
	cmd := exec.Command("git", "restore", "--staged", "--", path)
	cmd.Dir = dir
	return cmd.Run()
}

// Discard reverts the working-tree changes for path back to the index/HEAD.
// For untracked files it removes them outright; for tracked files it runs
// `git restore -- path`. Either way the file ends up matching the last
// committed state (or the index, if there are staged changes).
func Discard(dir, path string) error {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(dir, path)
	}
	// Untracked files aren't known to git restore — delete them directly.
	statusOut, _ := runOut(dir, "git", "status", "--porcelain=v1", "--", path)
	if strings.HasPrefix(strings.TrimLeft(statusOut, " "), "??") {
		return os.RemoveAll(abs)
	}
	cmd := exec.Command("git", "restore", "--", path)
	cmd.Dir = dir
	return cmd.Run()
}

// Commit creates a commit with the given message. Empty messages are rejected
// up front so we don't surface git's less-helpful "Aborting commit due to
// empty commit message" error to the user.
func Commit(dir, message string) error {
	return CommitWith(dir, message, false, false)
}

// CommitWith creates a commit with optional --amend / --signoff. When amend is
// true and the message is empty, the existing commit message is kept
// (--amend --no-edit); otherwise an empty message is rejected. signoff adds the
// "Signed-off-by:" trailer (git commit -s).
func CommitWith(dir, message string, amend, signoff bool) error {
	args := []string{"commit"}
	if amend {
		args = append(args, "--amend")
	}
	if signoff {
		args = append(args, "--signoff")
	}
	if strings.TrimSpace(message) == "" {
		if amend {
			// Reword nothing: keep the previous message verbatim.
			args = append(args, "--no-edit")
		} else {
			return fmt.Errorf("commit message is empty")
		}
	} else {
		args = append(args, "-m", message)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// BlameLine summarises `git blame` for a single line: who last touched it
// and when. Hash is the abbreviated commit SHA; "0000000" means "uncommitted
// (working tree)" — surface that as "you, just now" in the UI.
type BlameLine struct {
	Hash    string
	Author  string
	When    string // human-friendly relative time
	Subject string // commit message subject line
}

// BlameLineAt runs `git blame` for the single line `lineNo` (1-based) of
// `path` and returns the parsed result. Errors when the file isn't tracked.
func BlameLineAt(dir, path string, lineNo int) (BlameLine, error) {
	out, err := runOut(dir, "git", "blame",
		"-L", fmt.Sprintf("%d,%d", lineNo, lineNo),
		"--porcelain", "--", path)
	if err != nil {
		return BlameLine{}, err
	}
	return parseBlamePorcelain(out), nil
}

// parseBlamePorcelain extracts hash/author/when/subject from a single-line
// git blame --porcelain block. The format is well-defined:
//
//	<hash> <orig-line> <final-line> <line-count>
//	author <name>
//	author-time <epoch>
//	summary <subject>
//	... (more headers)
//	\t<line content>
func parseBlamePorcelain(s string) BlameLine {
	var bl BlameLine
	for _, line := range strings.Split(s, "\n") {
		switch {
		case bl.Hash == "" && len(line) > 0 && (line[0] >= '0' && line[0] <= '9' || line[0] >= 'a' && line[0] <= 'f'):
			parts := strings.SplitN(line, " ", 2)
			if len(parts) > 0 && len(parts[0]) >= 7 {
				bl.Hash = parts[0][:7]
			}
		case strings.HasPrefix(line, "author "):
			bl.Author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			var epoch int64
			fmt.Sscanf(strings.TrimPrefix(line, "author-time "), "%d", &epoch)
			bl.When = relativeTimeFromEpoch(epoch)
		case strings.HasPrefix(line, "summary "):
			bl.Subject = strings.TrimPrefix(line, "summary ")
		}
	}
	if bl.Hash == "0000000" || strings.HasPrefix(bl.Hash, "00000") {
		bl.Hash = "uncommitted"
		bl.Author = "you"
		bl.When = "just now"
		bl.Subject = "(working tree)"
	}
	return bl
}

// relativeTimeFromEpoch is a small helper turning a unix timestamp into a
// rough relative-time string ("3 days ago", "5 minutes ago", ...). Avoids
// pulling in a heavyweight dep — git's own --human-time relies on the
// system locale and is harder to parse stably.
func relativeTimeFromEpoch(epoch int64) string {
	if epoch <= 0 {
		return ""
	}
	now := time.Now().Unix()
	diff := now - epoch
	switch {
	case diff < 0:
		return "just now"
	case diff < 60:
		return "just now"
	case diff < 3600:
		return fmt.Sprintf("%d min ago", diff/60)
	case diff < 86400:
		return fmt.Sprintf("%d hr ago", diff/3600)
	case diff < 30*86400:
		return fmt.Sprintf("%d days ago", diff/86400)
	case diff < 365*86400:
		return fmt.Sprintf("%d months ago", diff/(30*86400))
	}
	return fmt.Sprintf("%d years ago", diff/(365*86400))
}

// FileHistory returns the commit list that touched the given path, newest
// first. Same field structure as Log() so the UI code can reuse the picker.
func FileHistory(dir, path string, limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	fmtArg := "--pretty=format:%h%x1f%s%x1f%an%x1f%ar%x1f%d%x1e"
	out, err := runOut(dir, "git", "log", fmtArg, fmt.Sprintf("-n%d", limit), "--follow", "--", path)
	if err != nil {
		return nil, err
	}
	var entries []LogEntry
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "\x1f")
		if len(fields) < 5 {
			continue
		}
		entries = append(entries, LogEntry{
			Hash:    fields[0],
			Subject: fields[1],
			Author:  fields[2],
			When:    fields[3],
			Refs:    strings.TrimSpace(fields[4]),
		})
	}
	return entries, nil
}

// Tags lists every tag in the repo, newest first (sorted by commit date).
func Tags(dir string) ([]string, error) {
	out, err := runOut(dir, "git", "tag", "--list", "--sort=-creatordate")
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		tags = append(tags, line)
	}
	return tags, nil
}

// LogEntry is one row of git log output, parsed from a custom format.
type LogEntry struct {
	Hash    string // short SHA
	Subject string
	Author  string
	When    string // human-friendly time ("3 days ago")
	Refs    string // decorations, e.g. "HEAD -> main, origin/main"
}

// Log returns the most recent N commits reachable from HEAD. Limit is the
// max number of entries; pass <= 0 for the default (200).
func Log(dir string, limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	// We use unit separators (\x1f) between fields and record separators
	// (\x1e) between records so commit messages with arbitrary punctuation
	// don't confuse the parser.
	fmtArg := "--pretty=format:%h%x1f%s%x1f%an%x1f%ar%x1f%d%x1e"
	out, err := runOut(dir, "git", "log", fmtArg, fmt.Sprintf("-n%d", limit))
	if err != nil {
		return nil, err
	}
	var entries []LogEntry
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "\x1f")
		if len(fields) < 5 {
			continue
		}
		entries = append(entries, LogEntry{
			Hash:    fields[0],
			Subject: fields[1],
			Author:  fields[2],
			When:    fields[3],
			Refs:    strings.TrimSpace(fields[4]),
		})
	}
	return entries, nil
}

// GraphLine is one line of `git log --graph` output. A commit node has Hash
// set (plus Subject/Refs/…); a pure topology connector line ("| |", "|\", "|/")
// has Hash empty and only Art populated.
type GraphLine struct {
	Art     string // graph art prefix, e.g. "* ", "| * ", "|\\ "
	Hash    string // short SHA; "" on connector lines
	Subject string
	Refs    string // decorations, e.g. "HEAD -> main"
	Age     string // compact relative committer date, e.g. "2h", "3d", "now"
	IsMerge bool   // commit has 2+ parents
	IsHead  bool   // commit is (or is pointed at by) HEAD
}

// GraphLog returns `git log --graph` for HEAD (current branch, including the
// merge topology that feeds into it). Each output line is parsed into a
// GraphLine: the leading graph art is split from the commit fields so the
// caller can colour them separately and map a row back to its commit. Limit
// caps the commit count; pass <= 0 for the default (200).
func GraphLog(dir string, limit int) ([]GraphLine, error) {
	if limit <= 0 {
		limit = 200
	}
	// --graph draws the topology; the pretty format is appended after the art
	// on each NODE line. Connector lines carry no format output, so they have
	// no \x1f and parse as art-only. Fields: hash, subject, refs, committer
	// relative date, parent hashes (for merge detection).
	fmtArg := "--pretty=format:%h%x1f%s%x1f%d%x1f%cr%x1f%p"
	out, err := runOut(dir, "git", "log", "--graph", "--abbrev-commit", "--decorate", fmtArg, fmt.Sprintf("-n%d", limit))
	if err != nil {
		return nil, err
	}
	var lines []GraphLine
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, " \t")
		if line == "" {
			continue
		}
		sep := strings.IndexByte(line, '\x1f')
		if sep < 0 {
			// Connector-only line (no commit fields).
			lines = append(lines, GraphLine{Art: line})
			continue
		}
		fields := strings.SplitN(line, "\x1f", 5)
		head := fields[0] // "<art><hash>"
		gl := GraphLine{}
		if len(fields) > 1 {
			gl.Subject = fields[1]
		}
		if len(fields) > 2 {
			gl.Refs = strings.TrimSpace(fields[2])
		}
		if len(fields) > 3 {
			gl.Age = compactAge(fields[3])
		}
		if len(fields) > 4 {
			gl.IsMerge = strings.Contains(strings.TrimSpace(fields[4]), " ")
		}
		gl.IsHead = strings.Contains(gl.Refs, "HEAD")
		// The hash is the last whitespace-delimited token of head; everything
		// before it (incl. the trailing space) is the graph art.
		gl.Art, gl.Hash = "", head
		if sp := strings.LastIndexByte(head, ' '); sp >= 0 {
			gl.Art = head[:sp+1]
			gl.Hash = head[sp+1:]
		}
		lines = append(lines, gl)
	}
	return lines, nil
}

// compactAge squeezes git's `%cr` ("3 hours ago", "just now") into a 2-3 char
// badge ("3h", "now") for the narrow sidebar.
func compactAge(rel string) string {
	rel = strings.TrimSpace(rel)
	switch rel {
	case "", "just now":
		return "now"
	}
	fields := strings.Fields(rel) // ["3", "hours", "ago"]
	if len(fields) < 2 {
		return ""
	}
	n, unit := fields[0], fields[1]
	switch {
	case strings.HasPrefix(unit, "second"):
		return "now"
	case strings.HasPrefix(unit, "minute"):
		return n + "m"
	case strings.HasPrefix(unit, "hour"):
		return n + "h"
	case strings.HasPrefix(unit, "day"):
		return n + "d"
	case strings.HasPrefix(unit, "week"):
		return n + "w"
	case strings.HasPrefix(unit, "month"):
		return n + "mo"
	case strings.HasPrefix(unit, "year"):
		return n + "y"
	}
	return ""
}

// ShowCommit returns the full diff body for a commit (`git show <hash>`).
func ShowCommit(dir, hash string) (string, error) {
	return runOut(dir, "git", "show", "--no-color", hash)
}

// runStdin runs a command feeding `stdin` to it, returning a combined-output
// error so `git apply` rejections surface their reason.
func runStdin(dir, stdin, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// StageHunk stages the single diff hunk that contains working-file line `line`
// (1-based), the `git add -p` equivalent. UnstageHunk / DiscardHunk are the
// index-reverse / worktree-reverse twins.
func StageHunk(dir, file string, line int) error {
	// read worktree-vs-index diff, apply it forward to the index
	return applyOneHunk(dir, file, line, false, true, false)
}

// UnstageHunk removes one staged hunk from the index (reverse-apply the staged
// diff). The line is matched against the staged (index) side of the diff.
func UnstageHunk(dir, file string, line int) error {
	// read index-vs-HEAD diff, reverse-apply it to the index
	return applyOneHunk(dir, file, line, true, true, true)
}

// DiscardHunk reverts one unstaged hunk in the working tree.
func DiscardHunk(dir, file string, line int) error {
	// read worktree-vs-index diff, reverse-apply it to the worktree
	return applyOneHunk(dir, file, line, false, false, true)
}

// applyOneHunk extracts the hunk under `line` from the file's diff and feeds it
// to `git apply`. diffCached reads the staged diff (index-vs-HEAD); applyCached
// targets the index (vs worktree); applyReverse reverses the patch.
func applyOneHunk(dir, file string, line int, diffCached, applyCached, applyReverse bool) error {
	diffArgs := []string{"diff", "--no-color"}
	if diffCached {
		diffArgs = append(diffArgs, "--cached")
	}
	diffArgs = append(diffArgs, "--", file)
	diff, err := runOut(dir, "git", diffArgs...)
	if err != nil {
		return err
	}
	patch, ok := extractHunkPatch(diff, line)
	if !ok {
		return fmt.Errorf("no change under the cursor")
	}
	applyArgs := []string{"apply"}
	if applyCached {
		applyArgs = append(applyArgs, "--cached")
	}
	if applyReverse {
		applyArgs = append(applyArgs, "--reverse")
	}
	return runStdin(dir, patch, "git", append(applyArgs, "-")...)
}

// extractHunkPatch returns a one-hunk patch (file header + the single hunk
// whose new-side range contains `line`) suitable for `git apply`.
func extractHunkPatch(diff string, line int) (string, bool) {
	lines := strings.Split(diff, "\n")
	var header []string
	i := 0
	for ; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "@@") {
			break
		}
		header = append(header, lines[i])
	}
	for i < len(lines) {
		if !strings.HasPrefix(lines[i], "@@") {
			i++
			continue
		}
		start := i
		newStart, newCount := parseHunkNewRange(lines[i])
		i++
		for i < len(lines) && !strings.HasPrefix(lines[i], "@@") {
			i++
		}
		if newCount <= 0 {
			newCount = 1
		}
		if line >= newStart && line < newStart+newCount {
			var b strings.Builder
			for _, h := range header {
				b.WriteString(h)
				b.WriteByte('\n')
			}
			for _, h := range lines[start:i] {
				b.WriteString(h)
				b.WriteByte('\n')
			}
			return b.String(), true
		}
	}
	return "", false
}

// parseHunkNewRange reads the "+start,count" side of an "@@ -a,b +c,d @@" header.
func parseHunkNewRange(h string) (start, count int) {
	plus := strings.IndexByte(h, '+')
	if plus < 0 {
		return 0, 0
	}
	rest := h[plus+1:]
	end := strings.IndexByte(rest, ' ')
	if end < 0 {
		end = len(rest)
	}
	rest = rest[:end]
	if comma := strings.IndexByte(rest, ','); comma >= 0 {
		return atoiPrefix(rest[:comma]), atoiPrefix(rest[comma+1:])
	}
	return atoiPrefix(rest), 1
}

func atoiPrefix(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// ShowFileAtRevision returns a file's contents at a revision
// (`git show <rev>:<path>`). When the path doesn't exist at that revision —
// e.g. a newly-added/untracked file — it returns ("", nil) so callers can
// treat the "before" side as empty rather than an error.
func ShowFileAtRevision(dir, rev, path string) (string, error) {
	out, err := runOut(dir, "git", "show", "--no-color", rev+":"+path)
	if err != nil {
		return "", nil
	}
	return out, nil
}

// CommitDetail is the rich metadata shown in the hover card for a commit.
type CommitDetail struct {
	Hash    string // short SHA
	Author  string
	Email   string
	DateAbs string // "2026-06-04 09:24"
	DateRel string // "4 hours ago"
	Subject string
	Body    string // full message body (may be multi-line)
	Files   int
	Add     int
	Del     int
}

// ShowCommitDetail loads the metadata + shortstat for a single commit, used by
// the Source Control hover card.
func ShowCommitDetail(dir, hash string) (CommitDetail, error) {
	const sep = "\x1f"
	// Fields, then \x1e, then git appends the --shortstat summary line.
	fmtArg := "--format=%h" + sep + "%an" + sep + "%ae" + sep + "%ad" + sep + "%ar" + sep + "%s" + sep + "%b" + "\x1e"
	out, err := runOut(dir, "git", "show", "--no-color", "--shortstat",
		"--date=format-local:%Y-%m-%d %H:%M", fmtArg, hash)
	if err != nil {
		return CommitDetail{}, err
	}
	var d CommitDetail
	meta, tail := out, ""
	if i := strings.IndexByte(out, '\x1e'); i >= 0 {
		meta, tail = out[:i], out[i+1:]
	}
	f := strings.Split(meta, sep)
	if len(f) >= 7 {
		d.Hash, d.Author, d.Email = f[0], f[1], f[2]
		d.DateAbs, d.DateRel, d.Subject = f[3], f[4], f[5]
		d.Body = strings.TrimSpace(f[6])
	}
	d.Files, d.Add, d.Del = parseShortstat(tail)
	return d, nil
}

// parseShortstat extracts the counts from git's
// " 4 files changed, 88 insertions(+), 24 deletions(-)" summary line.
func parseShortstat(s string) (files, add, del int) {
	num := func(after string) int {
		i := strings.Index(s, after)
		if i < 0 {
			return 0
		}
		// walk back over the number preceding `after`.
		j := i
		for j > 0 && s[j-1] == ' ' {
			j--
		}
		end := j
		for j > 0 && s[j-1] >= '0' && s[j-1] <= '9' {
			j--
		}
		n := 0
		for _, c := range s[j:end] {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		return n
	}
	return num("file"), num("insertion"), num("deletion")
}

// DiffAgainst returns the diff between HEAD and a named ref / revision.
// `ref` can be a branch ("main"), a relative ref ("HEAD~3"), or any other
// rev-parse-able expression. The diff covers the whole repo, not a single
// file.
func DiffAgainst(dir, ref string) (string, error) {
	return runOut(dir, "git", "diff", "--no-color", ref)
}

// StashEntry is one row in `git stash list`.
type StashEntry struct {
	Ref     string // e.g. "stash@{0}"
	Message string // the user-visible message (after the "WIP on …" prefix)
}

// Stash runs `git stash push` (with optional message). Returns "no changes"
// if there's nothing to stash; that's a soft failure the caller can surface
// as a toast rather than treating as an error.
func Stash(dir, message string) error {
	args := []string{"stash", "push"}
	if message != "" {
		args = append(args, "-m", message)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	if strings.Contains(string(out), "No local changes to save") {
		return fmt.Errorf("no local changes to save")
	}
	return nil
}

// StashPop runs `git stash pop` (applies stash@{0} and removes it).
func StashPop(dir string) error {
	cmd := exec.Command("git", "stash", "pop")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// StashApply runs `git stash apply <ref>` — applies but doesn't drop.
func StashApply(dir, ref string) error {
	cmd := exec.Command("git", "stash", "apply", ref)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// StashDrop runs `git stash drop <ref>`.
func StashDrop(dir, ref string) error {
	cmd := exec.Command("git", "stash", "drop", ref)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// StashList parses `git stash list --pretty=%gd|%s` into entries.
func StashList(dir string) ([]StashEntry, error) {
	out, err := runOut(dir, "git", "stash", "list", "--pretty=%gd|%s")
	if err != nil {
		return nil, err
	}
	var entries []StashEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		entries = append(entries, StashEntry{Ref: parts[0], Message: parts[1]})
	}
	return entries, nil
}

// Push runs `git push` in dir. Returns the combined stdout+stderr on
// failure so the caller can surface it (push errors usually contain
// actionable messages — "no upstream", "non-fast-forward", auth failures).
func Push(dir string) error {
	cmd := exec.Command("git", "push")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// PushSetUpstream is the equivalent of `git push -u origin <branch>` for
// the case where the local branch has no tracked upstream yet. Inferred
// remote is `origin`; if the user has a different default remote they can
// run the command from the integrated terminal instead.
func PushSetUpstream(dir, branch string) error {
	cmd := exec.Command("git", "push", "-u", "origin", branch)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Pull runs `git pull` in dir. Same error-formatting strategy as Push.
func Pull(dir string) error {
	cmd := exec.Command("git", "pull")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Fetch runs `git fetch --prune` in dir, updating remote-tracking refs without
// touching the working tree. Same error-formatting strategy as Push/Pull.
func Fetch(dir string) error {
	cmd := exec.Command("git", "fetch", "--prune")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Branches lists local branches in the repo. The current branch is marked
// in HEAD-style but the returned strings are plain names — the caller
// already knows which one is current via GetBranch().
func Branches(dir string) ([]string, error) {
	out, err := runOut(dir, "git", "branch", "--list", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		names = append(names, line)
	}
	return names, nil
}

// Checkout switches to the named branch. Fails if the working tree has
// changes that would be overwritten — git's own safety net, surfaced as-is.
func Checkout(dir, branch string) error {
	cmd := exec.Command("git", "checkout", branch)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CreateBranch creates a new branch from HEAD and switches to it
// (`git checkout -b <name>`).
func CreateBranch(dir, name string) error {
	cmd := exec.Command("git", "checkout", "-b", name)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// HasUpstream reports whether the current branch has a tracked upstream.
// Used to pick between Push and PushSetUpstream from the UI.
func HasUpstream(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "@{upstream}")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// Diff returns the unified diff of path against HEAD (worktree diff including
// staged changes). For untracked files it returns the file contents formatted
// as an "added file" diff. Output is the raw text suitable for rendering.
func Diff(dir, path string) (string, error) {
	// Try worktree-vs-HEAD first (this captures both staged and unstaged
	// changes for tracked files). If git refuses (path unknown to git, e.g.
	// untracked) fall back to building a synthetic added-file diff.
	out, err := runOut(dir, "git", "diff", "HEAD", "--", path)
	if err == nil && strings.TrimSpace(out) != "" {
		return out, nil
	}
	// Fall back: maybe it's untracked — synthesise a diff so the viewer
	// has something to show.
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(dir, path)
	}
	bodyBytes, readErr := os.ReadFile(abs)
	if readErr != nil {
		if err != nil {
			return "", err
		}
		return "", readErr
	}
	body := string(bodyBytes)
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
	fmt.Fprintf(&b, "new file mode 100644\n")
	fmt.Fprintf(&b, "--- /dev/null\n")
	fmt.Fprintf(&b, "+++ b/%s\n", path)
	lines := strings.Split(body, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
	for _, ln := range lines {
		b.WriteString("+")
		b.WriteString(ln)
		b.WriteString("\n")
	}
	return b.String(), nil
}
