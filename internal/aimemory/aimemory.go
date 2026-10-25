// Package aimemory is the AI chat's memory (OM-2): short lessons the AI
// wrote after earlier turns in one repository, kept in one Markdown file,
// .termocode/memory/lessons.md, in the workspace. The package is pure: it
// parses, caps, selects and writes the file; the app decides when.
//
// File format, one section per lesson, oldest first:
//
//	## 2026-10-25 · Run go test before claiming a fix {#a1b2c3d4}
//	The tests in internal/app take ~10 minutes; use -run for one area.
//	paths: internal/app/ai_chat.go
//
// The user may edit the file by hand: a section without an id gets one
// from its text, and lines outside sections are ignored.
package aimemory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// MaxLessons and MaxBytes cap the file: the oldest lessons go first.
	MaxLessons = 200
	MaxBytes   = 64 << 10
	// DirRel is the memory folder relative to the workspace root; File is
	// the lessons file in it.
	DirRel = ".termocode/memory"
	File   = "lessons.md"
	// readMax is how much of the file is read at most.
	readMax = 4 * MaxBytes
)

// header starts the file (it is not a lesson).
const header = "# termocode AI memory\n\n" +
	"Lessons the AI wrote after earlier chats in this repository. Edit or delete them freely.\n" +
	"One section per lesson; AI: Memory… in termocode lists them.\n"

// Lesson is one section of the file.
type Lesson struct {
	ID    string
	Date  string // YYYY-MM-DD
	Title string
	Text  string
	Paths []string // workspace-relative paths the lesson is about
}

// Dir is the memory folder of a workspace.
func Dir(root string) string { return filepath.Join(root, filepath.FromSlash(DirRel)) }

// Path is the lessons file of a workspace.
func Path(root string) string { return filepath.Join(Dir(root), File) }

var sectionRe = regexp.MustCompile(`^## (\d{4}-\d{2}-\d{2}) · (.*?)(?: \{#([0-9A-Za-z_-]{1,32})\})?\s*$`)

// Parse reads the lessons of a file. Sections it cannot read are skipped.
func Parse(b []byte) []Lesson {
	var out []Lesson
	var cur *Lesson
	var body []string
	flush := func() {
		if cur == nil {
			return
		}
		for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
			body = body[:len(body)-1]
		}
		if n := len(body); n > 0 && strings.HasPrefix(body[n-1], "paths:") {
			for _, p := range strings.Split(strings.TrimPrefix(body[n-1], "paths:"), ",") {
				if p = strings.TrimSpace(p); p != "" {
					cur.Paths = append(cur.Paths, p)
				}
			}
			body = body[:n-1]
		}
		cur.Text = strings.TrimSpace(strings.Join(body, "\n"))
		if cur.ID == "" {
			cur.ID = NewID(cur.Title, cur.Text)
		}
		if cur.Title != "" {
			out = append(out, *cur)
		}
		cur, body = nil, nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			flush()
			cur = &Lesson{Date: m[1], Title: strings.TrimSpace(m[2]), ID: m[3]}
			continue
		}
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# ") {
			flush() // a heading that is not a lesson ends the section
			continue
		}
		if cur != nil {
			body = append(body, line)
		}
	}
	flush()
	return out
}

// Marshal writes lessons in the file format.
func Marshal(ls []Lesson) []byte {
	var b strings.Builder
	b.WriteString(header)
	for _, l := range ls {
		fmt.Fprintf(&b, "\n## %s · %s {#%s}\n", l.Date, l.Title, l.ID)
		if l.Text != "" {
			b.WriteString(l.Text + "\n")
		}
		if len(l.Paths) > 0 {
			b.WriteString("paths: " + strings.Join(l.Paths, ", ") + "\n")
		}
	}
	return []byte(b.String())
}

// NewID is a lesson's id: a short hash of its title and text, so the same
// lesson written twice has the same id.
func NewID(title, text string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(title)) + "\n" + strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:4])
}

// Forget returns ls without the lesson id.
func Forget(ls []Lesson, id string) []Lesson {
	out := make([]Lesson, 0, len(ls))
	for _, l := range ls {
		if l.ID != id {
			out = append(out, l)
		}
	}
	return out
}

// Append adds lessons at the end (newest last), skipping one whose id is
// there already, and drops the oldest until the caps hold. A lesson
// without an id gets NewID.
func Append(ls []Lesson, add ...Lesson) []Lesson {
	out := append([]Lesson(nil), ls...)
	seen := map[string]bool{}
	for _, l := range out {
		seen[l.ID] = true
	}
	for _, l := range add {
		if l.ID == "" {
			l.ID = NewID(l.Title, l.Text)
		}
		if seen[l.ID] {
			continue
		}
		seen[l.ID] = true
		out = append(out, l)
	}
	if len(out) > MaxLessons {
		out = out[len(out)-MaxLessons:]
	}
	for len(out) > 1 && len(Marshal(out)) > MaxBytes {
		out = out[1:]
	}
	return out
}

// Load reads the lessons of a workspace. A missing file is no lessons; a
// file that is not a regular file (a link someone committed) is ignored.
func Load(root string) ([]Lesson, error) {
	p := Path(root)
	st, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filepath.ToSlash(filepath.Join(DirRel, File)))
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, readMax))
	if err != nil {
		return nil, err
	}
	return Parse(b), nil
}

// Save writes the lessons of a workspace atomically (a temp file renamed
// over the old one). The folder is made when missing; a linked
// .termocode or memory folder is refused, so a repository cannot point
// the write elsewhere.
func Save(root string, ls []Lesson) error {
	for _, d := range []string{filepath.Join(root, ".termocode"), Dir(root)} {
		st, err := os.Lstat(d)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(d, 0o755); err != nil {
				return err
			}
		case err != nil:
			return err
		case !st.IsDir():
			return fmt.Errorf("%s is not a folder", d)
		}
	}
	tmp, err := os.CreateTemp(Dir(root), ".lessons-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(Marshal(ls))
	if err := tmp.Chmod(0o644); werr == nil {
		werr = err
	}
	if err := tmp.Close(); werr == nil {
		werr = err
	}
	if werr != nil {
		_ = os.Remove(name)
		return werr
	}
	if err := os.Rename(name, Path(root)); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
