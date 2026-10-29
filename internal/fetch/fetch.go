// Package fetch holds the small HTTP-download and archive-extraction helpers
// shared by `termocode setup` (Nerd Font install) and the in-editor
// language-server installer (internal/lspinstall).
//
// Everything here is context-aware so a background install can be cancelled,
// and every extractor guards against zip-slip (entries escaping dest).
package fetch

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Progress is called while a download runs. total is -1 when the server did
// not send a Content-Length. May be nil.
type Progress func(done, total int64)

// Download fetches url into dest (created / truncated). A non-200 answer is
// an error that carries the status code.
func Download(ctx context.Context, url, dest string, progress Progress) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d for %s", resp.StatusCode, url)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	var src io.Reader = resp.Body
	if progress != nil {
		src = &progressReader{r: resp.Body, total: resp.ContentLength, fn: progress}
	}
	_, err = io.Copy(out, src)
	return err
}

type progressReader struct {
	r     io.Reader
	done  int64
	total int64
	fn    Progress
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	p.fn(p.done, p.total)
	return n, err
}

// NameMapper decides where an archive entry lands. It receives the entry's
// slash-separated name and returns the path relative to dest, or "" to skip
// the entry. Identity (nil) keeps the archive layout.
type NameMapper func(name string) string

// safeJoin joins rel onto dest and rejects results outside dest (zip-slip).
func safeJoin(dest, rel string) (string, error) {
	target := filepath.Join(dest, filepath.FromSlash(rel))
	cleanDest := filepath.Clean(dest)
	if target != cleanDest && !strings.HasPrefix(target, cleanDest+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q escapes destination", rel)
	}
	return target, nil
}

func mapName(m NameMapper, name string) string {
	if m == nil {
		return name
	}
	return m(name)
}

// ExtractZip unpacks zipPath into dest. File modes (exec bits) are kept.
func ExtractZip(zipPath, dest string, m NameMapper) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		rel := mapName(m, f.Name)
		if rel == "" {
			continue
		}
		target, err := safeJoin(dest, rel)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(target, rc, f.Mode().Perm())
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// ExtractTarGz unpacks a .tar.gz / .tgz into dest. Regular files, dirs and
// symlinks (kept inside dest) are supported; other entry types are skipped.
func ExtractTarGz(path, dest string, m NameMapper) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	realDest, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rel := mapName(m, h.Name)
		if rel == "" {
			continue
		}
		target, err := safeJoin(dest, rel)
		if err != nil {
			return err
		}
		// Check the parent as it is on disk: an earlier symlink entry
		// may already sit in the path.
		realParent := resolveExisting(filepath.Dir(target))
		if !within(realDest, realParent) {
			return fmt.Errorf("archive entry %q escapes destination", rel)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(target, tr, os.FileMode(h.Mode).Perm()); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Resolve the link relative to its real directory and refuse
			// anything that would point outside dest.
			linkTarget := filepath.Join(realParent, h.Linkname)
			if filepath.IsAbs(h.Linkname) {
				linkTarget = resolveExisting(h.Linkname)
			}
			if !within(realDest, linkTarget) {
				return fmt.Errorf("archive link %q escapes destination", rel)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		}
	}
}

// resolveExisting resolves the symlinks of the longest existing prefix of
// p and appends the rest unchanged. p is absolute or below an existing dir,
// so the loop always ends.
func resolveExisting(p string) string {
	rest := ""
	for cur := filepath.Clean(p); ; cur = filepath.Dir(cur) {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		rest = filepath.Join(filepath.Base(cur), rest)
	}
}

// within reports whether p is root or below it.
func within(root, p string) bool {
	p = filepath.Clean(p)
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// Gunzip decompresses a single-file .gz into dest with the given mode.
func Gunzip(path, dest string, mode os.FileMode) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return writeFile(dest, gz, mode)
}

func writeFile(target string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// FlattenSuffix returns a NameMapper that keeps only entries whose name ends
// with one of the (case-insensitive) suffixes and drops their directories —
// e.g. the Nerd Font zip → *.ttf / *.otf straight into the font dir.
func FlattenSuffix(suffixes ...string) NameMapper {
	return func(name string) string {
		lower := strings.ToLower(name)
		for _, s := range suffixes {
			if strings.HasSuffix(lower, strings.ToLower(s)) {
				return filepath.Base(name)
			}
		}
		return ""
	}
}
