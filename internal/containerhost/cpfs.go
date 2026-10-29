package containerhost

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

// cpfs reads and writes files with `cp` tar streams: no tool is needed
// in the image.

// errIsDir is returned when a file operation meets a folder.
var errIsDir = errors.New("is a folder")

// opCtx bounds one file operation.
func (h *Host) opCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, h.cfg.Timeout)
}

// cpOutArgs is `cp [-L] <c>:<p> -`. -L follows a symlink at p (docker;
// podman follows it on its own).
func (h *Host) cpOutArgs(p string, follow bool) []string {
	a := []string{"cp"}
	if follow && h.cfg.Kind != "podman" {
		a = append(a, "-L")
	}
	return append(a, h.cfg.Container+":"+p, "-")
}

// withTar streams the tar of p and hands its first header to fn, which
// may read on. When the stream has no tar, the call is made again
// without a stream to get the engine's error.
func (h *Host) withTar(ctx context.Context, op, p string, follow bool, fn func(tr *tar.Reader, first *tar.Header) error) error {
	argv := h.argv(h.cpOutArgs(p, follow)...)
	r, stop, err := h.cfg.X.Stream(ctx, "", argv...)
	if err != nil {
		return &fs.PathError{Op: op, Path: p, Err: err}
	}
	defer stop()
	tr := tar.NewReader(r)
	hd, err := tr.Next()
	if err != nil {
		if ctx.Err() != nil {
			return &fs.PathError{Op: op, Path: p, Err: ctx.Err()}
		}
		_, stderr, code, cerr := h.cfg.X.Capture(ctx, "", argv...)
		switch {
		case cerr != nil:
			return &fs.PathError{Op: op, Path: p, Err: cerr}
		case code != 0:
			return h.engineErr(op, p, stderr, code)
		}
		return &fs.PathError{Op: op, Path: p, Err: errors.New("the engine sent no archive")}
	}
	return fn(tr, hd)
}

