package search

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FileCount is one file that contains a literal needle and how many times.
type FileCount struct {
	Path  string // absolute
	Count int
}

// CountLiteral finds every file under `dirs` that contains `needle` (exact
// bytes, case-sensitive) and counts the occurrences in each. It backs the
// Replace-in-Workspace confirm dialog: the counts are what gets replaced.
//
// ripgrep picks the candidate files when installed (so .gitignore is
// honoured); otherwise the fallback walker's skip rules apply. Counting is
// always done here in Go so the numbers match ReplaceLiteralInFile exactly.
func CountLiteral(dirs []string, needle string) ([]FileCount, error) {
	if needle == "" {
		return nil, nil
	}
	useRg := Available()
	var out []FileCount
	var lastErr error
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		var files []string
		var err error
		if useRg {
			files, err = rgFilesWithMatches(dir, needle)
		} else {
			files, err = walkFiles(dir)
		}
		if err != nil {
			lastErr = err
			continue
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil || isBinary(data) {
				continue
			}
			if n := bytes.Count(data, []byte(needle)); n > 0 {
				out = append(out, FileCount{Path: f, Count: n})
			}
		}
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

// rgFilesWithMatches lists files under dir containing the literal needle,
// as absolute paths.
func rgFilesWithMatches(dir, needle string) ([]string, error) {
	cmd := exec.Command("rg", "--files-with-matches", "--null", "--fixed-strings", "--case-sensitive", "--", needle)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" && len(out) == 0 {
			return nil, errors.New(firstLine(msg))
		}
		if len(out) == 0 {
			return nil, err
		}
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, absJoin(dir, f))
		}
	}
	return files, nil
}

// walkFiles lists every searchable file under dir with the fallback
// walker's rules (pruned dirs, size cap). Binary files are filtered later.
func walkFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && skipSearchDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if info, e := d.Info(); e == nil && info.Size() > maxFallbackFileSize {
			return nil
		}
		if abs, e := filepath.Abs(path); e == nil {
			path = abs
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

// ReplaceLiteralInFile replaces every occurrence of find with repl in the
// file at path, keeping its permissions. Returns the number of replacements
// made (0 leaves the file untouched).
func ReplaceLiteralInFile(path, find, repl string) (int, error) {
	if find == "" {
		return 0, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n := bytes.Count(data, []byte(find))
	if n == 0 {
		return 0, nil
	}
	out := bytes.ReplaceAll(data, []byte(find), []byte(repl))
	if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil {
		return 0, err
	}
	return n, nil
}
