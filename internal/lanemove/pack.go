// Package lanemove moves a lane (a worktree and the work in it) from
// one machine to another: laptop to server, server to laptop.
//
// Pack writes one file (a .tlane: tar + gzip) with everything a lane
// needs to go on somewhere else:
//
//   - lane.json      what is in the pack (Manifest), always first
//   - branch.bundle  the commits of the branch the other side may not
//     have (`git bundle`, without what the remotes already have)
//   - changes.patch  the changes not committed yet (`git diff --binary
//     HEAD`: staged and not staged)
//   - files/…        the new files git does not track yet (ignored files
//     stay; big ones are left out and listed)
//   - claude/…       the Claude Code chats of the folder, so `claude
//     --resume` goes on there (only when the caller passes the home)
//
// Unpack makes a NEW worktree from a pack; it never writes over a
// folder, a branch or a file that is there already. Nothing on the
// machine the lane came from is changed: the old lane stays until the
// user archives it.
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
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// Ext is the file ending of a pack.
const Ext = ".tlane"

// Version is the pack format this build writes and reads.
const Version = 1

// The names inside a pack.
const (
	manifestName = "lane.json"
	bundleName   = "branch.bundle"
	patchName    = "changes.patch"
	filesDir     = "files/"
	claudeDir    = "claude/"
)

// Limits of what goes into a pack (new files and chats; the bundle and
// the patch are what git makes).
var (
	MaxFileSize   int64 = 10 << 20 // one new file
	MaxFilesTotal int64 = 50 << 20 // all new files
	MaxFiles            = 2000     // new files
	MaxChatsTotal int64 = 50 << 20 // all chats
)

// Manifest is lane.json: what the pack holds.
type Manifest struct {
	Version  int       `json:"version"`
	Name     string    `json:"name"`             // the lane's name (the worktree's name or folder)
	Branch   string    `json:"branch,omitempty"` // "" = detached
	Head     string    `json:"head"`             // the commit the lane is on
	Host     string    `json:"host,omitempty"`   // the machine it was packed on
	Path     string    `json:"path"`             // its folder there
	PackedAt time.Time `json:"packed_at"`

	Bundle    bool     `json:"bundle,omitempty"`     // branch.bundle is in the pack
	BundleRef string   `json:"bundle_ref,omitempty"` // the ref inside it
	Commits   int      `json:"commits,omitempty"`    // commits in the bundle
	Patch     bool     `json:"patch,omitempty"`      // changes.patch is in the pack
	Changed   int      `json:"changed,omitempty"`    // files the patch changes
	Files     []string `json:"files,omitempty"`      // new files (slash paths)
	Skipped   []string `json:"skipped,omitempty"`    // new files left out (too big, not plain files)
	Chats     []string `json:"chats,omitempty"`      // Claude Code session ids

	Agent string           `json:"agent,omitempty"` // the agent preset of the lane
	Notes []worktrees.Note `json:"notes,omitempty"` // the notes agents left on it
}

// Title is the name to show.
func (m Manifest) Title() string {
	if m.Name != "" {
		return m.Name
	}
	if m.Branch != "" {
		return m.Branch
	}
	return "lane"
}

// PackOptions are what Pack takes besides the folder.
type PackOptions struct {
	// ClaudeHome is ~/.claude (or CLAUDE_CONFIG_DIR); "" = the chats
	// stay (the user did not allow reading them).
	ClaudeHome string
	// Meta is what termocode knows about the worktree (name, agent,
	// notes); the zero Meta is fine.
	Meta worktrees.Meta
	// Host is the name of this machine ("" = os.Hostname).
	Host string
	// Now is the pack time (zero = time.Now).
	Now time.Time
}

// ErrNotRepo: the folder is not in a git worktree.
var ErrNotRepo = errors.New("not in a git worktree")

