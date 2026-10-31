package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Agent worktrees (G-WT): an AI agent that runs at the same time as
// another one works in its own git worktree, so the two never write the
// same files. The worktree lives in the repository's git dir
// (<git-common-dir>/termocode/worktrees/<id>), not in the project: the
// explorer, search, file watchers and `git status` of the user's tree
// never see it and no .gitignore line is needed. Its branch is
// termocode/agent-<id>; it starts from the user's working state (the
// uncommitted edits too, through `git stash create`, which touches
// nothing) and that start point is kept as refs/termocode/agent-base/<id>.
// The user's branch, index and files are never changed, except by
// MergeAgentWorktree, which writes the agent's files into the user's tree
// (never the index).

// AgentBranchPrefix starts the branch of every agent worktree.
const AgentBranchPrefix = "termocode/agent-"

const agentBaseRef = "refs/termocode/agent-base/"

// AgentWorktree is one agent's own working tree.
type AgentWorktree struct {
	ID     string
	Path   string
	Branch string
	// Sub is the folder CreateAgentWorktree was asked from, relative to
	// the repository's top ("" at the top): the agent's workspace is
	// Dir(), the same folder in its tree.
	Sub string
}

// Dir is the agent's workspace in its worktree (Path joined with Sub).
func (w AgentWorktree) Dir() string { return filepath.Join(w.Path, w.Sub) }

// AgentChange is one file an agent changed: Status is A (added),
// M (modified) or D (deleted).
type AgentChange struct {
	Status string
	Path   string
}

// AgentMerge is what MergeAgentWorktree did: Applied are the files written
// into the user's tree; Conflicts are the files the user changed too
// (with force: written with conflict markers, or kept as the user's when
// they cannot be merged as text, listed in Kept).
type AgentMerge struct {
	Applied   []string
	Conflicts []string
	Kept      []string
}

