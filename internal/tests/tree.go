package tests

import (
	"path/filepath"
	"strings"
)

// Tree is a workspace's discovered tests plus the logic that folds runner
// events into them.
type Tree struct {
	Root     string
	FW       Framework
	Packages []*Package
}

// Counts tallies the whole tree.
func (t *Tree) Counts() Counts {
	if t == nil {
		return Counts{}
	}
	return CountAll(t.Packages)
}

// Each calls fn for every case.
func (t *Tree) Each(fn func(p *Package, f *File, c *Case)) {
	if t == nil {
		return
	}
	for _, p := range t.Packages {
		for _, f := range p.Files {
			for _, c := range f.Cases {
				fn(p, f, c)
			}
		}
	}
}

// MarkRunning resets the cases selected by in (nil = all) to Running and
// clears their last results. Package errors of touched packages clear too.
func (t *Tree) MarkRunning(in func(*Case) bool) {
	if t == nil {
		return
	}
	touched := map[*Package]bool{}
	t.Each(func(p *Package, _ *File, c *Case) {
		if in != nil && !in(c) {
			return
		}
		touched[p] = true
		c.Status = StatusRunning
		c.Message, c.FailFile, c.FailLine, c.Output, c.Duration = "", "", 0, nil, 0
	})
	for p := range touched {
		p.Error, p.ErrFile, p.ErrLine = "", "", 0
	}
}

// FinishRun turns cases that never got a result back into Pending.
func (t *Tree) FinishRun() {
	t.Each(func(_ *Package, _ *File, c *Case) {
		if c.Status == StatusRunning {
			c.Status = StatusPending
		}
	})
}

// FindFile returns the file node for an absolute path.
func (t *Tree) FindFile(abs string) (*Package, *File) {
	if t == nil {
		return nil, nil
	}
	abs = filepath.Clean(abs)
	for _, p := range t.Packages {
		for _, f := range p.Files {
			if f.Path == abs {
				return p, f
			}
		}
	}
	return nil, nil
}

// abs resolves a runner-reported path against base.
func absPath(base, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, filepath.FromSlash(p))
}

// Apply folds one event into the tree. It returns the case it updated (nil
// for output events and unmatched results).
func (t *Tree) Apply(ev Event) *Case {
	if t == nil {
		return nil
	}
	switch ev.Kind {
	case EventPackage:
		t.applyPackage(ev)
		return nil
	case EventResult:
	default:
		return nil
	}
	c, base := t.match(ev)
	if c == nil {
		return nil
	}
	c.Status = MergeStatus(c.Status, ev.Status)
	if ev.Duration > 0 {
		c.Duration = ev.Duration
	}
	if ev.Message != "" && (ev.Status == StatusFailed || c.Message == "") {
		c.Message = ev.Message
	}
	if ev.FailFile != "" && ev.FailLine > 0 && c.FailLine == 0 {
		c.FailFile, c.FailLine = absPath(base, ev.FailFile), ev.FailLine
	}
	if len(ev.Output) > 0 {
		c.Output = append(c.Output, ev.Output...)
	}
	return c
}

func (t *Tree) applyPackage(ev Event) {
	var pkg *Package
	switch {
	case ev.File != "":
		pkg, _ = t.FindFile(absPath(t.Root, ev.File))
		if pkg != nil && ev.Status == StatusFailed {
			rel, _ := filepath.Rel(t.Root, absPath(t.Root, ev.File))
			pkg.Error = filepath.ToSlash(rel) + ": " + ev.Message
			return
		}
	case ev.Package != "":
		for _, p := range t.Packages {
			if p.Name == ev.Package {
				pkg = p
				break
			}
		}
	}
	if pkg == nil {
		return
	}
	if ev.Status == StatusFailed {
		pkg.Error = ev.Message
		if ev.FailFile != "" && ev.FailLine > 0 {
			pkg.ErrFile, pkg.ErrLine = absPath(t.Root, ev.FailFile), ev.FailLine
		}
	} else {
		pkg.Error, pkg.ErrFile, pkg.ErrLine = "", "", 0
	}
}

// match finds the case an event refers to and the base dir for relative
// failure paths. For pytest and jest an unknown test in a known file is
// added to that file (dynamic / parametrised names).
func (t *Tree) match(ev Event) (*Case, string) {
	switch t.FW {
	case FrameworkGo:
		for _, p := range t.Packages {
			if p.Name != ev.Package {
				continue
			}
			for _, f := range p.Files {
				for _, c := range f.Cases {
					if c.Name == ev.Test {
						return c, p.Dir
					}
				}
			}
			if len(p.Files) > 0 && ev.Test != "" {
				f := p.Files[0]
				c := &Case{Name: ev.Test, File: f.Path}
				f.Cases = append(f.Cases, c)
				return c, p.Dir
			}
		}
		return nil, ""
	case FrameworkPytest:
		if ev.Class != "" {
			return t.matchPytestClass(ev)
		}
		return t.matchInFile(absPath(t.Root, ev.File), ev.Test)
	case FrameworkJest, FrameworkVitest:
		return t.matchInFile(absPath(t.Root, ev.File), ev.Test)
	case FrameworkCargo:
		return t.matchCargo(ev)
	}
	return nil, ""
}

func (t *Tree) matchInFile(abs, name string) (*Case, string) {
	_, f := t.FindFile(abs)
	if f == nil || name == "" {
		return nil, t.Root
	}
	for _, c := range f.Cases {
		if c.Name == name {
			return c, t.Root
		}
	}
	c := &Case{Name: name, File: f.Path}
	f.Cases = append(f.Cases, c)
	return c, t.Root
}

// matchPytestClass resolves a junit classname ("tests.test_a.TestX") to a
// file by its dotted module path ("tests.test_a").
func (t *Tree) matchPytestClass(ev Event) (*Case, string) {
	var best *File
	bestRest := ""
	for _, p := range t.Packages {
		for _, f := range p.Files {
			mod := strings.ReplaceAll(strings.TrimSuffix(f.Rel, ".py"), "/", ".")
			switch {
			case ev.Class == mod:
				if best == nil || len(mod) > len(strings.TrimSuffix(best.Rel, ".py")) {
					best, bestRest = f, ""
				}
			case strings.HasPrefix(ev.Class, mod+"."):
				if best == nil || len(mod) > len(strings.TrimSuffix(best.Rel, ".py")) {
					best, bestRest = f, strings.TrimPrefix(ev.Class, mod+".")
				}
			}
		}
	}
	if best == nil && ev.File != "" {
		return t.matchInFile(absPath(t.Root, ev.File), ev.Test)
	}
	if best == nil {
		return nil, t.Root
	}
	name := ev.Test
	if bestRest != "" {
		name = strings.ReplaceAll(bestRest, ".", "::") + "::" + ev.Test
	}
	return t.matchInFile(best.Path, name)
}

// matchCargo picks the case whose (module-qualified) name is the longest
// suffix of libtest's full test path, preferring the binary's own source.
func (t *Tree) matchCargo(ev Event) (*Case, string) {
	var best *Case
	bestLen := -1
	bestOwn := false
	src := absPath(t.Root, ev.Package)
	t.Each(func(_ *Package, f *File, c *Case) {
		if ev.Test != c.Name && !strings.HasSuffix(ev.Test, "::"+c.Name) {
			return
		}
		own := f.Path == src
		if len(c.Name) > bestLen || (len(c.Name) == bestLen && own && !bestOwn) {
			best, bestLen, bestOwn = c, len(c.Name), own
		}
	})
	return best, t.Root
}
