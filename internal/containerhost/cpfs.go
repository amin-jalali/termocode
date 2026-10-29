package containerhost

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"io/fs"
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
