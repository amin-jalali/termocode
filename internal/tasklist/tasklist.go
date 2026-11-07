// Package tasklist reads a markdown checklist (a plan's task list) into
// tasks with dependencies, so each open task can run as its own agent
// step, in waves (CMP-76).
//
// The format is a plain checklist:
//
//   - [ ] 1. Add the parser
//     Read docs/grammar.md first.        ← indented lines: the task's detail
//   - [ ] 2. Add the flag (after: 1)     ← deps by id
//   - [ ] Write the docs (id: docs) (after: 1, 2)
//   - [x] Done tasks are skipped, and so are the deps on them
//
// A task's id is its (id: …) tag, else its leading number ("2."), else
// task-N (N = its place in the list). Checkboxes in code fences are no
// tasks. Nothing here runs anything or does I/O.
package tasklist

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentdag"
)

// MaxTasks is the most open tasks one list runs (a run has at most
// agentdag.MaxNodes steps; one more is the final check).
const MaxTasks = agentdag.MaxNodes - 1

// MaxWaves is the most waves one list has (the final check is one more).
const MaxWaves = agentdag.MaxLayers - 1

// MaxDetail caps a task's detail (bytes).
const MaxDetail = 4 << 10

// Task is one open task.
type Task struct {
	ID     string
	Title  string
	Detail string   // the indented lines under it
	Deps   []string // ids of open tasks that must finish first
	Line   int      // 1-based line of the checkbox
}

// List is the open tasks of a checklist, in list order.
type List struct {
	Tasks []Task
	Done  int // tasks already ticked
}

var (
	itemRe  = regexp.MustCompile(`^(\s*)(?:[-*+]|\d+[.)])\s+\[([ xX])\]\s*(.*)$`)
	numRe   = regexp.MustCompile(`^(\d+(?:\.\d+)*)[.)]?\s+`)
	tagRe   = regexp.MustCompile(`\((id|after)\s*:\s*([^)]*)\)`)
	idRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	fenceRe = regexp.MustCompile("^\\s*(```|~~~)")
)

type raw struct {
	Task
	done   bool
	indent int
	after  []string
}