// git runs git in dir and returns its output (stderr in the error).
func git(dir string, args ...string) (string, error) {
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return string(out), fmt.Errorf("git %s: %s", args[0], msg)
		}
		return string(out), fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// entry is one file of the pack.
type entry struct {
	name string
	mode int64
	data []byte // or
	from string // a file on disk
}

// Pack writes the lane of the worktree dir is in to w and returns what
// it wrote. The worktree is only read.
func Pack(dir string, opt PackOptions, w io.Writer) (Manifest, error) {
	top, err := git(dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	if err != nil {
		return Manifest{}, ErrNotRepo
	}
	top = strings.TrimSpace(top)
	head, err := git(top, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return Manifest{}, errors.New("the worktree has no commit yet")
	}
	man := Manifest{Version: Version, Head: strings.TrimSpace(head), Path: top, PackedAt: opt.Now, Host: opt.Host,
		Name: opt.Meta.Name, Agent: opt.Meta.Agent, Notes: opt.Meta.Notes}
	if man.PackedAt.IsZero() {
		man.PackedAt = time.Now()
	}
	man.PackedAt = man.PackedAt.UTC().Truncate(time.Second)
	if man.Host == "" {
		man.Host, _ = os.Hostname()
	}
	if b, err := git(top, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		man.Branch = strings.TrimSpace(b)
	}
	if man.Name == "" {
		man.Name = filepath.Base(top)
	}

	tmp, err := os.MkdirTemp("", "termocode-lane-*")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(tmp)
	var entries []entry

	// The commits the other side may not have.
	ref := "HEAD"
	if man.Branch != "" {
		ref = "refs/heads/" + man.Branch
	}
	bundle := filepath.Join(tmp, bundleName)
	if _, err := git(top, "bundle", "create", "-q", bundle, ref, "--not", "--remotes"); err == nil {
		man.Bundle, man.BundleRef = true, ref
		if n, err := git(top, "rev-list", "--count", ref, "--not", "--remotes"); err == nil {
			_, _ = fmt.Sscanf(strings.TrimSpace(n), "%d", &man.Commits)
		}
		entries = append(entries, entry{name: bundleName, mode: 0o644, from: bundle})
	} else if !strings.Contains(err.Error(), "empty bundle") {
		return Manifest{}, fmt.Errorf("the commits could not be packed: %w", err)
	}

	// The changes not committed.
	patch, err := git(top, "diff", "--binary", "--no-color", "--no-ext-diff", "HEAD")
	if err != nil {
		return Manifest{}, fmt.Errorf("the changes could not be read: %w", err)
	}
	if patch != "" {
		man.Patch = true
		if names, err := git(top, "diff", "--name-only", "-z", "HEAD"); err == nil {
			man.Changed = len(splitZ(names))
		}
		entries = append(entries, entry{name: patchName, mode: 0o644, data: []byte(patch)})
	}

	// The new files.
	others, err := git(top, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Manifest{}, fmt.Errorf("the new files could not be listed: %w", err)
	}
	var total int64
	for _, rel := range splitZ(others) {
		full := filepath.Join(top, filepath.FromSlash(rel))
		info, err := os.Lstat(full)
		switch {
		case err != nil, !info.Mode().IsRegular(), !safeRel(rel),
			info.Size() > MaxFileSize, total+info.Size() > MaxFilesTotal, len(man.Files) >= MaxFiles:
			man.Skipped = append(man.Skipped, rel)
			continue
		}
		total += info.Size()
		man.Files = append(man.Files, rel)
		mode := int64(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		entries = append(entries, entry{name: filesDir + rel, mode: mode, from: full})
	}

	// The Claude Code chats of the folder.
	if opt.ClaudeHome != "" {
		chats := filepath.Join(opt.ClaudeHome, "projects", agentlog.Slug(top))
		ents, _ := os.ReadDir(chats)
		var size int64
		for _, e := range ents {
			n := e.Name()
			id, ok := strings.CutSuffix(n, ".jsonl")
			if !ok || e.IsDir() || strings.HasPrefix(n, "agent-") || !safeID(id) {
				continue
			}
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() || size+info.Size() > MaxChatsTotal {
				continue
			}
			size += info.Size()
			man.Chats = append(man.Chats, id)
			entries = append(entries, entry{name: claudeDir + n, mode: 0o600, from: filepath.Join(chats, n)})
		}
		sort.Strings(man.Chats)
	}

	mb, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	all := append([]entry{{name: manifestName, mode: 0o644, data: mb}}, entries...)
	for _, e := range all {
		if err := writeEntry(tw, e, man.PackedAt); err != nil {
			return Manifest{}, err
		}
	}
	if err := tw.Close(); err != nil {
		return Manifest{}, err
	}
	if err := gz.Close(); err != nil {
		return Manifest{}, err
	}
	return man, nil
}

// writeEntry adds one file to the tar.
func writeEntry(tw *tar.Writer, e entry, at time.Time) error {
	var r io.Reader
	size := int64(len(e.data))
	if e.from != "" {
		f, err := os.Open(e.from)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		size, r = info.Size(), f
	} else {
		r = bytes.NewReader(e.data)
	}
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: e.name, Mode: e.mode, Size: size, ModTime: at, Format: tar.FormatPAX}); err != nil {
		return err
	}
	n, err := io.Copy(tw, io.LimitReader(r, size))
	if err == nil && n != size {
		err = fmt.Errorf("%s changed while it was packed", e.name)
	}
	return err
}

