package containerhost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/workspace"
)

// The container file browser (CTX-9) works on a workspace FS. Its paths
// are absolute from the FS root ("/" = the root, "/etc/hosts"); they
// are passed to the FS as root-relative names. Reading never changes
// the container; Upload writes only when the caller asks, and refuses
// to replace a file unless the person agreed.

// ErrBrowseExists is returned by BrowseUpload when the target file is
// there and replace was not asked.
var ErrBrowseExists = errors.New("a file with this name is already there")

// BrowseEntry is one row of a folder list.
type BrowseEntry struct {
	Name string
	Dir  bool
	Link bool
	Size int64
	Mode fs.FileMode
}

// browseClean is p as a clean absolute browser path.
func browseClean(p string) string { return path.Clean("/" + p) }

// fsName is the root-relative name the FS takes for p.
func fsName(p string) string {
	n := strings.TrimPrefix(browseClean(p), "/")
	if n == "" {
		return "."
	}
	return n
}

// BrowseList lists dir: folders first, then by name (case-insensitive).
func BrowseList(ctx context.Context, fsys workspace.FS, dir string) ([]BrowseEntry, error) {
	ents, err := fsys.ReadDir(ctx, fsName(dir))
	if err != nil {
		return nil, err
	}
	out := make([]BrowseEntry, 0, len(ents))
	for _, e := range ents {
		be := BrowseEntry{Name: e.Name(), Dir: e.IsDir(), Link: e.Type()&fs.ModeSymlink != 0}
		if info, err := e.Info(); err == nil {
			be.Size, be.Mode = info.Size(), info.Mode()
		}
		out = append(out, be)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// BrowseParent is the folder above p ("/" for the root).
func BrowseParent(p string) string { return path.Dir(browseClean(p)) }

// BrowseCrumbs is every folder from the root down to p.
func BrowseCrumbs(p string) []string {
	p = browseClean(p)
	out := []string{"/"}
	if p == "/" {
		return out
	}
	cur := ""
	for _, part := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
		cur += "/" + part
		out = append(out, cur)
	}
	return out
}

// BrowseFile is a file opened read-only for the view.
type BrowseFile struct {
	Path      string
	Text      string // "" for a binary file
	Size      int64  // the whole file
	Truncated bool   // Text holds only the first max bytes
	Binary    bool
}

// BrowseView reads p for a read-only view: at most max bytes of text;
// a file with a NUL byte in its start is binary and shows no text.
func BrowseView(ctx context.Context, fsys workspace.FS, p string, max int64) (BrowseFile, error) {
	name := fsName(p)
	info, err := fsys.Stat(ctx, name)
	if err != nil {
		return BrowseFile{}, err
	}
	if info.IsDir() {
		return BrowseFile{}, fmt.Errorf("%s is a folder", browseClean(p))
	}
	data, err := fsys.ReadFile(ctx, name)
	if err != nil {
		return BrowseFile{}, err
	}
	f := BrowseFile{Path: browseClean(p), Size: int64(len(data))}
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		f.Binary = true
		return f, nil
	}
	if max > 0 && int64(len(data)) > max {
		data, f.Truncated = data[:max], true
	}
	f.Text = string(data)
	return f, nil
}

// BrowseDownload copies p into the local folder destDir as a new file
// (0600). An existing name is never replaced: "name (1)", "name (2)"…
// It returns the local path written.
func BrowseDownload(ctx context.Context, fsys workspace.FS, p, destDir string) (string, error) {
	name := fsName(p)
	if info, err := fsys.Stat(ctx, name); err != nil {
		return "", err
	} else if info.IsDir() {
		return "", fmt.Errorf("%s is a folder", browseClean(p))
	}
	data, err := fsys.ReadFile(ctx, name)
	if err != nil {
		return "", err
	}
	base := path.Base(browseClean(p))
	if base == "/" || base == "." {
		return "", errors.New("no file name")
	}
	for i := 0; i < 1000; i++ {
		n := base
		if i > 0 {
			n = fmt.Sprintf("%s (%d)", base, i)
		}
		dst := filepath.Join(destDir, n)
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			_ = os.Remove(dst)
			return "", errors.Join(werr, cerr)
		}
		return dst, nil
	}
	return "", errors.New("too many files with this name")
}

// BrowseUpload copies the local file into the container folder dir and
// returns the browser path written. An existing file is replaced only
// with replace (the person said yes); otherwise ErrBrowseExists.
func BrowseUpload(ctx context.Context, fsys workspace.FS, local, dir string, replace bool) (string, error) {
	info, err := os.Stat(local)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a plain file", local)
	}
	target := path.Join(browseClean(dir), filepath.Base(local))
	if st, err := fsys.Stat(ctx, fsName(target)); err == nil {
		if st.IsDir() {
			return "", fmt.Errorf("%s is a folder", target)
		}
		if !replace {
			return "", ErrBrowseExists
		}
	}
	data, err := os.ReadFile(local)
	if err != nil {
		return "", err
	}
	if err := fsys.WriteFile(ctx, fsName(target), data, info.Mode().Perm()); err != nil {
		return "", err
	}
	return target, nil
}
