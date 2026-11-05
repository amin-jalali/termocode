package git

import (
	"errors"
	"path/filepath"
	"strings"
)

// Worktrees (WTX-1): every working tree of a repository as
// `git worktree list --porcelain` reports it. Git stays the truth for path
// and branch; internal/worktrees adds what git does not know (name, pin,
// notes …).

// Worktree is one block of `git worktree list --porcelain`.
type Worktree struct {
	Path string
	Head string // the commit id ("" for a bare repository)
	// Branch is the short branch name ("" when detached or bare).
	Branch      string
	Bare        bool
	Detached    bool
	Locked      bool
	LockReason  string
	Prunable    bool // its folder is gone (git worktree prune would drop it)
	PruneReason string
	// Main is the first block: the main checkout.
	Main bool
	// Agent: termocode made it for an agent lane (G-WT): it sits in
	// <git-common-dir>/termocode/worktrees/ or is on a termocode/agent-*
	// branch. AgentID is the lane's id.
	Agent   bool
	AgentID string
}

// ParseWorktreeList reads the porcelain output; common is the git common
// dir (absolute), which tells agent lanes apart.
func ParseWorktreeList(out, common string) []Worktree {
	lanes := filepath.Join(common, "termocode", "worktrees") + string(filepath.Separator)
	var ws []Worktree
	for _, block := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n\n") {
		var w Worktree
		for _, line := range strings.Split(block, "\n") {
			key, val, _ := strings.Cut(line, " ")
			switch key {
			case "worktree":
				w.Path = val
			case "HEAD":
				w.Head = val
			case "branch":
				w.Branch = strings.TrimPrefix(val, "refs/heads/")
			case "bare":
				w.Bare = true
			case "detached":
				w.Detached = true
			case "locked":
				w.Locked, w.LockReason = true, val
			case "prunable":
				w.Prunable, w.PruneReason = true, val
			}
		}
		if w.Path == "" {
			continue
		}
		w.Main = len(ws) == 0
		if id, ok := strings.CutPrefix(w.Branch, AgentBranchPrefix); ok {
			w.Agent, w.AgentID = true, id
		} else if rest, ok := strings.CutPrefix(filepath.Clean(w.Path)+string(filepath.Separator), lanes); ok && !w.Main {
			w.Agent, w.AgentID = true, strings.TrimSuffix(rest, string(filepath.Separator))
		}
		ws = append(ws, w)
	}
	return ws
}

// ListWorktrees lists the worktrees of the repository dir is in (from any
// of its worktrees) and returns the git common dir too.
func ListWorktrees(dir string) ([]Worktree, string, error) {
	out, err := gitOut(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, "", errors.New("not a git repository")
	}
	common := strings.TrimSpace(out)
	out, err = gitOut(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, "", err
	}
	return ParseWorktreeList(out, common), common, nil
}

// WorktreeStatus is how a worktree differs: Dirty changed, new or
// conflicted files; Ahead / Behind its upstream (Upstream: it has one).
type WorktreeStatus struct {
	Dirty    int
	Ahead    int
	Behind   int
	Upstream bool
}

// GetWorktreeStatus reads the status of the worktree at path with one
// `git status --porcelain=v2 --branch` call (no optional locks: it never
// takes the index lock from the user's own git command).
func GetWorktreeStatus(path string) (WorktreeStatus, error) {
	out, err := gitOut(path, "--no-optional-locks", "status", "--porcelain=v2", "--branch")
	if err != nil {
		return WorktreeStatus{}, err
	}
	return parseWorktreeStatus(out), nil
}

func parseWorktreeStatus(out string) WorktreeStatus {
	var st WorktreeStatus
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
		case strings.HasPrefix(line, "# branch.upstream "):
			st.Upstream = true
		case strings.HasPrefix(line, "# branch.ab "):
			f := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(f) == 2 {
				st.Ahead = atoiPrefix(strings.TrimPrefix(f[0], "+"))
				st.Behind = atoiPrefix(strings.TrimPrefix(f[1], "-"))
			}
		case line[0] == '#', line[0] == '!':
		default:
			st.Dirty++
		}
	}
	return st
}
