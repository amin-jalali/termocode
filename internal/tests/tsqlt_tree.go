package tests

import "strings"

// The tSQLt node of the Testing view (DBX-52): like pgTAP's, one package,
// one file per connection (not a file on disk), cases named Class.Test.

const tSQLtScheme = "tsqlt://"

// TSQLtConn returns the connection of a tSQLt tree file path.
func TSQLtConn(path string) (string, bool) {
	return strings.CutPrefix(path, tSQLtScheme)
}

// TSQLt returns the tSQLt package of the tree (nil before any run).
func (t *Tree) TSQLt() *Package {
	if t == nil {
		return nil
	}
	for _, p := range t.Packages {
		if p.Name == TSQLtPackage && p.Dir == "" {
			return p
		}
	}
	return nil
}

func (t *Tree) tSQLtFile(conn string) *File {
	p := t.TSQLt()
	if p == nil {
		p = &Package{Name: TSQLtPackage}
		t.Packages = append(t.Packages, p)
	}
	for _, f := range p.Files {
		if f.Path == tSQLtScheme+conn {
			return f
		}
	}
	f := &File{Path: tSQLtScheme + conn, Rel: conn}
	p.Files = append(p.Files, f)
	return f
}

// ResetTSQLt empties conn's tSQLt results before a new full run.
func (t *Tree) ResetTSQLt(conn string) {
	if t == nil {
		return
	}
	t.tSQLtFile(conn).Cases = nil
}

// ApplyTSQLt folds one tSQLt result of conn into the tree.
func (t *Tree) ApplyTSQLt(conn string, ev Event) *Case {
	if t == nil || ev.Kind != EventResult || ev.Test == "" {
		return nil
	}
	name := ev.Test
	if ev.Class != "" {
		name = ev.Class + "." + ev.Test
	}
	f := t.tSQLtFile(conn)
	var c *Case
	for _, x := range f.Cases {
		if x.Name == name {
			c = x
			break
		}
	}
	if c == nil {
		c = &Case{Name: name, File: f.Path}
		f.Cases = append(f.Cases, c)
	}
	c.Status = ev.Status
	c.Message, c.Output = ev.Message, ev.Output
	return c
}

// KeepTSQLt moves the tSQLt package of old into fresh (discovery only
// walks files, so it never finds database tests).
func KeepTSQLt(old, fresh *Tree) {
	if fresh == nil || fresh.TSQLt() != nil {
		return
	}
	if p := old.TSQLt(); p != nil {
		fresh.Packages = append(fresh.Packages, p)
	}
}
