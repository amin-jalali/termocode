package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// FuncStat is one function with the statements no test reached.
type FuncStat struct {
	Name          string // "Name" or "(*Type).Name"
	File          string // module-relative
	Line          int
	Missed, Total int
}

type funcSpan struct {
	name       string
	start, end int
	missed     int
	total      int
}

// TopUncovered finds, per system, the n functions with the most uncovered
// statements. It reads the source under root to know where functions begin
// and end, so the count is in statements (not a percent of a tiny function).
func TopUncovered(blocks []Block, module, root string, sys *Systems, n int) map[string][]FuncStat {
	byFile := map[string][]Block{}
	for _, b := range blocks {
		if sys.Measured(pkgOf(b.File, module)) {
			byFile[b.File] = append(byFile[b.File], b)
		}
	}
	out := map[string][]FuncStat{}
	for file, bs := range byFile {
		rel := strings.TrimPrefix(file, module+"/")
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, 0)
		if err != nil {
			continue
		}
		var spans []*funcSpan
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				spans = append(spans, &funcSpan{
					name:  funcName(fd),
					start: fset.Position(fd.Pos()).Line,
					end:   fset.Position(fd.End()).Line,
				})
			}
		}
		for _, b := range bs {
			line, _ := strconv.Atoi(strings.SplitN(b.Pos, ".", 2)[0])
			for _, s := range spans {
				if line >= s.start && line <= s.end {
					s.total += b.Statements
					if b.Count == 0 {
						s.missed += b.Statements
					}
					break
				}
			}
		}
		system, _ := sys.Classify(pkgOf(file, module))
		for _, s := range spans {
			if s.missed > 0 {
				out[system] = append(out[system], FuncStat{Name: s.name, File: rel, Line: s.start, Missed: s.missed, Total: s.total})
			}
		}
	}
	for name, list := range out {
		sort.Slice(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if a.Missed != b.Missed {
				return a.Missed > b.Missed
			}
			if a.File != b.File {
				return a.File < b.File
			}
			return a.Line < b.Line
		})
		if n > 0 && len(list) > n {
			list = list[:n]
		}
		out[name] = list
	}
	return out
}

func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	t := fd.Recv.List[0].Type
	star := ""
	if s, ok := t.(*ast.StarExpr); ok {
		star, t = "*", s.X
	}
	if ix, ok := t.(*ast.IndexExpr); ok {
		t = ix.X
	}
	if ix, ok := t.(*ast.IndexListExpr); ok {
		t = ix.X
	}
	id, ok := t.(*ast.Ident)
	if !ok {
		return fd.Name.Name
	}
	return "(" + star + id.Name + ")." + fd.Name.Name
}
