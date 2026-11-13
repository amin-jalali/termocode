package app

import (
	"os"
	"path/filepath"
	"strings"
)

// readBranch is the branch dir's repository is on, read from .git/HEAD
// (no git process; a linked worktree's .git file is followed). A
// detached HEAD gives the short commit. "": no repository, an odd file
// or a name with control bytes (WEB-55: the session picker's rows).
func readBranch(dir string) string {
	git := filepath.Join(dir, ".git")
	if fi, err := os.Stat(git); err == nil && !fi.IsDir() {
		b, err := os.ReadFile(git)
		if err != nil || len(b) > 4096 {
			return ""
		}
		g, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
		if !ok {
			return ""
		}
		if !filepath.IsAbs(g) {
			g = filepath.Join(dir, g)
		}
		git = g
	}
	f, err := os.Open(filepath.Join(git, "HEAD"))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	head := strings.TrimSpace(string(buf[:n]))
	if ref, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
		head = ref
	} else if len(head) >= 7 && !strings.ContainsAny(head, " /:") {
		head = head[:7]
	} else {
		return ""
	}
	for _, r := range head {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return webName(head)
}

// webProjectBranches reads the branch of each running folder.
func webProjectBranches(dirs []string) map[string]string {
	out := map[string]string{}
	for _, d := range dirs {
		if b := readBranch(d); b != "" {
			out[filepath.Clean(d)] = b
		}
	}
	return out
}
