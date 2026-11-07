// Package checkpoint keeps code checkpoints of a git working tree
// (CMP-62). A checkpoint is the whole working tree at one moment:
// tracked files with their unstaged and staged edits, and new files that
// are not ignored. It is saved as a commit under
// refs/termocode/checkpoints/<worktree key>/<id>, built in a temporary index, so the
// user's index, branch, HEAD and stash are never touched.
//
// Any agent's work can be rolled back with it, also an outside CLI agent
// in a terminal tab: termocode saves one when such an agent starts a
// turn (its hook reports "working"), and the user can save one by hand.
// Restore first saves the current tree as a new checkpoint, so a restore
// can itself be undone.
//
// Ignored files (build output, node_modules, .env …) are not part of a
// checkpoint and a restore leaves them alone.
package checkpoint

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// RefPrefix is where checkpoints live in the repository.
const RefPrefix = "refs/termocode/checkpoints/"

// refBase is the ref folder of the working tree at top: the worktrees of
// one repository share their refs, so each keeps its own folder.
func refBase(top string) string {
	sum := sha1.Sum([]byte(filepath.Clean(top)))
	return RefPrefix + hex.EncodeToString(sum[:6]) + "/"
}

// subjectPrefix starts each checkpoint commit's message.
const subjectPrefix = "termocode checkpoint: "

// MaxPoints is how many checkpoints a repository keeps; older ones are
// dropped when a new one is saved. A var so tests can lower it.
var MaxPoints = 50

// restoreChunk is how many paths one `git restore` gets.
const restoreChunk = 200

// ErrNotRepo: the folder is not inside a git working tree.
var ErrNotRepo = errors.New("not a git repository: checkpoints need git")

// Point is one saved checkpoint.
type Point struct {
	ID     string // the ref name after the worktree's folder (unix nanoseconds)
	Commit string
	At     time.Time
	Label  string
}

// Change is one file that differs between a checkpoint and now.
type Change struct {
	Path string // slash path from the repository top
	// Kind: 'A' made after the checkpoint (a restore deletes it), 'M'
	// changed since (a restore puts the old text back), 'D' deleted
	// since (a restore brings it back).
	Kind byte
}

// Result is what a restore did.
type Result struct {
	Restored []string // put back (changed or deleted since)
	Removed  []string // made after the checkpoint, deleted
	Failed   []string // could not be deleted
	Undo     Point    // the tree just before the restore
}

// identity is the author of checkpoint commits: they are private refs,
// so the user's own name and config are not needed.
var identity = []string{
	"GIT_AUTHOR_NAME=termocode", "GIT_AUTHOR_EMAIL=termocode@localhost",
	"GIT_COMMITTER_NAME=termocode", "GIT_COMMITTER_EMAIL=termocode@localhost",
}

// git runs git in dir; env is added to the process environment.
func git(dir string, env []string, args ...string) (string, error) {
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		name := ""
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				name = a
				break
			}
		}
		if msg := firstLine(stderr.String()); msg != "" {
			return string(out), fmt.Errorf("git %s: %s", name, msg)
		}
		return string(out), fmt.Errorf("git %s: %w", name, err)
	}
	return string(out), nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Top is the top folder of the working tree dir is in.
func Top(dir string) (string, error) {
	out, err := git(dir, nil, "rev-parse", "--show-toplevel")
	top := strings.TrimSpace(out)
	if err != nil || top == "" {
		return "", ErrNotRepo
	}
	return top, nil
}

