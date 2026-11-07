package lanemove

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentlog"
	gitpkg "github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// Limits of what Unpack reads.
var (
	MaxManifest  int64 = 1 << 20 // lane.json
	MaxPackTotal int64 = 4 << 30 // everything in a pack
	MaxPackFiles       = MaxFiles + 1000
)

// Errors of Unpack.
var (
	ErrBadPack     = errors.New("this is not a lane pack termocode can read")
	ErrNewerPack   = errors.New("the pack is from a newer termocode; update termocode here first")
	ErrMissingBase = errors.New("this repository lacks commits the lane is built on; git fetch here first, then try again")
)

// Inspect reads lane.json, the first file of a pack.
func Inspect(r io.Reader) (Manifest, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return Manifest{}, ErrBadPack
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	h, err := tr.Next()
	if err != nil || h.Name != manifestName || h.Typeflag != tar.TypeReg || h.Size > MaxManifest {
		return Manifest{}, ErrBadPack
	}
	return readManifest(tr, h.Size)
}

// InspectFile is Inspect of a file.
func InspectFile(name string) (Manifest, error) {
	f, err := os.Open(name)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	return Inspect(f)
}

func readManifest(r io.Reader, size int64) (Manifest, error) {
	b, err := io.ReadAll(io.LimitReader(r, size))
	if err != nil {
		return Manifest{}, ErrBadPack
	}
	var m Manifest
	if json.Unmarshal(b, &m) != nil || m.Version < 1 {
		return Manifest{}, ErrBadPack
	}
	if m.Version > Version {
		return m, ErrNewerPack
	}
	if !validHead(m.Head) || (m.Bundle && !validBundleRef(m.BundleRef)) {
		return Manifest{}, ErrBadPack
	}
	return m, nil
}

