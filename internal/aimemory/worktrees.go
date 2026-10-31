package aimemory

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SharedRoot returns a linked worktree's main checkout. Verify Git's
// registration backlink before reading outside root; a .git file alone is
// not proof. Separate external git dirs and bare repos keep local memory.
func SharedRoot(root string) string {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return ""
	}
	gitfile := filepath.Join(root, ".git")
	pointer := metadataText(gitfile)
	admin, ok := strings.CutPrefix(pointer, "gitdir: ")
	if !ok {
		return ""
	}
	resolve := func(base, path string) string {
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		p, e := filepath.EvalSymlinks(path)
		if e != nil {
			return ""
		}
		p, e = filepath.Abs(p)
		if e != nil {
			return ""
		}
		return p
	}
	admin = resolve(root, admin)
	if admin == "" {
		return ""
	}
	backlink := metadataText(filepath.Join(admin, "gitdir"))
	if backlink == "" || resolve(admin, backlink) != gitfile {
		return ""
	}
	commonRef := metadataText(filepath.Join(admin, "commondir"))
	if commonRef == "" {
		return ""
	}
	common := resolve(admin, commonRef)
	if common == "" || filepath.Base(common) != ".git" || filepath.Dir(admin) != filepath.Join(common, "worktrees") {
		return ""
	}
	main := filepath.Dir(common)
	if main == root || metadataText(filepath.Join(common, "HEAD")) == "" {
		return ""
	}
	return main
}

func metadataText(path string) string {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 64<<10 {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func loadUnlinked(root string) ([]Lesson, error) {
	for _, dir := range []string{filepath.Join(root, ".termocode"), Dir(root)} {
		st, err := os.Lstat(dir)
		if os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			return nil, nil
		}
	}
	return Load(root)
}

// LoadForWorktree adds the main checkout's lessons for reads only. Local
// IDs win, missing shared files are harmless, and neither file is changed.
func LoadForWorktree(root string) ([]Lesson, error) {
	local, err := loadUnlinked(root)
	if err != nil {
		return nil, err
	}
	main := SharedRoot(root)
	if main == "" {
		return local, nil
	}
	shared, err := loadUnlinked(main)
	if err != nil {
		return local, nil
	}
	localIDs := map[string]bool{}
	for _, l := range local {
		localIDs[l.ID] = true
	}
	kept := shared[:0]
	for _, l := range shared {
		if !localIDs[l.ID] {
			kept = append(kept, l)
		}
	}
	return Append(kept, local...), nil
}
