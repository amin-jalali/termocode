// Package tracker is a task list that agents and people share (CMP-81).
//
// Tasks are plain Markdown files in <main checkout>/.termocode/tasks/,
// one per task, named "0007-fix-the-login.md". Every worktree of the
// repository sees the same folder, so an agent in one lane can take a
// task another lane added. A file starts with a small header:
//
//	---
//	id: 7
//	title: Fix the login
//	status: todo
//	owner: claude
//	worktree: /src/app.worktrees/fix-login
//	created: 2026-11-07T10:00:00Z
//	updated: 2026-11-07T10:00:00Z
//	---
//	free text, notes below
//
// People edit the files in any editor; the header fields are one line
// each. Text in a task is data from whoever wrote it, never a command.
package tracker

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/amin-jalali/termocode/internal/git"
)

// Status values.
const (
	Todo    = "todo"
	Doing   = "doing"
	Blocked = "blocked"
	Done    = "done"
)

// Statuses are the allowed status values, in board order.
var Statuses = []string{Todo, Doing, Blocked, Done}

// Limits.
const (
	// MaxFileBytes caps one task file; a larger file is skipped.
	MaxFileBytes = 64 << 10
	// MaxTasks caps the files read from the folder.
	MaxTasks = 2000
	// MaxTitle caps a title (letters).
	MaxTitle = 200
	// MaxField caps owner and worktree.
	MaxField = 300
	// MaxNote caps one note line.
	MaxNote = 500
)

// DirName is the folder under the main checkout.
const DirName = ".termocode/tasks"