func validHead(h string) bool {
	if len(h) != 40 && len(h) != 64 {
		return false
	}
	for _, c := range h {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validBundleRef(ref string) bool {
	return ref == "HEAD" || (strings.HasPrefix(ref, "refs/heads/") && !strings.ContainsAny(ref, " \t\n:^~?*[\\") && !strings.Contains(ref, ".."))
}

// UnpackOptions say where the lane goes.
type UnpackOptions struct {
	Repo   string // a folder of the repository here
	Folder string // the new worktree's folder (absolute; must not exist)
	Branch string // its new branch (must not exist)
	// ClaudeHome is ~/.claude here; "" = the chats in the pack stay out.
	ClaudeHome string
}

// Result is what Unpack made.
type Result struct {
	Manifest Manifest
	Folder   string
	Branch   string
	Files    int    // new files written
	Kept     int    // new files left out: a file was there already
	Chats    int    // chats copied
	Patched  bool   // the changes were applied
	PatchAt  string // the patch file kept next to the folder when it did not apply
}

// Unpack makes a new worktree from the pack in r. When the commits or
// the worktree cannot be made, nothing is left behind; a patch that does
// not apply leaves the worktree on its commit and keeps the patch next
// to it (Result.PatchAt).
func Unpack(r io.Reader, opt UnpackOptions) (Result, error) {
	if !filepath.IsAbs(opt.Folder) {
		return Result{}, fmt.Errorf("%s is not an absolute path", opt.Folder)
	}
	tmp, err := os.MkdirTemp("", "termocode-lane-*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(tmp)
	man, err := extract(r, tmp)
	if err != nil {
		return Result{}, err
	}
	res := Result{Manifest: man, Folder: opt.Folder, Branch: opt.Branch}

	// The commits.
	if man.Bundle {
		bundle := filepath.Join(tmp, bundleName)
		// No -q: with it, git 2.43 hides the "lacks these prerequisite
		// commits" line that tells a missing base from a broken pack.
		if _, err := git(opt.Repo, "bundle", "verify", bundle); err != nil {
			if strings.Contains(err.Error(), "prerequisite") || strings.Contains(err.Error(), "lacks these") {
				return Result{}, ErrMissingBase
			}
			return Result{}, fmt.Errorf("the commits in the pack could not be read: %w", err)
		}
		tmpRef := "refs/termocode/lane-move/" + man.Head[:12]
		if _, err := git(opt.Repo, "fetch", "-q", "--no-tags", bundle, "+"+man.BundleRef+":"+tmpRef); err != nil {
			return Result{}, fmt.Errorf("the commits could not be fetched from the pack: %w", err)
		}
		defer func() { _, _ = git(opt.Repo, "update-ref", "-d", tmpRef) }()
	}
	if _, err := git(opt.Repo, "cat-file", "-e", man.Head+"^{commit}"); err != nil {
		return Result{}, ErrMissingBase
	}
	if err := gitpkg.AddWorktree(opt.Repo, opt.Folder, opt.Branch, man.Head); err != nil {
		return Result{}, err
	}

	// The changes.
	if man.Patch {
		patch := filepath.Join(tmp, patchName)
		if _, err := git(opt.Folder, "apply", "--binary", "--whitespace=nowarn", patch); err == nil {
			res.Patched = true
		} else {
			keep := filepath.Clean(opt.Folder) + ".lane.patch"
			if b, rerr := os.ReadFile(patch); rerr == nil && os.WriteFile(keep, b, 0o600) == nil {
				res.PatchAt = keep
			}
		}
	}

	// The new files: never over a file that is there.
	if len(man.Files) > 0 {
		root, err := os.OpenRoot(opt.Folder)
		if err != nil {
			return res, err
		}
		for _, rel := range man.Files {
			if !safeRel(rel) {
				continue
			}
			ok, err := placeFile(root, filepath.Join(tmp, "files", filepath.FromSlash(rel)), filepath.FromSlash(rel))
			if err != nil {
				_ = root.Close()
				return res, fmt.Errorf("%s could not be written: %w", rel, err)
			}
			if ok {
				res.Files++
			} else {
				res.Kept++
			}
		}
		_ = root.Close()
	}

	// The chats, with the old folder's path made the new one.
	if opt.ClaudeHome != "" && len(man.Chats) > 0 {
		dst := filepath.Join(opt.ClaudeHome, "projects", agentlog.Slug(opt.Folder))
		if err := os.MkdirAll(dst, 0o700); err == nil {
			for _, id := range man.Chats {
				if !safeID(id) {
					continue
				}
				b, err := os.ReadFile(filepath.Join(tmp, "claude", id+".jsonl"))
				if err != nil {
					continue
				}
				b = rewritePath(b, man.Path, opt.Folder)
				f, err := os.OpenFile(filepath.Join(dst, id+".jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if err != nil {
					continue // a chat with that id is here already
				}
				_, werr := f.Write(b)
				if cerr := f.Close(); werr == nil && cerr == nil {
					res.Chats++
				}
			}
		}
	}
	return res, nil
}

// placeFile copies src to rel inside root; false when rel is there.
func placeFile(root *os.Root, src, rel string) (bool, error) {
	in, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return false, err
	}
	if d := filepath.Dir(rel); d != "." {
		if err := root.MkdirAll(d, 0o755); err != nil {
			return false, err
		}
	}
	perm := os.FileMode(0o644)
	if info.Mode()&0o100 != 0 {
		perm = 0o755
	}
	out, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, werr := io.Copy(out, in)
	if cerr := out.Close(); werr == nil {
		werr = cerr
	}
	return werr == nil, werr
}

// rewritePath makes the JSON strings that start with the old folder
// start with the new one (Claude Code keeps "cwd" in every line).
func rewritePath(b []byte, from, to string) []byte {
	if from == "" || from == to {
		return b
	}
	q := func(s string) []byte {
		j, _ := json.Marshal(s)
		return j[:len(j)-1] // the opening quote and the text, no closing quote
	}
	b = bytes.ReplaceAll(b, append(q(from), '"'), append(q(to), '"'))
	return bytes.ReplaceAll(b, append(q(from), '/'), append(q(to), '/'))
}

// extract reads the pack into dir: only the names a pack has, only
// plain files, inside the limits.
func extract(r io.Reader, dir string) (Manifest, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return Manifest{}, ErrBadPack
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	h, err := tr.Next()
	if err != nil || h.Name != manifestName || h.Typeflag != tar.TypeReg || h.Size > MaxManifest {
		return Manifest{}, ErrBadPack
	}
	man, err := readManifest(tr, h.Size)
	if err != nil {
		return Manifest{}, err
	}
	want := map[string]bool{}
	if man.Bundle {
		want[bundleName] = true
	}
	if man.Patch {
		want[patchName] = true
	}
	for _, f := range man.Files {
		if safeRel(f) {
			want[filesDir+f] = true
		}
	}
	for _, id := range man.Chats {
		if safeID(id) {
			want[claudeDir+id+".jsonl"] = true
		}
	}
	var total int64
	n := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Manifest{}, ErrBadPack
		}
		n++
		total += h.Size
		if h.Typeflag != tar.TypeReg || !want[h.Name] || h.Size < 0 || total > MaxPackTotal || n > MaxPackFiles {
			return Manifest{}, fmt.Errorf("%w (%s)", ErrBadPack, h.Name)
		}
		delete(want, h.Name)
		dst := filepath.Join(dir, filepath.FromSlash(h.Name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return Manifest{}, err
		}
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600|os.FileMode(h.Mode&0o100))
		if err != nil {
			return Manifest{}, err
		}
		_, werr := io.Copy(f, io.LimitReader(tr, h.Size))
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return Manifest{}, werr
		}
	}
	if len(want) > 0 {
		return Manifest{}, fmt.Errorf("%w (files are missing)", ErrBadPack)
	}
	return man, nil
}

