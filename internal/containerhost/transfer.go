package containerhost

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Transfer (plan RC, phase 8): upload and download in a container
// workspace. The local side is a folder termocode owns (a stage under the
// temp folder); the container side is the workspace. Both directions are
// one tar stream through `cp`, so the image needs no tool.

// ErrTransferTooLarge is returned when a transfer is over its byte cap.
var ErrTransferTooLarge = errors.New("the transfer is too large")

// CopyOut copies the file or folder p of the container into the local
// folder dest (it must exist) and returns the name it got there. Only
// files and folders are copied (a symlink inside a folder is skipped; p
// itself is followed). maxBytes caps the file data (0 = no cap).
func (h *Host) CopyOut(ctx context.Context, p, dest string, maxBytes int64) (string, error) {
	abs, err := resolve(h.cfg.Root, p)
	if err != nil {
		return "", err
	}
	var name string
	err = h.withTar(ctx, "download", abs, true, func(tr *tar.Reader, hd *tar.Header) error {
		var total int64
		for ; hd != nil; hd, err = tr.Next() {
			rel := path.Clean(hd.Name)
			if rel == "." || path.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
				return fmt.Errorf("unsafe name in the archive: %q", hd.Name)
			}
			if name == "" {
				name, _, _ = strings.Cut(rel, "/")
			}
			local := filepath.Join(dest, filepath.FromSlash(rel))
			switch hd.Typeflag {
			case tar.TypeDir:
				if err := os.MkdirAll(local, 0o755); err != nil {
					return err
				}
			case tar.TypeReg:
				total += hd.Size
				if maxBytes > 0 && total > maxBytes {
					return ErrTransferTooLarge
				}
				if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
					return err
				}
				if err := writeLocal(local, tr, hd.FileInfo().Mode().Perm()); err != nil {
					return err
				}
			}
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", &fs.PathError{Op: "download", Path: abs, Err: errors.New("the engine sent an empty archive")}
	}
	return name, nil
}

// writeLocal writes one new local file (never through a symlink, never
// over an existing file).
func writeLocal(p string, r io.Reader, perm fs.FileMode) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm&0o777|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// CopyIn copies each local file or folder in srcs into the container
// folder dir and returns the names used. It never replaces anything: a
// name already in dir becomes "name (1)", "name (2)"… New files get the
// uid and gid commands run as (when known) and their local mode.
// Symlinks are skipped. maxBytes caps the file data (0 = no cap).
func (h *Host) CopyIn(ctx context.Context, srcs []string, dir string, maxBytes int64) ([]string, error) {
	absDir, err := resolve(h.cfg.Root, dir)
	if err != nil {
		return nil, err
	}
	sctx, cancel := h.opCtx(ctx)
	fi, err := h.fs.Stat(sctx, absDir)
	cancel()
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, &fs.PathError{Op: "upload", Path: absDir, Err: errors.New("not a folder")}
	}
	uid, gid, known := h.Owner()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	var names []string
	var total int64
	taken := map[string]bool{}
	for _, src := range srcs {
		st, err := os.Lstat(src)
		if err != nil {
			return nil, err
		}
		if st.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		name, err := h.freeName(ctx, absDir, filepath.Base(src), st.IsDir(), taken)
		if err != nil {
			return nil, err
		}
		taken[name] = true
		err = filepath.WalkDir(src, func(lp string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() && !info.IsDir() {
				return nil // symlinks, devices, sockets
			}
			rel, err := filepath.Rel(src, lp)
			if err != nil {
				return err
			}
			tn := name
			if rel != "." {
				tn = name + "/" + filepath.ToSlash(rel)
			}
			hd := &tar.Header{Name: tn, Mode: int64(info.Mode().Perm()), ModTime: info.ModTime(), Typeflag: tar.TypeReg, Size: info.Size()}
			if info.IsDir() {
				hd.Name += "/"
				hd.Typeflag, hd.Size = tar.TypeDir, 0
			}
			if known {
				hd.Uid, hd.Gid = uid, gid
			}
			if hd.Typeflag == tar.TypeReg {
				total += hd.Size
				if maxBytes > 0 && total > maxBytes {
					return ErrTransferTooLarge
				}
			}
			if err := tw.WriteHeader(hd); err != nil {
				return err
			}
			if hd.Typeflag != tar.TypeReg {
				return nil
			}
			f, err := os.Open(lp)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			_, err = io.CopyN(tw, f, hd.Size)
			return err
		})
		if err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	_, stderr, code, err := h.cfg.X.CaptureInput(ctx, "", buf.Bytes(), h.argv(h.cpInArgs(absDir)...)...)
	if err != nil {
		return nil, &fs.PathError{Op: "upload", Path: absDir, Err: err}
	}
	if code != 0 {
		return nil, h.engineErr("upload", absDir, stderr, code)
	}
	return names, nil
}

// freeName is name, or "name (1).ext"…: the first that is not in the
// container folder dir and not taken by this upload.
func (h *Host) freeName(ctx context.Context, dir, name string, isDir bool, taken map[string]bool) (string, error) {
	free := func(n string) (bool, error) {
		if taken[n] {
			return false, nil
		}
		sctx, cancel := h.opCtx(ctx)
		defer cancel()
		_, err := h.fs.Stat(sctx, path.Join(dir, n))
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	if ok, err := free(name); ok || err != nil {
		return name, err
	}
	stem, ext := name, ""
	if e := path.Ext(name); !isDir && e != "" && e != name {
		stem, ext = strings.TrimSuffix(name, e), e
	}
	for i := 1; i < 1000; i++ {
		n := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if ok, err := free(n); ok || err != nil {
			return n, err
		}
	}
	return "", fmt.Errorf("no free name for %s in %s", name, dir)
}
