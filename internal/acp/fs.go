package acp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/textdiff"
)

// maxFile caps a file the agent reads or writes.
const maxFile = 8 << 20

// Inside resolves path (absolute, as the protocol asks) and reports an
// error unless it is inside root, also after symlinks. A file or
// folders that do not exist yet are fine: the deepest existing parent
// is checked.
func Inside(root, path string) (string, error) {
	if root == "" {
		return "", errors.New("no workspace")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("not an absolute path: %q", path)
	}
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	p := filepath.Clean(path)
	abs, err := evalExisting(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(r, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("outside the workspace: %s", path)
	}
	return abs, nil
}

// evalExisting resolves the symlinks of the deepest existing parent of
// p and puts the missing tail back on.
func evalExisting(p string) (string, error) {
	tail := ""
	cur := p
	for {
		res, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(res, tail), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if _, lerr := os.Lstat(cur); lerr == nil {
			// It is there but does not resolve: a dangling link, whose
			// target a write would create anywhere.
			return "", fmt.Errorf("a broken link: %s", cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil
		}
		tail = filepath.Join(filepath.Base(cur), tail)
		cur = parent
	}
}

// ReadInside reads a text file inside root: from 1-based line (0: the
// start), at most limit lines (0: all).
func ReadInside(root, path string, line, limit int) (string, error) {
	p, err := Inside(root, path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if st.Size() > maxFile {
		return "", fmt.Errorf("file too big (%d bytes)", st.Size())
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	text := string(b)
	if line <= 1 && limit <= 0 {
		return text, nil
	}
	lines := strings.SplitAfter(text, "\n")
	start := max(line-1, 0)
	if start >= len(lines) {
		return "", nil
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	return strings.Join(lines[start:end], ""), nil
}

// WriteInside writes a text file inside root (folders made as needed;
// an existing file keeps its mode).
func WriteInside(root, path, content string) error {
	if len(content) > maxFile {
		return fmt.Errorf("content too big (%d bytes)", len(content))
	}
	p, err := Inside(root, path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(p); err == nil {
		if st.IsDir() {
			return fmt.Errorf("%s is a folder", path)
		}
		mode = st.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), mode)
}

// DiffPreview is a short unified view of old → new for a card: at most
// maxLines rows, then a "… N more lines" line.
func DiffPreview(oldText, newText string, maxLines int) string {
	d := textdiff.Compute(oldText, newText)
	var b strings.Builder
	shown := 0
	for _, r := range d.Rows {
		if shown == maxLines {
			fmt.Fprintf(&b, "… %d more lines", len(d.Rows)-shown)
			return b.String()
		}
		mark := " "
		switch r.Kind {
		case textdiff.Added:
			mark = "+"
		case textdiff.Removed:
			mark = "-"
		}
		b.WriteString(mark + r.Text + "\n")
		shown++
	}
	return strings.TrimRight(b.String(), "\n")
}

// diffCounts are the added and removed lines of old → new.
func diffCounts(oldText, newText string) (int, int) {
	d := textdiff.Compute(oldText, newText)
	return d.Added, d.Removed
}
