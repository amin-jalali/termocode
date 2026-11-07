package tests

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// FS is the part of a workspace file system discovery reads (a
// workspace.FS fits it). Paths are absolute, slash-separated: the
// workspace of a container (ADR 0018), where every read is one call
// through the engine CLI.
type FS interface {
	Stat(ctx context.Context, p string) (fs.FileInfo, error)
	ReadDir(ctx context.Context, p string) ([]fs.DirEntry, error)
	ReadFile(ctx context.Context, p string) ([]byte, error)
}

// maxScanDirs caps the folders a remote walk lists: each one is a
// command in the container, so a huge tree stops early instead of
// running for minutes.
const maxScanDirs = 3000

// DetectFS is Detect for a workspace reached through fsys.
func DetectFS(ctx context.Context, fsys FS, root string) Framework {
	has := func(name string) bool {
		_, err := fsys.Stat(ctx, path.Join(root, name))
		return err == nil
	}
	switch {
	case has("go.mod"):
		return FrameworkGo
	case has("Cargo.toml"):
		return FrameworkCargo
	case has("pyproject.toml"), has("setup.py"), has("pytest.ini"), has("setup.cfg"), has("tox.ini"):
		return FrameworkPytest
	case has("package.json"):
		data, _ := fsys.ReadFile(ctx, path.Join(root, "package.json"))
		return detectJS(string(data))
	}
	return FrameworkNone
}

// goModulePathOf reads the `module` line of a go.mod text.
func goModulePathOf(data []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(ln, "module ") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(ln, "module")), `"`)
		}
	}
	return ""
}

// DiscoverFS is Discover for a workspace reached through fsys: the same
// skip rules and the same tree, read file by file through the host. A
// cancelled ctx (or the folder cap) ends the walk with what was found.
func DiscoverFS(ctx context.Context, fsys FS, root string, fw Framework) ([]*Package, error) {
	if fw == FrameworkNone {
		return nil, nil
	}
	root = path.Clean(root)
	module := ""
	if fw == FrameworkGo {
		if data, err := fsys.ReadFile(ctx, path.Join(root, "go.mod")); err == nil {
			module = goModulePathOf(data)
		}
	}
	byDir := map[string]*Package{}
	scanned, dirs := 0, 0
	var walkErr error
	queue := []string{root}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			walkErr = err
			break
		}
		dir := queue[0]
		queue = queue[1:]
		dirs++
		if dirs > maxScanDirs {
			walkErr = errors.New("the workspace is too big to list every folder; showing the tests found so far")
			break
		}
		ents, err := fsys.ReadDir(ctx, dir)
		if err != nil {
			if dir == root {
				return nil, err
			}
			continue // unreadable folder: skip, keep walking
		}
		for _, d := range ents {
			name := d.Name()
			p := path.Join(dir, name)
			if d.IsDir() {
				if strings.HasPrefix(name, ".") || skipDirs[name] || (fw == FrameworkGo && name == "testdata") {
					continue
				}
				queue = append(queue, p)
				continue
			}
			if !IsTestFile(fw, p) {
				continue
			}
			scanned++
			if scanned > maxScanFiles {
				queue = nil
				break
			}
			if info, err := d.Info(); err == nil && info.Size() > maxFileBytes {
				continue
			}
			data, err := fsys.ReadFile(ctx, p)
			if err != nil || len(data) > maxFileBytes {
				continue
			}
			cases := ParseFile(fw, p, string(data))
			if len(cases) == 0 {
				continue
			}
			pkg := byDir[dir]
			if pkg == nil {
				relDir := strings.TrimPrefix(strings.TrimPrefix(dir, root), "/")
				if relDir == "" {
					relDir = "."
				}
				pkg = &Package{Name: relDir, Dir: dir}
				if fw == FrameworkGo {
					pkg.Name = GoImportPath(module, relDir)
				}
				byDir[dir] = pkg
			}
			rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
			pkg.Files = append(pkg.Files, &File{Path: p, Rel: rel, Cases: cases})
		}
	}
	pkgs := make([]*Package, 0, len(byDir))
	for _, p := range byDir {
		sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Path < p.Files[j].Path })
		pkgs = append(pkgs, p)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	return pkgs, walkErr
}