// ReadFile reads one file with `cp -L <c>:<p> -`.
func (f containerFS) ReadFile(ctx context.Context, p string) ([]byte, error) {
	h := f.h
	abs, err := resolve(h.cfg.Root, p)
	if err != nil {
		return nil, err
	}
	ctx, cancel := h.opCtx(ctx)
	defer cancel()
	var data []byte
	err = h.withTar(ctx, "read", abs, true, func(tr *tar.Reader, hd *tar.Header) error {
		switch hd.Typeflag {
		case tar.TypeDir:
			return &fs.PathError{Op: "read", Path: abs, Err: errIsDir}
		case tar.TypeReg:
		default:
			return &fs.PathError{Op: "read", Path: abs, Err: errors.New("not a regular file")}
		}
		data, err = io.ReadAll(tr)
		if err != nil {
			return &fs.PathError{Op: "read", Path: abs, Err: err}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// header is the tar header of p itself (follow: through a symlink).
func (h *Host) header(ctx context.Context, op, abs string, follow bool) (*tar.Header, error) {
	var out *tar.Header
	err := h.withTar(ctx, op, abs, follow, func(_ *tar.Reader, hd *tar.Header) error {
		c := *hd
		out = &c
		return nil
	})
	return out, err
}

// Stat reads the first tar header of `cp -L <c>:<p> -` and stops the
// stream: it needs no tool in the image. Sys() is the *tar.Header (Uid,
// Gid).
func (f containerFS) Stat(ctx context.Context, p string) (fs.FileInfo, error) {
	abs, err := resolve(f.h.cfg.Root, p)
	if err != nil {
		return nil, err
	}
	ctx, cancel := f.h.opCtx(ctx)
	defer cancel()
	hd, err := f.h.header(ctx, "stat", abs, true)
	if err != nil {
		return nil, err
	}
	return hd.FileInfo(), nil
}

// readDirTar lists a folder from the tar of the whole folder (images
// without sh). It reads the full tree, so it is slower than the sh way.
func (f containerFS) readDirTar(ctx context.Context, abs string) ([]fs.DirEntry, error) {
	var ents []fs.DirEntry
	err := f.h.withTar(ctx, "readdir", abs, true, func(tr *tar.Reader, first *tar.Header) error {
		if first.Typeflag != tar.TypeDir {
			return &fs.PathError{Op: "readdir", Path: abs, Err: errors.New("not a folder")}
		}
		for {
			hd, err := tr.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return &fs.PathError{Op: "readdir", Path: abs, Err: err}
			}
			_, rest, _ := strings.Cut(hd.Name, "/")
			rest = strings.TrimSuffix(rest, "/")
			if rest == "" || strings.Contains(rest, "/") {
				continue
			}
			fi := hd.FileInfo()
			ents = append(ents, &dirEntry{f: f, dir: abs, name: rest, typ: fi.Mode().Type(), info: fi})
		}
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })
	return ents, nil
}

// cpInArgs is `cp <keep owner> - <c>:<dir>`: the tar's uid, gid and mode
// are kept (docker -a; podman --archive=false, since podman's default
// changes the owner).
func (h *Host) cpInArgs(dir string) []string {
	keep := "-a"
	if h.cfg.Kind == "podman" {
		keep = "--archive=false"
	}
	return []string{"cp", keep, "-", h.cfg.Container + ":" + dir}
}

// putTar extracts the tar data into the folder dir.
func (h *Host) putTar(ctx context.Context, op, p, dir string, data []byte) error {
	_, stderr, code, err := h.call(ctx, data, h.cpInArgs(dir)...)
	if err != nil {
		return &fs.PathError{Op: op, Path: p, Err: err}
	}
	if code != 0 {
		return h.engineErr(op, p, stderr, code)
	}
	return nil
}

// WriteFile replaces or creates one file with a one-entry tar into its
// folder. An existing file keeps its mode, uid and gid; a new file gets
// perm and the uid/gid commands run as (`id -u`), or the folder's owner
// when that is unknown. Nothing else in the container changes.
func (f containerFS) WriteFile(ctx context.Context, p string, data []byte, perm fs.FileMode) error {
	h := f.h
	abs, err := resolve(h.cfg.Root, p)
	if err != nil {
		return err
	}
	if abs == h.cfg.Root {
		return &fs.PathError{Op: "write", Path: abs, Err: errIsDir}
	}
	ctx, cancel := h.opCtx(ctx)
	defer cancel()
	mode, uid, gid := int64(perm.Perm()), 0, 0
	hd, err := h.header(ctx, "write", abs, false)
	switch {
	case err == nil && hd.Typeflag == tar.TypeDir:
		return &fs.PathError{Op: "write", Path: abs, Err: errIsDir}
	case err == nil && hd.Typeflag == tar.TypeSymlink:
		return &fs.PathError{Op: "write", Path: abs, Err: errors.New("is a symlink")}
	case err == nil:
		mode, uid, gid = hd.Mode&0o7777, hd.Uid, hd.Gid
	case errors.Is(err, fs.ErrNotExist):
		parent, perr := h.header(ctx, "write", path.Dir(abs), true)
		if perr != nil {
			return perr
		}
		uid, gid = parent.Uid, parent.Gid
		if u, g, ok := h.Owner(); ok {
			uid, gid = u, g
		}
	default:
		return err
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err = tw.WriteHeader(&tar.Header{
		Name:     path.Base(abs),
		Typeflag: tar.TypeReg,
		Mode:     mode,
		Uid:      uid,
		Gid:      gid,
		Size:     int64(len(data)),
		ModTime:  time.Now(),
		Format:   tar.FormatPAX,
	})
	if err == nil {
		_, err = tw.Write(data)
	}
	if err == nil {
		err = tw.Close()
	}
	if err != nil {
		return &fs.PathError{Op: "write", Path: abs, Err: err}
	}
	return h.putTar(ctx, "write", abs, path.Dir(abs), buf.Bytes())
}