// AgentWorktreeID makes a worktree id from a run id and a node id: lower
// case letters, digits and dashes only (it names a branch and a folder).
func AgentWorktreeID(runID, nodeID string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(runID + "-" + nodeID) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		switch {
		case ok:
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return string(out), fmt.Errorf("git %s: %s", args[0], msg)
		}
		return string(out), fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// agentRepo is the top of the user's work tree and the git common dir.
func agentRepo(dir string) (top, common string, err error) {
	out, err := gitOut(dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return "", "", errors.New("not a git repository")
	}
	f := strings.Split(strings.TrimSpace(out), "\n")
	if len(f) != 2 {
		return "", "", errors.New("not a git repository")
	}
	return f[0], f[1], nil
}

func agentWorktree(common, id string) AgentWorktree {
	return AgentWorktree{ID: id, Path: filepath.Join(common, "termocode", "worktrees", id), Branch: AgentBranchPrefix + id}
}

// AgentWorktreePath is where the worktree of id is (or would be).
func AgentWorktreePath(dir, id string) (AgentWorktree, error) {
	_, common, err := agentRepo(dir)
	if err != nil {
		return AgentWorktree{}, err
	}
	return agentWorktree(common, id), nil
}

// agentCreateMu lets one CreateAgentWorktree at a time change a
// repository: two `git worktree add` at once can read each other's half
// made admin folder ("failed to read .git/worktrees/<id>/commondir") and
// two `git stash create` at once fight for the index lock.
var agentCreateMu sync.Mutex

// agentLockTries is how often a git step that met another git process's
// lock is tried; agentLockWait is the first wait (it doubles).
var (
	agentLockTries = 6
	agentLockWait  = 40 * time.Millisecond
)

// gitLocked reports whether err is a git error that passes once the other
// git process is done (a lock file, a half made worktree).
func gitLocked(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	for _, s := range []string{".lock", "Another git process", "commondir", "cannot lock ref", "Unable to create"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

// gitRetry is gitOut tried again while another git process holds a lock
// (the user's own git, an editor, termocode's status refresh).
func gitRetry(dir string, args ...string) (string, error) {
	wait := agentLockWait
	for i := 1; ; i++ {
		out, err := gitOut(dir, args...)
		if !gitLocked(err) || i >= agentLockTries {
			return out, err
		}
		time.Sleep(wait)
		wait *= 2
	}
}

// CreateAgentWorktree makes the worktree of id from the user's working
// state, or returns it when it is there already (a retry keeps the
// agent's work). Creates run one at a time and a git lock is waited for
// (bounded); an error means there is no worktree, never a worktree from
// a wrong start.
func CreateAgentWorktree(dir, id string) (AgentWorktree, error) {
	return createAgentWorktree(dir, id, true)
}

// CreateAgentWorktreeFromHead is CreateAgentWorktree from the last commit
// only: the user's uncommitted edits stay out (a plan run's branch that
// may be pushed).
func CreateAgentWorktreeFromHead(dir, id string) (AgentWorktree, error) {
	return createAgentWorktree(dir, id, false)
}

func createAgentWorktree(dir, id string, edits bool) (AgentWorktree, error) {
	if id == "" {
		return AgentWorktree{}, errors.New("no worktree id")
	}
	agentCreateMu.Lock()
	defer agentCreateMu.Unlock()
	top, common, err := agentRepo(dir)
	if err != nil {
		return AgentWorktree{}, err
	}
	w := agentWorktree(common, id)
	if pre, err := gitOut(dir, "rev-parse", "--show-prefix"); err == nil {
		w.Sub = filepath.FromSlash(strings.TrimSuffix(strings.TrimSpace(pre), "/"))
	}
	if _, err := os.Stat(filepath.Join(w.Path, ".git")); err == nil {
		return w, nil
	}
	head, err := gitOut(top, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		return AgentWorktree{}, errors.New("the repository has no commit yet: an agent worktree needs one")
	}
	base := strings.TrimSpace(head)
	// stash create takes the index lock; a failure is not "no edits": the
	// worktree would miss the user's uncommitted edits.
	if edits {
		st, err := gitRetry(top, "stash", "create", "termocode agent "+id)
		if err != nil {
			return AgentWorktree{}, fmt.Errorf("could not read your uncommitted edits: %w", err)
		}
		if st := strings.TrimSpace(st); st != "" {
			base = st // the user's uncommitted edits, as a commit nothing points at
		}
	}
	if err := os.MkdirAll(filepath.Dir(w.Path), 0o755); err != nil {
		return AgentWorktree{}, err
	}
	// A worktree folder deleted by hand is still registered, and git
	// refuses to add it again until a prune.
	_, _ = gitOut(top, "worktree", "prune")
	if _, err := gitRetry(top, "update-ref", agentBaseRef+id, base); err != nil {
		return AgentWorktree{}, err
	}
	wait := agentLockWait
	for i := 1; ; i++ {
		// checkout.workers=0: write the files with one worker per core
		// (git's parallel checkout; the same files, a big repo is ready
		// sooner).
		_, err = gitOut(top, "-c", "checkout.workers=0", "worktree", "add", "-q", "-B", w.Branch, w.Path, base)
		if err == nil {
			return w, nil
		}
		// A failed add may leave a half made tree: clear it before a try.
		_ = os.RemoveAll(w.Path)
		_, _ = gitOut(top, "worktree", "prune")
		if !gitLocked(err) || i >= agentLockTries {
			break
		}
		time.Sleep(wait)
		wait *= 2
	}
	_, _ = gitOut(top, "update-ref", "-d", agentBaseRef+id)
	return AgentWorktree{}, err
}

// snapshotAgent commits everything in the worktree on its branch (the
// agent's work is then safe in git), and returns the base and the tip.
func snapshotAgent(dir, id string) (top string, w AgentWorktree, base, tip string, err error) {
	top, common, err := agentRepo(dir)
	if err != nil {
		return "", w, "", "", err
	}
	w = agentWorktree(common, id)
	b, err := gitOut(top, "rev-parse", "--verify", "-q", agentBaseRef+id)
	if err != nil {
		return "", w, "", "", fmt.Errorf("agent worktree %s is not known", id)
	}
	base = strings.TrimSpace(b)
	if _, err := os.Stat(w.Path); err == nil {
		if _, err := gitOut(w.Path, "add", "-A"); err != nil {
			return "", w, "", "", err
		}
		if st, _ := gitOut(w.Path, "status", "--porcelain"); strings.TrimSpace(st) != "" {
			args := []string{"-c", "commit.gpgsign=false"}
			if e, _ := gitOut(w.Path, "config", "user.email"); strings.TrimSpace(e) == "" {
				args = append(args, "-c", "user.name=termocode", "-c", "user.email=termocode@localhost")
			}
			args = append(args, "commit", "-q", "--no-verify", "-m", "termocode agent "+id)
			if _, err := gitOut(w.Path, args...); err != nil {
				return "", w, "", "", err
			}
		}
	}
	t, err := gitOut(top, "rev-parse", "--verify", "-q", "refs/heads/"+w.Branch)
	if err != nil {
		return "", w, "", "", fmt.Errorf("branch %s is gone", w.Branch)
	}
	return top, w, base, strings.TrimSpace(t), nil
}

// CommitAgentWorktree commits everything in the worktree of id on its
// branch with message (the user's git identity and hooks; termocode's
// identity only when the repository has none). ok=false: nothing to
// commit.
func CommitAgentWorktree(dir, id, message string) (ok bool, err error) {
	_, common, err := agentRepo(dir)
	if err != nil {
		return false, err
	}
	w := agentWorktree(common, id)
	if _, err := os.Stat(w.Path); err != nil {
		return false, fmt.Errorf("agent worktree %s is gone", id)
	}
	if _, err := gitOut(w.Path, "add", "-A"); err != nil {
		return false, err
	}
	if st, _ := gitOut(w.Path, "status", "--porcelain"); strings.TrimSpace(st) == "" {
		return false, nil
	}
	var args []string
	if e, _ := gitOut(w.Path, "config", "user.email"); strings.TrimSpace(e) == "" {
		args = append(args, "-c", "user.name=termocode", "-c", "user.email=termocode@localhost")
	}
	if _, err := gitOut(w.Path, append(args, "commit", "-q", "-m", message)...); err != nil {
		return false, err
	}
	return true, nil
}

// AgentChanges lists the files the agent of id changed, by path.
func AgentChanges(dir, id string) ([]AgentChange, error) {
	top, _, base, tip, err := snapshotAgent(dir, id)
	if err != nil {
		return nil, err
	}
	ds, err := agentDiffRaw(top, base, tip)
	if err != nil {
		return nil, err
	}
	ch := make([]AgentChange, len(ds))
	for i, d := range ds {
		ch[i] = d.AgentChange
	}
	return ch, nil
}

// agentDiffEntry is one changed path with its blob ids and modes before
// (base) and after (tip); a side without the file has the zero id.
type agentDiffEntry struct {
	AgentChange
	baseMode, tipMode string
	baseID, tipID     string
}

// agentDiffRaw is `git diff --raw` from base to tip, by path: one git
// process for every changed file's ids and modes.
func agentDiffRaw(top, base, tip string) ([]agentDiffEntry, error) {
	out, err := gitOut(top, "diff", "--no-renames", "--raw", "--no-abbrev", "-z", base, tip)
	if err != nil {
		return nil, err
	}
	var ds []agentDiffEntry
	f := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	for i := 0; i+1 < len(f); i += 2 {
		// ":<base mode> <tip mode> <base id> <tip id> <status>"
		h := strings.Fields(strings.TrimPrefix(f[i], ":"))
		if len(h) != 5 {
			return nil, fmt.Errorf("git diff: unexpected line %q", f[i])
		}
		st := h[4]
		if st == "T" {
			st = "M"
		}
		ds = append(ds, agentDiffEntry{AgentChange: AgentChange{Status: st, Path: f[i+1]},
			baseMode: h[0], tipMode: h[1], baseID: h[2], tipID: h[3]})
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i].Path < ds[j].Path })
	return ds, nil
}

// agentBlobs reads the blobs of ids with one `git cat-file --batch`. A
// zero id, a submodule (not a blob) or a missing object is not in the
// map.
func agentBlobs(top string, ids []string) (map[string][]byte, error) {
	var in strings.Builder
	asked := map[string]bool{}
	for _, id := range ids {
		if strings.Trim(id, "0") == "" || asked[id] {
			continue
		}
		asked[id] = true
		in.WriteString(id + "\n")
	}
	out := map[string][]byte{}
	if len(asked) == 0 {
		return out, nil
	}
	cmd := spawn.Command("git", "cat-file", "--batch")
	cmd.Dir = top
	cmd.Stdin = strings.NewReader(in.String())
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	for len(b) > 0 {
		nl := bytes.IndexByte(b, '\n')
		if nl < 0 {
			return nil, errors.New("git cat-file: output cut short")
		}
		h := strings.Fields(string(b[:nl]))
		b = b[nl+1:]
		if len(h) != 3 { // "<id> missing"
			continue
		}
		n, err := strconv.Atoi(h[2])
		if err != nil || n+1 > len(b) {
			return nil, errors.New("git cat-file: bad object size")
		}
		if h[1] == "blob" {
			out[h[0]] = b[:n:n]
		}
		b = b[n+1:]
	}
	return out, nil
}

// agentChangedFiles snapshots the agent of id and returns every changed
// file with its blobs before (base) and after (tip): one snapshot and a
// fixed number of git processes, however many files changed.
func agentChangedFiles(dir, id string) (top string, ds []agentDiffEntry, blobs map[string][]byte, err error) {
	top, _, base, tip, err := snapshotAgent(dir, id)
	if err != nil {
		return "", nil, nil, err
	}
	if ds, err = agentDiffRaw(top, base, tip); err != nil {
		return "", nil, nil, err
	}
	ids := make([]string, 0, 2*len(ds))
	for _, d := range ds {
		ids = append(ids, d.baseID, d.tipID)
	}
	if blobs, err = agentBlobs(top, ids); err != nil {
		return "", nil, nil, err
	}
	return top, ds, blobs, nil
}

// AgentDiff is the patch of everything the agent of id changed.
func AgentDiff(dir, id string) (string, error) {
	top, _, base, tip, err := snapshotAgent(dir, id)
	if err != nil {
		return "", err
	}
	return gitOut(top, "diff", "--no-renames", base, tip)
}

type agentFile struct {
	path               string
	base, theirs, ours []byte
	hasBase, hasTheirs bool
	hasOurs            bool
	mode               string
	baseMode           string
}

// MergeAgentWorktree writes the agent's changes into the user's tree,
// file by file against what the user has now. Without force a conflict
// stops it before any file is written (Conflicts lists them). With force
// the clean files are written and each conflict gets conflict markers;
// a conflict that is not plain text keeps the user's file (Kept). The
// worktree and branch stay; RemoveAgentWorktree removes them.
func MergeAgentWorktree(dir, id string, force bool) (AgentMerge, error) {
	top, ds, blobs, err := agentChangedFiles(dir, id)
	if err != nil {
		return AgentMerge{}, err
	}
	var res AgentMerge
	var todo, conflicts []agentFile
	for _, c := range ds {
		f := agentFile{path: c.Path, baseMode: c.baseMode}
		f.base, f.hasBase = blobs[c.baseID]
		f.theirs, f.hasTheirs = blobs[c.tipID]
		if f.hasTheirs {
			f.mode = c.tipMode
		}
		full := filepath.Join(top, filepath.FromSlash(c.Path))
		fi, err := os.Lstat(full)
		if err != nil {
			fi = nil
		}
		if err == nil && fi.Mode().IsRegular() {
			f.ours, _ = os.ReadFile(full)
			f.hasOurs = true
		} else if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			l, _ := os.Readlink(full)
			f.ours, f.hasOurs = []byte(l), true
		}
		switch {
		case f.hasOurs == f.hasTheirs && bytes.Equal(f.ours, f.theirs):
			// The user has the text already; a mode-only change (chmod +x)
			// still needs the exec bit (FIX-27).
			if f.hasTheirs && fi != nil && fi.Mode().IsRegular() {
				if err := setAgentExec(full, fi.Mode().Perm(), f.baseMode, f.mode); err != nil {
					return res, err
				}
			}
			res.Applied = append(res.Applied, c.Path) // the user has it already
		case f.hasOurs == f.hasBase && bytes.Equal(f.ours, f.base):
			todo = append(todo, f)
		default:
			conflicts = append(conflicts, f)
			res.Conflicts = append(res.Conflicts, c.Path)
		}
	}
	if len(conflicts) > 0 && !force {
		res.Applied = nil
		return res, nil
	}
	for _, f := range todo {
		if err := writeAgentFile(top, f.path, f.theirs, f.baseMode, f.mode, f.hasTheirs); err != nil {
			return res, err
		}
		res.Applied = append(res.Applied, f.path)
	}
	for _, f := range conflicts {
		text := f.hasOurs && f.hasTheirs && f.mode != "120000" &&
			bytes.IndexByte(f.ours, 0) < 0 && bytes.IndexByte(f.theirs, 0) < 0 && bytes.IndexByte(f.base, 0) < 0
		if !text {
			res.Kept = append(res.Kept, f.path)
			continue
		}
		merged, err := mergeText(f.ours, f.base, f.theirs, id)
		if err != nil {
			return res, err
		}
		if err := writeAgentFile(top, f.path, merged, f.baseMode, f.mode, true); err != nil {
			return res, err
		}
	}
	sort.Strings(res.Applied)
	return res, nil
}