// Place is where a lane goes in the repository of dir: the folder under
// the worktree folder (setting = config worktree_dir, home = the home
// folder) and a branch name that is free. name overrides the pack's.
type Place struct {
	Repo, Common, Folder, Branch string
}

// PlaceFor picks the folder and branch for man in the repository of dir.
func PlaceFor(dir string, man Manifest, name, setting, home string) (Place, error) {
	list, common, err := gitpkg.ListWorktrees(dir)
	if err != nil {
		return Place{}, ErrNotRepo
	}
	main := dir
	for _, w := range list {
		if w.Main && !w.Bare {
			main = w.Path
		}
	}
	branch := strings.TrimSpace(name)
	if branch == "" {
		branch = man.Branch
	}
	if branch == "" {
		branch = "lane/" + worktreeform.FolderName(man.Title())
	}
	if err := gitpkg.CheckBranchName(dir, branch); err != nil {
		return Place{}, err
	}
	base := worktreeform.BaseDir(main, setting, home)
	free := func(b string) bool {
		_, err := os.Lstat(worktreeform.Folder(base, b))
		return !gitpkg.BranchExists(dir, b) && errors.Is(err, os.ErrNotExist)
	}
	pick := branch
	for i := 1; !free(pick); i++ {
		if i > 50 {
			return Place{}, fmt.Errorf("no free name for %s", branch)
		}
		pick = fmt.Sprintf("%s-moved", branch)
		if i > 1 {
			pick = fmt.Sprintf("%s-moved-%d", branch, i)
		}
	}
	return Place{Repo: dir, Common: common, Folder: worktreeform.Folder(base, pick), Branch: pick}, nil
}

