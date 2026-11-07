package search

import (
	"bytes"
	"context"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// WriteFS is an FS the replace can also write (workspace.FS fits it).
type WriteFS interface {
	FS
	WriteFile(ctx context.Context, p string, data []byte, perm fs.FileMode) error
}

// CountLiteralFS is CountLiteral over fsys (a container workspace, read
// through its Host): the same folders skipped as the built-in search,
// the same exact byte count. Paths are absolute on fsys.
func CountLiteralFS(ctx context.Context, fsys FS, root, needle string) ([]FileCount, error) {
	if needle == "" || root == "" {
		return nil, nil
	}
	var out []FileCount
	var walk func(dir string, top bool) error
	walk = func(dir string, top bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		es, err := fsys.ReadDir(ctx, dir)
		if err != nil {
			if top {
				return err
			}
			return nil // an unreadable folder is skipped
		}
		slices.SortFunc(es, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
		for _, e := range es {
			p := path.Join(dir, e.Name())
			if e.IsDir() {
				if skipSearchDir(e.Name()) {
					continue
				}
				if err := walk(p, false); err != nil {
					return err
				}
				continue
			}
			if !e.Type().IsRegular() {
				continue
			}
			if info, err := e.Info(); err == nil && info.Size() > maxFallbackFileSize {
				continue
			}
			data, err := fsys.ReadFile(ctx, p)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				continue
			}
			if isBinary(data) {
				continue
			}
			if n := bytes.Count(data, []byte(needle)); n > 0 {
				out = append(out, FileCount{Path: p, Count: n})
			}
		}
		return nil
	}
	if err := walk(path.Clean(root), true); err != nil {
		return nil, err
	}
	return out, nil
}

// ReplaceLiteralInFileFS is ReplaceLiteralInFile through fsys: one read,
// one write (the file keeps its mode, as WriteFS promises).
func ReplaceLiteralInFileFS(ctx context.Context, fsys WriteFS, p, find, repl string) (int, error) {
	if find == "" {
		return 0, nil
	}
	data, err := fsys.ReadFile(ctx, p)
	if err != nil {
		return 0, err
	}
	n := bytes.Count(data, []byte(find))
	if n == 0 {
		return 0, nil
	}
	if err := fsys.WriteFile(ctx, p, bytes.ReplaceAll(data, []byte(find), []byte(repl)), 0o644); err != nil {
		return 0, err
	}
	return n, nil
}
