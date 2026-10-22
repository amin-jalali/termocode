package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// maxConfigFile is the biggest editor file a reader opens (same cap as jsonFile).
const maxConfigFile = 1 << 20

// readInside reads root/rel. A missing file gives found=false and no reason.
// A file that cannot be used (a link that leaves root, too big, not a regular
// file) gives found=false and a reason for the "not imported" row. Readers
// never write.
func readInside(root, rel string) (data []byte, found bool, reason string) {
	p, reason := resolveInside(root, rel)
	if p == "" {
		return nil, false, reason
	}
	info, err := os.Stat(p)
	if err != nil {
		return nil, false, "cannot be read: " + err.Error()
	}
	if !info.Mode().IsRegular() {
		return nil, false, "is not a regular file; skipped"
	}
	if info.Size() > maxConfigFile {
		return nil, false, "is larger than 1 MB; skipped"
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false, "cannot be read: " + err.Error()
	}
	return b, true, ""
}

// resolveInside returns the real path of root/rel when it exists and stays
// inside root. It returns "" and a reason (or "" when simply missing).
func resolveInside(root, rel string) (string, string) {
	p := filepath.Join(root, rel)
	if _, err := os.Lstat(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ""
		}
		return "", "cannot be read: " + err.Error()
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "folder cannot be read: " + err.Error()
	}
	realP, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", "is a broken link; skipped"
	}
	if realP != realRoot && !strings.HasPrefix(realP, realRoot+string(filepath.Separator)) {
		return "", "is a link that leads outside the editor folder; skipped"
	}
	return realP, ""
}

// listInside lists the entries of root/rel that are real files or folders.
// Links are skipped and counted in skipped (their names), so the caller can
// report them.
func listInside(root, rel string) (names []string, skippedLinks []string) {
	dir, _ := resolveInside(root, rel)
	if dir == "" {
		return nil, nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, e := range ents {
		if e.Type()&os.ModeSymlink != 0 {
			skippedLinks = append(skippedLinks, e.Name())
			continue
		}
		names = append(names, e.Name())
	}
	return names, skippedLinks
}