// Record writes what termocode knows about the moved lane into
// worktrees.json: its name, agent and notes, and a note where it came
// from.
func Record(common string, res Result, note string) error {
	if common == "" {
		return nil
	}
	man := res.Manifest
	return worktrees.Update(common, func(mm map[string]worktrees.Meta) {
		md := mm[res.Folder]
		if t, err := worktrees.CleanNote(man.Name, nil); err == nil && len(t) <= 80 {
			md.Name = t
		}
		if safeID(man.Agent) {
			md.Agent = man.Agent
		}
		md.Notes = nil
		for _, n := range man.Notes { // text from the other machine: cleaned again
			if t, err := worktrees.CleanNote(n.Text, nil); err == nil {
				md.Notes = append(md.Notes, worktrees.Note{Text: t, At: n.At})
			}
		}
		md.Notes = append(md.Notes, worktrees.Note{Text: note, At: man.PackedAt})
		if len(md.Notes) > worktrees.MaxNotes {
			md.Notes = md.Notes[len(md.Notes)-worktrees.MaxNotes:]
		}
		if md.Created.IsZero() {
			md.Created = man.PackedAt
		}
		mm[res.Folder] = md
	})
}

// FromNote is the note a moved lane gets.
func FromNote(man Manifest) string {
	from := man.Path
	if man.Host != "" {
		from = man.Host + ":" + from
	}
	return "moved here from " + from
}

// Dir is the folder packs are kept in: $XDG_DATA_HOME/termocode/lanes,
// else ~/.local/share/termocode/lanes.
func Dir(getenv func(string) string, home string) string {
	if x := getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "termocode", "lanes")
	}
	return filepath.Join(home, ".local", "share", "termocode", "lanes")
}

// FileName is the name of a new pack of man: <name>-<time>.tlane.
func FileName(man Manifest) string {
	n := worktreeform.FolderName(man.Title())
	var b strings.Builder
	for _, c := range n {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-.")
	if s == "" {
		s = "lane"
	}
	return s + "-" + man.PackedAt.UTC().Format("20060102-150405") + Ext
}

// Into is where UnpackFile puts a lane.
type Into struct {
	Dir        string // a folder of the repository here
	Name       string // the branch to make ("" = the pack's)
	Setting    string // config worktree_dir
	Home       string // the home folder
	ClaudeHome string // ~/.claude here ("" = no chats)
}

// UnpackFile unpacks the pack file name into a new worktree of the
// repository of in.Dir and records it in worktrees.json.
func UnpackFile(name string, in Into) (Result, error) {
	man, err := InspectFile(name)
	if err != nil {
		return Result{}, err
	}
	pl, err := PlaceFor(in.Dir, man, in.Name, in.Setting, in.Home)
	if err != nil {
		return Result{}, err
	}
	f, err := os.Open(name)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()
	res, err := Unpack(f, UnpackOptions{Repo: in.Dir, Folder: pl.Folder, Branch: pl.Branch, ClaudeHome: in.ClaudeHome})
	if err != nil {
		return res, err
	}
	if err := Record(pl.Common, res, FromNote(res.Manifest)); err != nil {
		return res, fmt.Errorf("the lane is unpacked, but its notes could not be kept: %w", err)
	}
	return res, nil
}

// MetaOf is what termocode knows about the worktree dir is in.
func MetaOf(dir string) worktrees.Meta {
	out, err := git(dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return worktrees.Meta{}
	}
	f := strings.Split(strings.TrimSpace(out), "\n")
	if len(f) != 2 {
		return worktrees.Meta{}
	}
	mm, _ := worktrees.Load(f[1])
	return mm[f[0]]
}

// Summary is one line about what Unpack made.
func (r Result) Summary() string {
	var parts []string
	parts = append(parts, "branch "+r.Branch)
	if r.Manifest.Commits > 0 {
		parts = append(parts, plural(r.Manifest.Commits, "commit"))
	}
	switch {
	case r.Patched:
		parts = append(parts, plural(r.Manifest.Changed, "changed file"))
	case r.PatchAt != "":
		parts = append(parts, "changes did NOT apply (kept in "+r.PatchAt+")")
	}
	if r.Files > 0 {
		parts = append(parts, plural(r.Files, "new file"))
	}
	if r.Kept > 0 {
		parts = append(parts, plural(r.Kept, "file")+" already there (kept)")
	}
	if r.Chats > 0 {
		parts = append(parts, plural(r.Chats, "agent chat"))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