func writeAgentFile(top, path string, data []byte, baseMode, mode string, keep bool) error {
	full := filepath.Join(top, filepath.FromSlash(path))
	if !keep {
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if mode == "120000" {
		_ = os.Remove(full)
		return os.Symlink(string(data), full)
	}
	perm := os.FileMode(0o644)
	if mode == "100755" {
		perm = 0o755
	}
	fi, statErr := os.Stat(full)
	if statErr == nil && mode != "100755" {
		perm = fi.Mode().Perm()
	}
	if err := os.WriteFile(full, data, perm); err != nil {
		return err
	}
	if statErr != nil { // created: WriteFile set perm
		return nil
	}
	// os.WriteFile keeps an existing file's mode: set the agent's exec
	// bit change on it (FIX-27).
	return setAgentExec(full, fi.Mode().Perm(), baseMode, mode)
}

// setAgentExec applies the agent's exec bit change to a regular file the
// user has: 100644 → 100755 adds x where r is set, 100755 → 100644 drops
// it. Other bits and other changes keep the user's mode.
func setAgentExec(full string, perm os.FileMode, baseMode, mode string) error {
	want := perm
	switch {
	case mode == "100755":
		want = perm | (perm&0o444)>>2
	case mode == "100644" && baseMode == "100755":
		want = perm &^ 0o111
	}
	if want == perm {
		return nil
	}
	return os.Chmod(full, want)
}

// mergeText is `git merge-file -p` of the three versions (markers on a
// conflict).
func mergeText(ours, base, theirs []byte, id string) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "termocode-merge-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	names := []string{"yours", "base", "agent"}
	for i, b := range [][]byte{ours, base, theirs} {
		if err := os.WriteFile(filepath.Join(tmp, names[i]), b, 0o600); err != nil {
			return nil, err
		}
	}
	cmd := spawn.Command("git", "merge-file", "-p", "-L", "yours", "-L", "base", "-L", "agent "+id, "yours", "base", "agent")
	cmd.Dir = tmp
	out, err := cmd.Output()
	var ee interface{ ExitCode() int }
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() > 0) {
		return nil, fmt.Errorf("git merge-file: %w", err)
	}
	return out, nil
}

