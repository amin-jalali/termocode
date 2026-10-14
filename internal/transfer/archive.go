package transfer

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// TarGz writes the folder root as a tar.gz; names start with the folder's
// own name ("proj/a.txt"). Regular files, folders and symlinks are kept;
// sockets and devices are skipped.
func TarGz(w io.Writer, root string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	base := filepath.Dir(filepath.Clean(root))
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		link := ""
		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			if link, err = os.Readlink(p); err != nil {
				return err
			}
		case fi.IsDir(), fi.Mode().IsRegular():
		default:
			return nil
		}
		h, err := tar.FileInfoHeader(fi, link)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		if fi.IsDir() {
			h.Name += "/"
		}
		h.Uname, h.Gname = "", ""
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, err = io.CopyN(tw, f, h.Size)
		_ = f.Close()
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// checkRel accepts a relative slash path with no "..", no absolute part.
func checkRel(rel string) error {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") || isWinAbs("/"+rel) {
		return fmt.Errorf("bad name %q", rel)
	}
	if c := path.Clean(rel); c != rel || c == ".." || strings.HasPrefix(c, "../") {
		return fmt.Errorf("bad name %q", rel)
	}
	return nil
}

// ExtractTarGz unpacks r into dest: only folders and regular files (links,
// devices and unsafe names are skipped), at most maxBytes of file data.
// It unpacks into a hidden folder in dest first, then moves each top
// level entry into dest under a free name (UniqueName), so nothing is
// replaced and a failed upload leaves nothing behind. It returns the
// top-level names as placed.
func ExtractTarGz(r io.Reader, dest string, maxBytes int64) ([]string, error) {
	stage, err := os.MkdirTemp(dest, ".termocode-upload-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(strings.TrimPrefix(h.Name, "./"), "/")
		if checkRel(name) != nil {
			continue
		}
		dst := filepath.Join(stage, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return nil, err
			}
		case tar.TypeReg, tar.TypeRegA: //nolint:staticcheck // old archives
			total += h.Size
			if maxBytes > 0 && total > maxBytes {
				return nil, ErrTooBig
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return nil, err
			}
			mode := os.FileMode(h.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode|0o200)
			if err != nil {
				continue // a duplicate entry
			}
			_, err = io.CopyN(f, tr, h.Size)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return nil, err
			}
		}
	}
	return PlaceAll(stage, dest)
}

// PlaceAll moves every entry of stage into dest under a free name and
// returns the names used.
func PlaceAll(stage, dest string) ([]string, error) {
	ents, err := os.ReadDir(stage)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		name := UniqueName(dest, e.Name())
		if err := os.Rename(filepath.Join(stage, e.Name()), filepath.Join(dest, name)); err != nil {
			return names, err
		}
		names = append(names, name)
	}
	return names, nil
}

// UniqueName returns name, or "name (1).ext", "name (2).ext", … : the
// first that does not exist in dir.
func UniqueName(dir, name string) string {
	if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
		return name
	}
	stem, ext := name, ""
	if fi, err := os.Stat(filepath.Join(dir, name)); err == nil && !fi.IsDir() {
		if e := filepath.Ext(name); e != "" && e != name {
			stem, ext = strings.TrimSuffix(name, e), e
		}
	}
	for i := 1; ; i++ {
		n := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if _, err := os.Lstat(filepath.Join(dir, n)); err != nil {
			return n
		}
	}
}

// Inside reports whether p is root or below it (lexically, both cleaned).
func Inside(root, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