// Task is one task file.
type Task struct {
	ID       int       `json:"id"`
	Title    string    `json:"title"`
	Status   string    `json:"status"`
	Owner    string    `json:"owner,omitempty"`
	Worktree string    `json:"worktree,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Body     string    `json:"body,omitempty"`
	File     string    `json:"file"`
}

// ValidStatus reports whether s is a status.
func ValidStatus(s string) bool {
	for _, v := range Statuses {
		if s == v {
			return true
		}
	}
	return false
}

// Dir is the tasks folder of the repository dir is in: under its main
// checkout, so all worktrees share it. Outside a repository (or for a
// bare one) it is under dir itself.
func Dir(dir string) string {
	if ws, _, err := git.ListWorktrees(dir); err == nil {
		for _, w := range ws {
			if w.Main && !w.Bare && w.Path != "" {
				return filepath.Join(w.Path, filepath.FromSlash(DirName))
			}
		}
	}
	return filepath.Join(dir, filepath.FromSlash(DirName))
}

// Here is the worktree dir is in ("" outside a repository): the
// longest worktree path that holds dir.
func Here(dir string) string {
	ws, _, err := git.ListWorktrees(dir)
	if err != nil {
		return ""
	}
	best := ""
	clean := filepath.Clean(dir)
	for _, w := range ws {
		p := filepath.Clean(w.Path)
		if (clean == p || strings.HasPrefix(clean, p+string(filepath.Separator))) && len(p) > len(best) {
			best = p
		}
	}
	return best
}

// List reads the tasks in dir, by id. A missing folder is no tasks.
// Files that are too big or have no valid header are skipped.
func List(dir string) ([]Task, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Task
	for _, e := range ents {
		if len(out) >= MaxTasks {
			break
		}
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		t, err := readFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Find reads task id.
func Find(dir string, id int) (Task, error) {
	ts, err := List(dir)
	if err != nil {
		return Task{}, err
	}
	for _, t := range ts {
		if t.ID == id {
			return t, nil
		}
	}
	return Task{}, fmt.Errorf("no task %d", id)
}

// Add makes a new task with the next free id.
func Add(dir, title, body string, now time.Time) (Task, error) {
	title = Clean(title, MaxTitle)
	if title == "" {
		return Task{}, errors.New("a task needs a title")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Task{}, err
	}
	ts, err := List(dir)
	if err != nil {
		return Task{}, err
	}
	next := 1
	for _, t := range ts {
		if t.ID >= next {
			next = t.ID + 1
		}
	}
	now = now.UTC().Truncate(time.Second)
	t := Task{Title: title, Status: Todo, Created: now, Updated: now, Body: strings.TrimSpace(body)}
	// An id is taken by making .ids/<id> (mkdir is atomic): two agents
	// adding at once never get the same id, and a deleted task's id is
	// not used again.
	ids := filepath.Join(dir, ".ids")
	if err := os.MkdirAll(ids, 0o755); err != nil {
		return Task{}, err
	}
	for try := 0; try < 1000; try++ {
		t.ID = next + try
		if err := os.Mkdir(filepath.Join(ids, strconv.Itoa(t.ID)), 0o755); errors.Is(err, os.ErrExist) {
			continue
		} else if err != nil {
			return Task{}, err
		}
		t.File = filepath.Join(dir, fileName(t.ID, title))
		f, err := os.OpenFile(t.File, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return Task{}, err
		}
		_, werr := f.WriteString(Format(t))
		cerr := f.Close()
		if werr != nil || cerr != nil {
			_ = os.Remove(t.File)
			return Task{}, errors.Join(werr, cerr)
		}
		return t, nil
	}
	return Task{}, errors.New("no free task id")
}

// Update changes task id with fn and writes it back (a temp file and a
// rename, so a reader never sees half a file). Updated is set to now.
func Update(dir string, id int, now time.Time, fn func(*Task) error) (Task, error) {
	t, err := Find(dir, id)
	if err != nil {
		return Task{}, err
	}
	if err := fn(&t); err != nil {
		return Task{}, err
	}
	t.Title = Clean(t.Title, MaxTitle)
	t.Owner = Clean(t.Owner, MaxField)
	t.Worktree = Clean(t.Worktree, MaxField)
	if !ValidStatus(t.Status) {
		return Task{}, fmt.Errorf("status must be one of %s", strings.Join(Statuses, ", "))
	}
	t.Updated = now.UTC().Truncate(time.Second)
	tmp, err := os.CreateTemp(dir, ".task-*.tmp")
	if err != nil {
		return Task{}, err
	}
	_, werr := tmp.WriteString(Format(t))
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return Task{}, errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp.Name(), t.File); err != nil {
		_ = os.Remove(tmp.Name())
		return Task{}, err
	}
	return t, nil
}

// SetStatus sets the status (and the owner when one is given).
func SetStatus(dir string, id int, status, owner, worktree string, now time.Time) (Task, error) {
	if !ValidStatus(status) {
		return Task{}, fmt.Errorf("status must be one of %s", strings.Join(Statuses, ", "))
	}
	return Update(dir, id, now, func(t *Task) error {
		t.Status = status
		if owner != "" {
			t.Owner = owner
		}
		if worktree != "" {
			t.Worktree = worktree
		}
		return nil
	})
}

// Claim takes a todo or blocked task: it becomes doing with the owner.
// A task someone else is doing is not taken.
func Claim(dir string, id int, owner, worktree string, now time.Time) (Task, error) {
	return Update(dir, id, now, func(t *Task) error {
		switch {
		case t.Status == Done:
			return fmt.Errorf("task %d is done", id)
		case t.Status == Doing && t.Owner != "" && t.Owner != Clean(owner, MaxField):
			return fmt.Errorf("task %d is taken by %s", id, t.Owner)
		}
		t.Status, t.Owner = Doing, owner
		if worktree != "" {
			t.Worktree = worktree
		}
		return nil
	})
}

// Note adds a line "- 2026-11-07 10:04 owner: text" under the body.
func Note(dir string, id int, owner, text string, now time.Time) (Task, error) {
	text = Clean(text, MaxNote)
	if text == "" {
		return Task{}, errors.New("a note needs text")
	}
	return Update(dir, id, now, func(t *Task) error {
		line := "- " + now.Local().Format("2006-01-02 15:04")
		if o := Clean(owner, MaxField); o != "" {
			line += " " + o
		}
		line += ": " + text
		if t.Body != "" {
			t.Body += "\n"
		}
		t.Body += line
		return nil
	})
}

// Format is the file text of t.
func Format(t Task) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %d\n", t.ID)
	fmt.Fprintf(&b, "title: %s\n", Clean(t.Title, MaxTitle))
	fmt.Fprintf(&b, "status: %s\n", t.Status)
	if t.Owner != "" {
		fmt.Fprintf(&b, "owner: %s\n", Clean(t.Owner, MaxField))
	}
	if t.Worktree != "" {
		fmt.Fprintf(&b, "worktree: %s\n", Clean(t.Worktree, MaxField))
	}
	fmt.Fprintf(&b, "created: %s\n", t.Created.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "updated: %s\n", t.Updated.UTC().Format(time.RFC3339))
	b.WriteString("---\n")
	if body := strings.TrimSpace(t.Body); body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.String()
}

// Parse reads a task file's text. The header must have an id > 0 and a
// title; an unknown status reads as todo.
func Parse(text string) (Task, error) {
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 4096), MaxFileBytes+1)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return Task{}, errors.New("no header")
	}
	var t Task
	closed := false
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			closed = true
			break
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "id":
			t.ID, _ = strconv.Atoi(v)
		case "title":
			t.Title = Clean(v, MaxTitle)
		case "status":
			t.Status = strings.ToLower(v)
		case "owner":
			t.Owner = Clean(v, MaxField)
		case "worktree":
			t.Worktree = Clean(v, MaxField)
		case "created":
			t.Created, _ = time.Parse(time.RFC3339, v)
		case "updated":
			t.Updated, _ = time.Parse(time.RFC3339, v)
		}
	}
	if !closed || t.ID <= 0 || t.Title == "" {
		return Task{}, errors.New("bad header")
	}
	if !ValidStatus(t.Status) {
		t.Status = Todo
	}
	var body []string
	for sc.Scan() {
		body = append(body, sc.Text())
	}
	t.Body = strings.TrimSpace(strings.Join(body, "\n"))
	return t, nil
}

func readFile(path string) (Task, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return Task{}, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > MaxFileBytes {
		return Task{}, errors.New("skipped")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Task{}, err
	}
	t, err := Parse(string(b))
	if err != nil {
		return Task{}, err
	}
	t.File = path
	return t, nil
}

// Clean makes s one safe line: control bytes and runs of space become
// one space, at most max letters.
func Clean(s string, max int) string {
	var b strings.Builder
	n := 0
	space := false
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		if n >= max {
			break
		}
		b.WriteRune(r)
		space = false
		n++
	}
	return strings.TrimSpace(b.String())
}

// fileName is "0007-fix-the-login.md".
func fileName(id int, title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		slug = "task"
	}
	return fmt.Sprintf("%04d-%s.md", id, slug)
}
