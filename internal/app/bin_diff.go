package app

import (
	"io"
	"os"
	"path/filepath"

	"github.com/amin-jalali/termocode/internal/bindiff"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/textdiff"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Image and PDF diffs (CMP-54). Git calls an image or a PDF "binary", so
// the diff card only said "Binary file". For the files people can still
// read, internal/bindiff turns each side into a few lines of text (size,
// changed pixels and their box; the PDF's text) and the normal text diff
// shows what changed. Used by the commit / compare card, Compare Two
// Files and the web's git diff sheet.

// binDiffSides returns readable sides for an image or a PDF; other files
// pass through unchanged (ok=false).
func binDiffSides(name, old, new string) (string, string, bool) {
	o, n, ok := bindiff.Describe(name, []byte(old), []byte(new))
	if !ok {
		return old, new, false
	}
	return o, n, true
}

// webGitBinaryRows is the web diff of a binary image or PDF: the described
// sides as ctx / add / del rows under one hunk. It is not marked binary,
// so the diff sheet shows the rows.
func webGitBinaryRows(name, old, new string) (webGitParsed, bool) {
	o, n, ok := binDiffSides(name, old, new)
	if !ok {
		return webGitParsed{}, false
	}
	d := textdiff.Compute(o, n)
	p := webGitParsed{rows: []webproto.DiffRow{{K: "hunk", Text: "@@ " + name + " (compared as readable text) @@", Old: 1, New: 1}}}
	for _, r := range d.Rows {
		switch r.Kind {
		case textdiff.Added:
			p.rows = append(p.rows, webproto.DiffRow{K: "add", Text: r.Text, New: r.NewLine})
			p.added++
		case textdiff.Removed:
			p.rows = append(p.rows, webproto.DiffRow{K: "del", Text: r.Text, Old: r.OldLine})
			p.removed++
		default:
			p.rows = append(p.rows, webproto.DiffRow{K: "ctx", Text: r.Text, Old: r.OldLine, New: r.NewLine})
		}
	}
	return p, true
}

// webGitBinarySides reads the two sides of a changed file for the web
// diff: staged = HEAD vs the index, else the index vs the working tree
// (an untracked file has no old side).
func webGitBinarySides(root, path string, staged, untracked bool) (old, new string) {
	switch {
	case staged:
		old, _ = git.ShowFileAtRevision(root, "HEAD", path)
		new, _ = git.ShowFileAtRevision(root, "", path)
	default:
		if !untracked {
			old, _ = git.ShowFileAtRevision(root, "", path)
		}
		new = webGitReadWork(root, path)
	}
	return old, new
}

// webGitReadWork reads a working-tree file (at most bindiff.MaxBytes+1
// bytes, so a huge file shows the too-big note); "" when it is gone.
func webGitReadWork(root, path string) string {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return ""
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, bindiff.MaxBytes+1))
	return string(b)
}
