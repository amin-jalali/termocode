package transfer

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Local transfers: termocode runs on the user's own computer, so a
// download is a copy into the Downloads folder and an upload is a copy of
// files the user picks (PickChooser) into the workspace.

// ParseUserDirs reads key (XDG_DOWNLOAD_DIR) from the text of
// user-dirs.dirs. Values are "$HOME/…" or absolute; "$HOME" alone means
// the folder is turned off. It returns "" when unset.
func ParseUserDirs(data []byte, key, home string) string {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		return userDirValue(v, home)
	}
	return ""
}

// userDirValue expands one user-dirs value (quoted or not).
func userDirValue(v, home string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimSuffix(strings.TrimPrefix(v, `"`), `"`)
	switch {
	case v == "$HOME" || v == "$HOME/":
		return ""
	case strings.HasPrefix(v, "$HOME/"):
		v = filepath.Join(home, strings.TrimPrefix(v, "$HOME/"))
	case !filepath.IsAbs(v):
		return ""
	}
	if filepath.Clean(v) == filepath.Clean(home) {
		return ""
	}
	return filepath.Clean(v)
}

// DownloadsDir is the user's Downloads folder: what `xdg-user-dir
// DOWNLOAD` prints (userDir runs it; nil or "" when it is missing), else
// $XDG_DOWNLOAD_DIR, else XDG_DOWNLOAD_DIR in user-dirs.dirs, else
// ~/Downloads. macOS has no such setting: ~/Downloads.
func DownloadsDir(getenv func(string) string, home, goos string, userDir func() string) string {
	def := filepath.Join(home, "Downloads")
	if goos == "darwin" || goos == "windows" {
		return def
	}
	if userDir != nil {
		if d := strings.TrimSpace(userDir()); d != "" && filepath.IsAbs(d) && filepath.Clean(d) != filepath.Clean(home) {
			return filepath.Clean(d)
		}
	}
	if d := userDirValue(getenv("XDG_DOWNLOAD_DIR"), home); d != "" {
		return d
	}
	cfg := getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	if data, err := os.ReadFile(filepath.Join(cfg, "user-dirs.dirs")); err == nil {
		if d := ParseUserDirs(data, "XDG_DOWNLOAD_DIR", home); d != "" {
			return d
		}
	}
	return def
}

// CopyInto copies src (a file, or a folder as is) into dir under a name
// that is free there (UniqueName: "a (1).txt"): nothing is ever replaced.
// progress, when set, gets the bytes copied so far. On an error or a
// cancel nothing is left behind. It returns the new name.
func CopyInto(ctx context.Context, src, dir string, progress func(int64)) (string, error) {
	fi, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ErrCanceled
	}
	c := &copier{ctx: ctx, progress: progress}
	base := filepath.Base(src)
	for range 1000 {
		name := UniqueName(dir, base)
		dst := filepath.Join(dir, name)
		if fi.IsDir() {
			err = os.Mkdir(dst, fi.Mode().Perm()|0o700)
		} else {
			err = c.file(src, dst, fi)
		}
		if os.IsExist(err) {
			continue // taken between UniqueName and the create
		}
		if err == nil && fi.IsDir() {
			err = c.tree(src, dst)
		}
		if err != nil {
			_ = os.RemoveAll(dst)
			return "", err
		}
		_ = os.Chtimes(dst, fi.ModTime(), fi.ModTime())
		return name, nil
	}
	return "", fs.ErrExist
}

type copier struct {
	ctx      context.Context
	progress func(int64)
	done     int64
	buf      []byte
}

// file creates dst (it must not exist) with the data of src.
func (c *copier) file(src, dst string, fi fs.FileInfo) error {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return err
	}
	err = c.data(src, out)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return err
	}
	_ = os.Chmod(dst, fi.Mode().Perm())
	_ = os.Chtimes(dst, fi.ModTime(), fi.ModTime())
	return nil
}

func (c *copier) data(src string, out io.Writer) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if c.buf == nil {
		c.buf = make([]byte, 1<<20)
	}
	for {
		if c.ctx.Err() != nil {
			return ErrCanceled
		}
		n, rerr := in.Read(c.buf)
		if n > 0 {
			if _, err := out.Write(c.buf[:n]); err != nil {
				return err
			}
			c.done += int64(n)
			if c.progress != nil {
				c.progress(c.done)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// tree copies what is below src into the (new, empty) folder dst.
// Symbolic links are copied as links; devices and sockets are skipped.
func (c *copier) tree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil || rel == "." {
			return err
		}
		to := filepath.Join(dst, rel)
		fi, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.Mkdir(to, fi.Mode().Perm()|0o700)
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(target, to)
		case d.Type().IsRegular():
			return c.file(p, to, fi)
		}
		return nil
	})
}