// head is the HEAD commit ("" in a repository with no commit yet).
func head(top string) string {
	out, err := git(top, nil, "rev-parse", "--verify", "-q", "HEAD^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// tree writes the working tree as a git tree, through a copy of the
// index (its stat cache keeps `add -A` quick), and gives its id.
func tree(top string) (string, error) {
	tmp, err := os.MkdirTemp("", "termocode-checkpoint-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	idx := filepath.Join(tmp, "index")
	env := append([]string{"GIT_INDEX_FILE=" + idx}, identity...)
	copied := false
	if p, err := git(top, nil, "rev-parse", "--path-format=absolute", "--git-path", "index"); err == nil {
		copied = copyFile(strings.TrimSpace(p), idx) == nil
	}
	if !copied {
		_ = os.Remove(idx) // a half copy is no index
		if h := head(top); h != "" {
			if _, err := git(top, env, "read-tree", h); err != nil {
				return "", err
			}
		}
	}
	if _, err := git(top, env, "add", "-A", "--", "."); err != nil {
		return "", err
	}
	out, err := git(top, env, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// cleanLabel keeps a label on one short line.
func cleanLabel(label string) string {
	label = strings.Join(strings.Fields(label), " ")
	if r := []rune(label); len(r) > 80 {
		label = string(r[:80]) + "…"
	}
	if label == "" {
		label = "checkpoint"
	}
	return label
}

// Save keeps the working tree of the repository dir is in as a new
// checkpoint. Nothing in the working tree, the index or HEAD changes.
func Save(dir, label string) (Point, error) {
	top, err := Top(dir)
	if err != nil {
		return Point{}, err
	}
	label = cleanLabel(label)
	t, err := tree(top)
	if err != nil {
		return Point{}, err
	}
	args := []string{"commit-tree", "--no-gpg-sign", "-m", subjectPrefix + label}
	if h := head(top); h != "" {
		args = append(args, "-p", h)
	}
	args = append(args, t)
	out, err := git(top, identity, args...)
	if err != nil {
		return Point{}, err
	}
	commit := strings.TrimSpace(out)
	at := clock.Now()
	id := strconv.FormatInt(at.UnixNano(), 10)
	// Two saves in the same nanosecond (a coarse test clock): next id.
	base := refBase(top)
	for n := 0; n < 1000; n++ {
		if _, err := git(top, nil, "rev-parse", "--verify", "-q", base+id); err != nil {
			break
		}
		id = strconv.FormatInt(at.UnixNano()+int64(n+1), 10)
	}
	if _, err := git(top, nil, "update-ref", base+id, commit); err != nil {
		return Point{}, err
	}
	prune(top)
	return Point{ID: id, Commit: commit, At: at, Label: label}, nil
}

// List gives the checkpoints of the repository dir is in, newest first.
func List(dir string) ([]Point, error) {
	top, err := Top(dir)
	if err != nil {
		return nil, err
	}
	base := refBase(top)
	out, err := git(top, nil, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(subject)", base)
	if err != nil {
		return nil, err
	}
	var pts []Point
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\x00", 3)
		if len(f) != 3 || !strings.HasPrefix(f[0], base) {
			continue
		}
		id := strings.TrimPrefix(f[0], base)
		ns, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			continue // not ours
		}
		pts = append(pts, Point{ID: id, Commit: f[1], At: time.Unix(0, ns), Label: strings.TrimPrefix(f[2], subjectPrefix)})
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].At.After(pts[j].At) })
	return pts, nil
}

// Find is the checkpoint with id.
func Find(dir, id string) (Point, error) {
	pts, err := List(dir)
	if err != nil {
		return Point{}, err
	}
	for _, p := range pts {
		if p.ID == id {
			return p, nil
		}
	}
	return Point{}, fmt.Errorf("no checkpoint %q", id)
}

// Delete drops one checkpoint.
func Delete(dir, id string) error {
	top, err := Top(dir)
	if err != nil {
		return err
	}
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return fmt.Errorf("no checkpoint %q", id)
	}
	_, err = git(top, nil, "update-ref", "-d", refBase(top)+id)
	return err
}

// prune keeps the newest MaxPoints checkpoints.
func prune(top string) {
	pts, err := List(top)
	if err != nil || len(pts) <= MaxPoints {
		return
	}
	base := refBase(top)
	for _, p := range pts[MaxPoints:] {
		_, _ = git(top, nil, "update-ref", "-d", base+p.ID)
	}
}

// diff lists what differs from tree a to tree-ish b.
func diff(top, a, b string) ([]Change, error) {
	out, err := git(top, nil, "diff-tree", "-r", "-z", "--no-renames", "--name-status", a, b)
	if err != nil {
		return nil, err
	}
	f := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var ch []Change
	for i := 0; i+1 < len(f); i += 2 {
		if f[i] == "" {
			continue
		}
		k := f[i][0]
		if k == 'T' {
			k = 'M'
		}
		ch = append(ch, Change{Path: f[i+1], Kind: k})
	}
	return ch, nil
}

// Changes lists the files that differ between checkpoint p and the
// working tree now: what a restore of p would change.
func Changes(dir string, p Point) ([]Change, error) {
	top, err := Top(dir)
	if err != nil {
		return nil, err
	}
	now, err := tree(top)
	if err != nil {
		return nil, err
	}
	return diff(top, p.Commit, now)
}

// Restore puts the working tree back to checkpoint p: changed and
// deleted files get their checkpoint text, files made after it are
// deleted. The index and HEAD stay; ignored files stay. The tree just
// before is saved first (Result.Undo), so the restore can be undone.
func Restore(dir string, p Point) (Result, error) {
	top, err := Top(dir)
	if err != nil {
		return Result{}, err
	}
	undo, err := Save(top, "before restoring: "+p.Label)
	if err != nil {
		return Result{}, err
	}
	res := Result{Undo: undo}
	ch, err := diff(top, p.Commit, undo.Commit)
	if err != nil {
		return res, err
	}
	var back []string
	for _, c := range ch {
		if c.Kind == 'A' {
			abs := filepath.Join(top, filepath.FromSlash(c.Path))
			if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
				res.Failed = append(res.Failed, c.Path)
				continue
			}
			res.Removed = append(res.Removed, c.Path)
			removeEmptyParents(top, filepath.Dir(abs))
			continue
		}
		back = append(back, c.Path)
	}
	for i := 0; i < len(back); i += restoreChunk {
		j := i + restoreChunk
		if j > len(back) {
			j = len(back)
		}
		args := append([]string{"--literal-pathspecs", "restore", "--source=" + p.Commit, "--worktree", "--"}, back[i:j]...)
		if _, err := git(top, nil, args...); err != nil {
			return res, err
		}
		res.Restored = append(res.Restored, back[i:j]...)
	}
	return res, nil
}

// removeEmptyParents drops folders a restore left empty, up to top.
func removeEmptyParents(top, dir string) {
	top = filepath.Clean(top)
	for d := filepath.Clean(dir); d != top && strings.HasPrefix(d, top+string(filepath.Separator)); d = filepath.Dir(d) {
		if os.Remove(d) != nil { // not empty (or gone): stop
			return
		}
	}
}

// Summary is "2 changed, 1 new (deleted on restore), 1 deleted".
func Summary(ch []Change) string {
	var m, a, d int
	for _, c := range ch {
		switch c.Kind {
		case 'A':
			a++
		case 'D':
			d++
		default:
			m++
		}
	}
	var parts []string
	if m > 0 {
		parts = append(parts, strconv.Itoa(m)+" changed since")
	}
	if a > 0 {
		parts = append(parts, strconv.Itoa(a)+" new since (deleted)")
	}
	if d > 0 {
		parts = append(parts, strconv.Itoa(d)+" deleted since (brought back)")
	}
	if len(parts) == 0 {
		return "no change since"
	}
	return strings.Join(parts, ", ")
}