// splitZ splits NUL-ended git output.
func splitZ(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// safeRel: rel is a plain relative slash path inside the worktree
// (never .git, never ..).
func safeRel(rel string) bool {
	if rel == "" || strings.Contains(rel, "\\") || strings.ContainsRune(rel, 0) || path.IsAbs(rel) || path.Clean(rel) != rel || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return false
	}
	for _, p := range strings.Split(rel, "/") {
		if p == ".git" || p == ".." {
			return false
		}
	}
	return true
}

// safeID: a session id is a plain file name.
func safeID(id string) bool {
	if id == "" || len(id) > 128 || strings.HasPrefix(id, ".") {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// PackFile packs dir into file; file "" = a new name in lanesDir (made
// when missing). It never writes over a file.
func PackFile(dir, file, lanesDir string, opt PackOptions) (string, Manifest, error) {
	tmpDir := filepath.Dir(file)
	if file == "" {
		tmpDir = lanesDir
		if err := os.MkdirAll(tmpDir, 0o700); err != nil {
			return "", Manifest{}, err
		}
	}
	f, err := os.CreateTemp(tmpDir, ".lane-*")
	if err != nil {
		return "", Manifest{}, err
	}
	man, err := Pack(dir, opt, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", Manifest{}, err
	}
	if file == "" {
		file = filepath.Join(tmpDir, FileName(man))
	}
	if _, err := os.Lstat(file); err == nil {
		_ = os.Remove(f.Name())
		return "", Manifest{}, fmt.Errorf("%s is there already", file)
	}
	if err := os.Rename(f.Name(), file); err != nil {
		_ = os.Remove(f.Name())
		return "", Manifest{}, err
	}
	return file, man, nil
}

// Packs are the pack files in dirs, newest first.
func Packs(dirs ...string) []string {
	type pf struct {
		path string
		at   time.Time
	}
	var all []pf
	seen := map[string]bool{}
	for _, d := range dirs {
		ents, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range ents {
			n := e.Name()
			p := filepath.Join(d, n)
			if e.IsDir() || !strings.HasSuffix(n, Ext) || strings.HasPrefix(n, ".") || seen[p] {
				continue
			}
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			seen[p] = true
			all = append(all, pf{p, info.ModTime()})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	out := make([]string, len(all))
	for i, p := range all {
		out[i] = p.path
	}
	return out
}
