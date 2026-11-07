// Package worktreeform is the pure part of Worktree: New… (WTX-5..7):
// the branch drafted from the typed name, the folder the worktree goes
// to (<repo>.worktrees/<name>, setting worktree_dir), and the repo file
// .termocode/worktrees.json that lists the files to copy (include) or
// link (shared) into each new worktree and the setup commands.
//
// Copied files may hold secrets (.env): their contents are never read
// beyond the copy, and nothing here logs a name or a byte. A path that
// leads out of the repository (.., an absolute path, a symlink to
// elsewhere) is refused, in the source and in the new worktree.
package worktreeform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// MaxBranch is the longest drafted branch name.
const MaxBranch = 60

// BranchFromName drafts a branch from the typed name: lower case, runs of
// other characters as one -, the / of a prefix kept, no part that starts
// with . or ends with .lock. "" when nothing is left.
func BranchFromName(name string) string {
	var parts []string
	for _, p := range strings.Split(name, "/") {
		var b strings.Builder
		dash := false
		for _, r := range strings.ToLower(p) {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				if dash && b.Len() > 0 {
					b.WriteByte('-')
				}
				b.WriteRune(r)
				dash = false
				continue
			}
			dash = true
		}
		if s := b.String(); s != "" {
			parts = append(parts, s)
		}
	}
	out := strings.Join(parts, "/")
	if len(out) > MaxBranch {
		out = strings.TrimRight(cutRunes(out, MaxBranch), "-/")
	}
	return out
}

// BranchFromIssue is the branch of a worktree made from issue n: the
// number, then the title as a slug (no /), cut on a word border;
// "issue-<n>" when the title gives nothing.
func BranchFromIssue(n int, title string) string {
	num := strconv.Itoa(n)
	slug := BranchFromName(strings.ReplaceAll(title, "/", " "))
	if slug == "" {
		return "issue-" + num
	}
	out := num + "-" + slug
	for len(out) > MaxBranch {
		i := strings.LastIndexByte(out, '-')
		if i <= len(num) {
			out = strings.TrimRight(cutRunes(out, MaxBranch), "-")
			break
		}
		out = out[:i]
	}
	return out
}

// cutRunes is s cut to at most n bytes on a rune border.
func cutRunes(s string, n int) string {
	var b strings.Builder
	for _, c := range s {
		if b.Len()+len(string(c)) > n {
			break
		}
		b.WriteRune(c)
	}
	return b.String()
}

// FolderName is the folder of branch: its / as -.
func FolderName(branch string) string { return strings.ReplaceAll(branch, "/", "-") }

// BaseDir is the folder user worktrees go to: setting worktree_dir, or
// "<main>.worktrees" next to the main checkout. ~ is home; a relative
// path is relative to the main checkout.
func BaseDir(main, setting, home string) string {
	s := strings.TrimSpace(setting)
	switch {
	case s == "":
		return filepath.Clean(main) + ".worktrees"
	case s == "~" || strings.HasPrefix(s, "~/"):
		return filepath.Join(home, strings.TrimPrefix(s, "~"))
	case filepath.IsAbs(s):
		return filepath.Clean(s)
	}
	return filepath.Join(main, s)
}

// Folder is the worktree folder of branch under base.
func Folder(base, branch string) string { return filepath.Join(base, FolderName(branch)) }

// ConfigFile is the repo file of Worktree: New… (relative to the main
// checkout).
const ConfigFile = ".termocode/worktrees.json"

// Config is .termocode/worktrees.json.
type Config struct {
	Include []string `json:"include,omitempty"` // files copied into each new worktree
	Shared  []string `json:"shared,omitempty"`  // files or folders linked to the main checkout's
	Setup   []string `json:"setup,omitempty"`   // shell lines run in the new worktree (after trust)
	// Env adds plain variables to the setup (warm.go).
	Env map[string]string `json:"env,omitempty"`
	// Warm keeps a copy of what the setup made for the next worktree (warm.go).
	Warm *WarmConfig `json:"warm,omitempty"`
}

// LoadConfig reads the config of the main checkout top (comments
// allowed). A missing file is an empty Config and hash "". hash is the
// sha256 of the file's bytes: the setup trust is bound to it.
func LoadConfig(top string) (c Config, hash string, err error) {
	b, err := os.ReadFile(filepath.Join(top, filepath.FromSlash(ConfigFile)))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, "", nil
	}
	if err != nil {
		return Config{}, "", err
	}
	if err := json.Unmarshal(jsonc.Strip(b), &c); err != nil {
		return Config{}, "", fmt.Errorf("%s: %v", ConfigFile, err)
	}
	sum := sha256.Sum256(b)
	return c, hex.EncodeToString(sum[:]), nil
}