// Parse reads src. It fails when no task is open, an id is bad or used
// twice, a dep names no task, the deps make a cycle or there are more
// than MaxTasks open tasks.
func Parse(src string) (List, error) {
	var items []*raw
	var cur *raw
	inFence := false
	top := -1 // the indent of the list's checkboxes
	for i, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			if cur != nil && cur.indent < indentOf(line) {
				cur.Detail += strings.TrimSpace(line) + "\n"
			}
			continue
		}
		if inFence {
			if cur != nil && (strings.TrimSpace(line) == "" || indentOf(line) > cur.indent) {
				cur.Detail += strings.TrimRight(line, " \t") + "\n"
			}
			continue
		}
		if m := itemRe.FindStringSubmatch(line); m != nil && (top < 0 || len(m[1]) <= top) {
			top = len(m[1])
			cur = &raw{indent: len(m[1]), done: m[2] != " "}
			cur.Line = i + 1
			cur.Title = m[3]
			items = append(items, cur)
			continue
		}
		switch {
		case cur == nil:
		case strings.TrimSpace(line) == "":
			if cur.Detail != "" {
				cur.Detail += "\n"
			}
		case indentOf(line) > cur.indent:
			cur.Detail += strings.TrimSpace(line) + "\n"
		default:
			cur = nil // text at the list's level ends the task
		}
	}

	var l List
	ids := map[string]int{}   // id → index in items
	open := map[string]bool{} // open ids
	for n, it := range items {
		title := it.Title
		var tagID string
		for _, m := range tagRe.FindAllStringSubmatch(title, -1) {
			switch m[1] {
			case "id":
				tagID = strings.TrimSpace(m[2])
			case "after":
				for _, d := range strings.Split(m[2], ",") {
					if d = strings.TrimSpace(d); d != "" {
						it.after = append(it.after, d)
					}
				}
			}
		}
		title = strings.TrimSpace(tagRe.ReplaceAllString(title, ""))
		id := tagID
		if nm := numRe.FindStringSubmatch(title); nm != nil {
			if id == "" {
				id = nm[1]
			}
			title = strings.TrimSpace(title[len(nm[0]):])
		}
		if id == "" {
			id = "task-" + strconv.Itoa(n+1)
		}
		switch {
		case !idRe.MatchString(id):
			return List{}, fmt.Errorf("line %d: the id %q may have only letters, digits, '-', '_' and '.'", it.Line, id)
		case ids[id] != 0:
			return List{}, fmt.Errorf("line %d: the id %q is used twice (also line %d)", it.Line, id, items[ids[id]-1].Line)
		}
		ids[id] = n + 1
		if title == "" && !it.done {
			return List{}, fmt.Errorf("line %d: the task has no title", it.Line)
		}
		it.ID, it.Title = id, title
		it.Detail = strings.TrimSpace(it.Detail)
		if len(it.Detail) > MaxDetail {
			it.Detail = it.Detail[:MaxDetail] + "\n…"
		}
		if it.done {
			l.Done++
		} else {
			open[id] = true
		}
	}
	for _, it := range items {
		if it.done {
			continue
		}
		seen := map[string]bool{}
		for _, d := range it.after {
			if ids[d] == 0 {
				return List{}, fmt.Errorf("line %d: after: names an unknown task %q", it.Line, d)
			}
			if d == it.ID {
				return List{}, fmt.Errorf("line %d: a task cannot come after itself", it.Line)
			}
			if open[d] && !seen[d] { // a done task is no dep any more
				seen[d] = true
				it.Deps = append(it.Deps, d)
			}
		}
		l.Tasks = append(l.Tasks, it.Task)
	}
	switch {
	case len(l.Tasks) == 0:
		return List{}, errors.New("no open tasks: write them as a checklist, like \"- [ ] Add the parser\"")
	case len(l.Tasks) > MaxTasks:
		return List{}, fmt.Errorf("%d open tasks; one list runs at most %d (split it, or tick the done ones)", len(l.Tasks), MaxTasks)
	}
	w, err := l.Waves()
	if err != nil {
		return List{}, err
	}
	if len(w) > MaxWaves {
		return List{}, fmt.Errorf("the after: tags make %d waves; one list runs at most %d (fewer after: tags run more at once)", len(w), MaxWaves)
	}
	return l, nil
}

func indentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " \t")) }

// run is the list as a graph.
func (l List) run() agentdag.Run {
	r := agentdag.Run{ID: "tasks"}
	for _, t := range l.Tasks {
		r.Nodes = append(r.Nodes, agentdag.Node{ID: t.ID, Title: t.Title})
		for _, d := range t.Deps {
			r.Edges = append(r.Edges, agentdag.Edge{From: d, To: t.ID})
		}
	}
	return r
}

// Waves is the task ids by wave: a wave's tasks need only tasks of
// earlier waves, so they can run at the same time.
func (l List) Waves() ([][]string, error) {
	if len(l.Tasks) == 0 {
		return nil, errors.New("no open tasks")
	}
	w, err := agentdag.Layers(l.run())
	if err != nil {
		if strings.Contains(err.Error(), "cycle") {
			return nil, errors.New("the after: tags make a cycle: some task waits for itself")
		}
		return nil, err
	}
	return w, nil
}

// Leaves are the tasks no other open task waits for, in list order.
func (l List) Leaves() []string {
	needed := map[string]bool{}
	for _, t := range l.Tasks {
		for _, d := range t.Deps {
			needed[d] = true
		}
	}
	var out []string
	for _, t := range l.Tasks {
		if !needed[t.ID] {
			out = append(out, t.ID)
		}
	}
	return out
}

// Get is the task with id.
func (l List) Get(id string) (Task, bool) {
	for _, t := range l.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}
