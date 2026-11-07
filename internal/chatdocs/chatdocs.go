// Package chatdocs keeps the docs of one AI chat (CMP-94): Markdown
// files the user and the AI write together inside a chat (a plan, a
// spec, release notes), with every change kept as a revision.
//
// A chat's docs live in one folder of termocode's config
// (<config>/ai/docs/<chat id>/), never in the project:
//
//	plan.md               the current text (the user edits it in the editor)
//	plan.revs/0001-ai.md  revision 1, written by the AI
//	plan.revs/0002-you.md revision 2, the user's own edit
//
// A write that finds the file changed since the last revision first keeps
// the user's text as a "you" revision, so no edit is lost. Restoring an
// old revision is a new revision, so nothing is lost there either.
package chatdocs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/amin-jalali/termocode/internal/cfgfile"
)

// Authors of a revision.
const (
	AuthorAI  = "ai"
	AuthorYou = "you"
)

const (
	NameMax = 40         // runes of a doc name (its slug)
	TextMax = 256 * 1024 // bytes of one doc
	RevsMax = 50         // revisions kept per doc (oldest go first)
	DocsMax = 30         // docs per chat
)

// Doc is one doc of a chat.
type Doc struct {
	Name    string
	Revs    int // revisions kept
	Updated time.Time
}

// Rev is one revision.
type Rev struct {
	N      int
	Author string
	Time   time.Time
	path   string
}

// Store is the docs folder of one chat. A zero Store has no folder: it
// lists nothing and refuses writes.
type Store struct{ Dir string }

// ForChat is the store of chat id under the chats folder's parent
// (<config>/ai/chats → <config>/ai/docs/<id>).
func ForChat(chatsDir, id string) Store {
	if chatsDir == "" || id == "" || strings.ContainsAny(id, `/\`) || id == "." || id == ".." {
		return Store{}
	}
	return Store{Dir: filepath.Join(filepath.Dir(chatsDir), "docs", id)}
}

// Slug is the file name of a doc name: lower case letters and digits,
// runs of anything else as one '-'.
func Slug(name string) (string, error) {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	s := b.String()
	if r := []rune(s); len(r) > NameMax {
		s = strings.TrimRight(string(r[:NameMax]), "-")
	}
	if s == "" {
		return "", errors.New("a doc name needs a letter or a digit")
	}
	return s, nil
}

// Path is the current file of doc name (its slug; "" when the name has
// none or the store no folder).
func (s Store) Path(name string) string {
	slug, err := Slug(name)
	if err != nil || s.Dir == "" {
		return ""
	}
	return filepath.Join(s.Dir, slug+".md")
}

func (s Store) revDir(slug string) string { return filepath.Join(s.Dir, slug+".revs") }

// List is every doc, by name.
func (s Store) List() []Doc {
	if s.Dir == "" {
		return nil
	}
	ents, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	var out []Doc
	for _, e := range ents {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || e.IsDir() {
			continue
		}
		d := Doc{Name: name, Revs: len(s.Revisions(name))}
		if info, err := e.Info(); err == nil {
			d.Updated = info.ModTime()
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Read is the current text of doc name.
func (s Store) Read(name string) (string, error) {
	p := s.Path(name)
	if p == "" {
		return "", errors.New("no such doc")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("no doc %q in this chat", name)
	}
	return string(b), nil
}

// Revisions are the kept revisions of doc name, oldest first.
func (s Store) Revisions(name string) []Rev {
	slug, err := Slug(name)
	if err != nil || s.Dir == "" {
		return nil
	}
	ents, err := os.ReadDir(s.revDir(slug))
	if err != nil {
		return nil
	}
	var out []Rev
	for _, e := range ents {
		base, ok := strings.CutSuffix(e.Name(), ".md")
		num, author, ok2 := strings.Cut(base, "-")
		n, err := strconv.Atoi(num)
		if !ok || !ok2 || err != nil || n <= 0 {
			continue
		}
		r := Rev{N: n, Author: author, path: filepath.Join(s.revDir(slug), e.Name())}
		if info, err := e.Info(); err == nil {
			r.Time = info.ModTime()
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N < out[j].N })
	return out
}

// ReadRev is the text of revision n of doc name.
func (s Store) ReadRev(name string, n int) (string, error) {
	for _, r := range s.Revisions(name) {
		if r.N == n {
			b, err := os.ReadFile(r.path)
			return string(b), err
		}
	}
	return "", fmt.Errorf("doc %q has no revision %d", name, n)
}

// Write makes text the current text of doc name and keeps it as a new
// revision by author; it returns the revision number. The same text as
// now makes no revision. When the file was changed since the last
// revision (the user edited it), that text is kept first as a "you"
// revision.
func (s Store) Write(name, text, author string) (int, error) {
	slug, err := Slug(name)
	if err != nil {
		return 0, err
	}
	if s.Dir == "" {
		return 0, errors.New("this chat has no docs folder")
	}
	if len(text) > TextMax {
		return 0, fmt.Errorf("a doc is at most %d KB", TextMax/1024)
	}
	if author != AuthorAI && author != AuthorYou {
		return 0, fmt.Errorf("unknown author %q", author)
	}
	cur := filepath.Join(s.Dir, slug+".md")
	revs := s.Revisions(slug)
	last := 0
	if len(revs) > 0 {
		last = revs[len(revs)-1].N
	}
	now, nowErr := os.ReadFile(cur)
	if nowErr != nil && len(s.List()) >= DocsMax {
		return 0, fmt.Errorf("a chat has at most %d docs", DocsMax)
	}
	if nowErr == nil {
		lastText, _ := s.ReadRev(slug, last)
		if last == 0 || string(now) != lastText { // the user's edit
			last++
			if err := s.keep(slug, last, AuthorYou, string(now)); err != nil {
				return 0, err
			}
		}
		if string(now) == text {
			s.prune(slug)
			return last, nil
		}
	}
	last++
	if err := s.keep(slug, last, author, text); err != nil {
		return 0, err
	}
	if err := cfgfile.WriteFile(cur, []byte(text)); err != nil {
		return 0, err
	}
	s.prune(slug)
	return last, nil
}

// Snapshot keeps the user's edit of doc name as a revision when the file
// changed since the last one (before a history is shown).
func (s Store) Snapshot(name string) {
	if txt, err := s.Read(name); err == nil {
		_, _ = s.Write(name, txt, AuthorYou)
	}
}

// Restore makes revision n the current text again (a new revision).
func (s Store) Restore(name string, n int) (int, error) {
	txt, err := s.ReadRev(name, n)
	if err != nil {
		return 0, err
	}
	return s.Write(name, txt, AuthorYou)
}

func (s Store) keep(slug string, n int, author, text string) error {
	p := filepath.Join(s.revDir(slug), fmt.Sprintf("%04d-%s.md", n, author))
	return cfgfile.WriteFile(p, []byte(text))
}

// prune drops the oldest revisions over RevsMax.
func (s Store) prune(slug string) {
	revs := s.Revisions(slug)
	for len(revs) > RevsMax {
		_ = os.Remove(revs[0].path)
		revs = revs[1:]
	}
}