// SetupTrusted says whether the stored trust (a hash) covers the config
// with hash.
func SetupTrusted(hash, trusted string) bool { return hash != "" && hash == trusted }

// Kind is what happens to a File.
type Kind int

const (
	Copy Kind = iota // a copy (include)
	Link             // a link to the main checkout's file or folder (shared)
)

// MaxCopy is the largest file that is copied.
const MaxCopy = 20 << 20

// File is one row of the plan.
type File struct {
	Rel  string // as written in the config (slash separated)
	Kind Kind
	Size int64
	Skip string // why it is not copied / linked ("" = it is)
	src  string // the real source path
}

// PlanFiles is what include and shared will do from the main checkout
// top. Empty entries are dropped.
func PlanFiles(top string, c Config) []File {
	realTop, err := filepath.EvalSymlinks(top)
	if err != nil {
		realTop = top
	}
	var out []File
	add := func(rel string, k Kind) {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			return
		}
		f := File{Rel: rel, Kind: k}
		f.src, f.Skip = source(realTop, rel)
		if f.Skip == "" {
			fi, err := os.Stat(f.src)
			switch {
			case err != nil:
				f.Skip = "not there"
			case k == Copy && fi.IsDir():
				f.Skip = "a folder (list it under shared)"
			case k == Copy && !fi.Mode().IsRegular():
				f.Skip = "not a plain file"
			case k == Copy && fi.Size() > MaxCopy:
				f.Skip = "larger than 20 MB"
			default:
				f.Size = fi.Size()
			}
		}
		out = append(out, f)
	}
	for _, r := range c.Include {
		add(r, Copy)
	}
	for _, r := range c.Shared {
		add(r, Link)
	}
	return out
}

// source is the real path of rel inside realTop, or why it is refused.
func source(realTop, rel string) (string, string) {
	p, ok := inside(realTop, rel)
	if !ok {
		return "", "outside the repository"
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", "not there"
	}
	if !within(realTop, real) {
		return "", "points outside the repository"
	}
	return real, ""
}

// inside joins rel to root when it stays inside (no .., not absolute).
func inside(root, rel string) (string, bool) {
	r := filepath.FromSlash(rel)
	if filepath.IsAbs(r) || strings.HasPrefix(rel, "/") || filepath.VolumeName(r) != "" {
		return "", false
	}
	c := filepath.Clean(r)
	if c == "." || c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Join(root, c), true
}

// within says whether p is root or below it.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ApplyFiles copies and links the plan's files into the new worktree
// dest. A file that is there already (git checked it out) is kept; a
// path whose folder leads out of dest is refused. The rows come back
// with Skip set for what did not happen.
func ApplyFiles(top, dest string, files []File) []File {
	realDest, err := filepath.EvalSymlinks(dest)
	if err != nil {
		realDest = dest
	}
	out := make([]File, 0, len(files))
	for _, f := range files {
		if f.Skip == "" {
			f.Skip = apply(realDest, f)
		}
		out = append(out, f)
	}
	return out
}

func apply(realDest string, f File) string {
	dst, ok := inside(realDest, f.Rel)
	if !ok {
		return "outside the worktree"
	}
	if _, err := os.Lstat(dst); err == nil {
		return "already in the worktree"
	}
	// The folders on the way: each one that exists must stay inside
	// (a checked-out symlink could lead elsewhere).
	parent := filepath.Dir(dst)
	if real, err := realExisting(parent); err != nil || !within(realDest, real) {
		return "its folder leads outside the worktree"
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "could not make its folder"
	}
	if f.Kind == Link {
		if err := os.Symlink(f.src, dst); err != nil {
			return "could not link it"
		}
		return ""
	}
	if err := copyFile(f.src, dst); err != nil {
		_ = os.Remove(dst)
		return "could not copy it"
	}
	return ""
}

// realExisting is the real path of p's deepest existing folder, with the
// missing rest joined on.
func realExisting(p string) (string, error) {
	rest := ""
	for {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(real, rest), nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", errors.New("no folder")
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(in, MaxCopy+1)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Counts are the rows that will happen: copies and links.
func Counts(files []File) (copies, links int) {
	for _, f := range files {
		switch {
		case f.Skip != "":
		case f.Kind == Copy:
			copies++
		default:
			links++
		}
	}
	return copies, links
}