// RemoveAgentWorktree removes the worktree of id; with dropBranch also
// its branch and base ref (the work is then gone).
func RemoveAgentWorktree(dir, id string, dropBranch bool) error {
	top, common, err := agentRepo(dir)
	if err != nil {
		return err
	}
	w := agentWorktree(common, id)
	if _, err := os.Stat(w.Path); err == nil {
		if _, err := gitOut(top, "worktree", "remove", "--force", w.Path); err != nil {
			return err
		}
	}
	_, _ = gitOut(top, "worktree", "prune")
	if dropBranch {
		_, _ = gitOut(top, "branch", "-D", w.Branch)
		_, _ = gitOut(top, "update-ref", "-d", agentBaseRef+id)
	}
	return nil
}

// LeftoverAgentWorktrees lists the agent worktrees of the repository at
// dir (by id). It only reads.
func LeftoverAgentWorktrees(dir string) ([]AgentWorktree, error) {
	top, common, err := agentRepo(dir)
	if err != nil {
		return nil, err
	}
	out, err := gitOut(top, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var left []AgentWorktree
	for _, block := range strings.Split(out, "\n\n") {
		for _, line := range strings.Split(block, "\n") {
			if b, ok := strings.CutPrefix(line, "branch refs/heads/"+AgentBranchPrefix); ok {
				left = append(left, agentWorktree(common, b))
			}
		}
	}
	sort.Slice(left, func(i, j int) bool { return left[i].ID < left[j].ID })
	return left, nil
}

// AgentFile is one changed file with its text before the agent (Before)
// and after (After); a side that does not exist is "".
type AgentFile struct {
	AgentChange
	Before, After string
}

// AgentFiles lists the files the agent of id changed with both texts
// (the review card).
func AgentFiles(dir, id string) ([]AgentFile, error) {
	_, ds, blobs, err := agentChangedFiles(dir, id)
	if err != nil {
		return nil, err
	}
	out := make([]AgentFile, 0, len(ds))
	for _, c := range ds {
		out = append(out, AgentFile{AgentChange: c.AgentChange, Before: string(blobs[c.baseID]), After: string(blobs[c.tipID])})
	}
	return out, nil
}
