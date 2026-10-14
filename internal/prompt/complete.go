package prompt

import (
	"os"
	"path/filepath"
	"strings"
)

// SetCompleter makes Tab replace the value with complete(value), e.g.
// CompletePath.
func (m *Model) SetCompleter(complete func(string) string) { m.complete = complete }

// CompletePath completes the last part of a file path (~ is the home
// folder): to the one matching name (a folder gets "/"), or to the
// longest start that all matching names share. Hidden names match only
// when the part starts with ".". No match: s is returned unchanged.
func CompletePath(s string) string {
	full := s
	if s == "~" || strings.HasPrefix(s, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return s
		}
		full = home + strings.TrimPrefix(s, "~")
	}
	dir, part := filepath.Split(full)
	if dir == "" {
		dir = "."
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return s
	}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if !strings.HasPrefix(n, part) || (strings.HasPrefix(n, ".") && !strings.HasPrefix(part, ".")) {
			continue
		}
		if e.IsDir() {
			n += "/"
		} else if e.Type()&os.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(dir, n)); err == nil && fi.IsDir() {
				n += "/"
			}
		}
		names = append(names, n)
	}
	if len(names) == 0 {
		return s
	}
	common := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, common) {
			common = common[:len(common)-1]
		}
	}
	if len(names) > 1 {
		common = strings.TrimSuffix(common, "/")
	}
	return s[:len(s)-len(part)] + common
}
