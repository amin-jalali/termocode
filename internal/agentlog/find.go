package agentlog

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Homes are the CLIs' own folders: ~/.claude and ~/.codex, or what
// CLAUDE_CONFIG_DIR / CODEX_HOME name.
type Homes struct {
	Claude, Codex string
}

// HomesFrom builds Homes from the home folder and an env lookup.
func HomesFrom(home string, getenv func(string) string) Homes {
	h := Homes{Claude: filepath.Join(home, ".claude"), Codex: filepath.Join(home, ".codex")}
	if v := getenv("CLAUDE_CONFIG_DIR"); v != "" {
		h.Claude = v
	}
	if v := getenv("CODEX_HOME"); v != "" {
		h.Codex = v
	}
	return h
}

// OnOpen, when set, is told every file and folder this package opens
// (tests prove that nothing is opened while the setting is off).
var OnOpen func(path string)

func observe(path string) {
	if f := OnOpen; f != nil {
		f(path)
	}
}

func init() {
	openFile = func(path string) (*os.File, error) {
		observe(path)
		return os.Open(path)
	}
}

// readDir is os.ReadDir, observed.
func readDir(dir string) ([]os.DirEntry, error) {
	observe(dir)
	return os.ReadDir(dir)
}

// Slug is Claude Code's folder name for a working folder: every
// character that is not an ASCII letter or digit becomes a dash.
func Slug(dir string) string {
	b := []byte(dir)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// Ref is one session file.
type Ref struct {
	CLI     CLI
	Path    string
	ModTime time.Time
	Size    int64
	// Cwd is the folder the session ran in when the listing knows it
	// (Codex: from the first line).
	Cwd string
}

// claudeDirFiles lists the session files of one projects/<slug> folder.
func claudeDirFiles(dir string) []Ref {
	ents, err := readDir(dir)
	if err != nil {
		return nil
	}
	var out []Ref
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".jsonl") || strings.HasPrefix(n, "agent-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Ref{CLI: Claude, Path: filepath.Join(dir, n), ModTime: info.ModTime(), Size: info.Size()})
	}
	return out
}

// codexFiles lists every Codex rollout file, newest first, without
// reading them.
func (h Homes) codexFiles() []Ref {
	root := filepath.Join(h.Codex, "sessions")
	var out []Ref
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		ents, err := readDir(dir)
		if err != nil {
			return
		}
		for _, e := range ents {
			p := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if depth < 4 {
					walk(p, depth+1)
				}
				continue
			}
			if !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
				out = append(out, Ref{CLI: Codex, Path: p, ModTime: info.ModTime(), Size: info.Size()})
			}
		}
	}
	walk(root, 0)
	sortNewest(out)
	return out
}

func sortNewest(rs []Ref) {
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].ModTime.After(rs[j].ModTime) })
}

// maxMetaLine is how much of a Codex file's first line is read for its
// cwd (the line can hold the CLI's instructions).
const maxMetaLine = 256 << 10

// codexCwd is the cwd in a Codex file's session_meta line ("" if none).
func codexCwd(path string) string {
	f, err := openFile(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(io.LimitReader(f, maxMetaLine), 32<<10).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return ""
	}
	var l struct {
		Type    string `json:"type"`
		Payload struct {
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != "session_meta" {
		return ""
	}
	return l.Payload.Cwd
}

// Latest is the newest session file of cli for the folder cwd that
// changed at or after since.
func (h Homes) Latest(cli CLI, cwd string, since time.Time) (Ref, bool) {
	cwd = filepath.Clean(cwd)
	switch cli {
	case Claude:
		rs := claudeDirFiles(filepath.Join(h.Claude, "projects", Slug(cwd)))
		sortNewest(rs)
		if len(rs) > 0 && !rs[0].ModTime.Before(since) {
			return rs[0], true
		}
	case Codex:
		for _, r := range h.codexFiles() {
			if r.ModTime.Before(since) {
				break // newest first: the rest are older still
			}
			if c := codexCwd(r.Path); c != "" && filepath.Clean(c) == cwd {
				r.Cwd = c
				return r, true
			}
		}
	}
	return Ref{}, false
}

// InProject reports whether dir is the project root, a folder under it,
// or one of its worktrees beside it (<root>.worktrees/…).
func InProject(root, dir string) bool {
	root, dir = filepath.Clean(root), filepath.Clean(dir)
	sep := string(filepath.Separator)
	return dir == root || strings.HasPrefix(dir, root+sep) || strings.HasPrefix(dir, root+".worktrees"+sep)
}

// Files lists the session files of the project at root from both CLIs,
// newest first. Claude Code folders are matched by slug and then by the
// cwd inside the file is checked by the reader (a slug can stand for
// more than one folder); Codex files by the cwd of their first line.
func (h Homes) Files(root string) []Ref {
	root = filepath.Clean(root)
	slug := Slug(root)
	var out []Ref
	projects := filepath.Join(h.Claude, "projects")
	if ents, err := readDir(projects); err == nil {
		for _, e := range ents {
			n := e.Name()
			if !e.IsDir() || !(n == slug || strings.HasPrefix(n, slug+"-")) {
				continue
			}
			for _, r := range claudeDirFiles(filepath.Join(projects, n)) {
				if n != slug { // another folder with the same start: check it
					c := claudeCwd(r.Path)
					if c == "" || !InProject(root, c) {
						continue
					}
					r.Cwd = c
				}
				out = append(out, r)
			}
		}
	}
	for _, r := range h.codexFiles() {
		if c := codexCwd(r.Path); c != "" && InProject(root, c) {
			r.Cwd = c
			out = append(out, r)
		}
	}
	sortNewest(out)
	return out
}

// claudeCwd is the first cwd in a Claude Code file (the first 64 KB).
func claudeCwd(path string) string {
	f, err := openFile(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 64<<10))
	sc.Buffer(make([]byte, 0, 16<<10), 64<<10)
	for sc.Scan() {
		var l struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &l) == nil && l.Cwd != "" {
			return l.Cwd
		}
	}
	return ""
}

// statFile is a file's size (not observed: no file is opened).
func statFile(path string) (int64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}
