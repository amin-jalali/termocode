package worktrees

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/git"
)

// Kind is where a worktree came from.
type Kind int

const (
	KindMain     Kind = iota // the main checkout
	KindUser                 // made by the user through termocode (<repo>.worktrees/<name>)
	KindAgent                // an agent lane termocode made (in the git dir)
	KindExternal             // made outside termocode (`git worktree add` by hand)
)

func (k Kind) String() string {
	switch k {
	case KindMain:
		return "main"
	case KindUser:
		return "user"
	case KindAgent:
		return "agent"
	}
	return "external"
}

// Entry is one worktree of the view: git's facts and termocode's
// metadata. Gone: git lists it as prunable (its folder was deleted) or
// no longer lists it at all (only the metadata knows it).
type Entry struct {
	git.Worktree
	Kind Kind
	Gone bool
	Meta Meta
}

// Title is the worktree's name: the one the user gave, else its branch,
// else its folder.
func (e Entry) Title() string {
	switch {
	case e.Meta.Name != "":
		return e.Meta.Name
	case e.Branch != "":
		return e.Branch
	}
	return filepath.Base(e.Path)
}

// UserDir is the folder user worktrees go in (owner decision D3): a
// sibling of the main checkout, <repo>.worktrees.
func UserDir(mainPath string) string { return filepath.Clean(mainPath) + ".worktrees" }

// within: p is dir or inside it.
func within(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, dir+string(filepath.Separator))
}

// Build joins git's list with the metadata, in git's order; metadata of
// worktrees git does not list comes last (sorted by path) as gone. Bare
// blocks are left out: there is nothing to open.
func Build(list []git.Worktree, metas map[string]Meta) []Entry {
	clean := make(map[string]Meta, len(metas))
	for p, m := range metas {
		clean[filepath.Clean(p)] = m
	}
	userDir := ""
	for _, w := range list {
		if w.Main {
			userDir = UserDir(w.Path)
		}
	}
	var out []Entry
	seen := map[string]bool{}
	for _, w := range list {
		if w.Bare {
			continue
		}
		p := filepath.Clean(w.Path)
		seen[p] = true
		e := Entry{Worktree: w, Meta: clean[p], Gone: w.Prunable}
		e.Path = p
		switch {
		case w.Main:
			e.Kind = KindMain
		case w.Agent:
			e.Kind = KindAgent
		case (userDir != "" && within(p, userDir)) || !e.Meta.Created.IsZero():
			e.Kind = KindUser
		default:
			e.Kind = KindExternal
		}
		out = append(out, e)
	}
	var gone []string
	for p := range clean {
		if !seen[p] {
			gone = append(gone, p)
		}
	}
	sort.Strings(gone)
	for _, p := range gone {
		e := Entry{Worktree: git.Worktree{Path: p, Branch: clean[p].Branch}, Meta: clean[p], Gone: true, Kind: KindExternal}
		if (userDir != "" && within(p, userDir)) || !e.Meta.Created.IsZero() {
			e.Kind = KindUser
		}
		out = append(out, e)
	}
	return out
}
