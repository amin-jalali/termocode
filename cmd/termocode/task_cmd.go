package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/tracker"
)

// `termocode task …` (CMP-81): the shared task list of the repository,
// plain Markdown files in <main checkout>/.termocode/tasks/. It works on
// the files directly, so it needs no running termocode; agents in any
// worktree of the repository see the same tasks.

const taskUsage = `termocode task: the task list agents and people share

  termocode task list [--all] [--json]
        the open tasks (--all: done ones too)
  termocode task add <title…>
        add a task (status todo); prints its id
  termocode task show <id>
        a task with its notes
  termocode task claim <id> [--as <name>]
        take a task: it becomes doing, owned by you, in this worktree
  termocode task status <id> <todo|doing|blocked|done>
        set a task's status
  termocode task note <id> <text…>
        add a note line to a task (at most 500 characters)

Tasks are files in .termocode/tasks/ of the main checkout, one per task.
Edit them in any editor. The owner is --as, else $TERMOCODE_TASK_OWNER,
else the termocode tab, else $USER.
`

func runTask(args []string) int {
	return runTaskIn(args, os.Stdout, os.Stderr, os.Getenv, os.Getwd, time.Now)
}

func runTaskIn(args []string, out, errw io.Writer, getenv func(string) string, getwd func() (string, error), now func() time.Time) int {
	fail := func(f string, a ...any) int {
		fmt.Fprintf(errw, "termocode task: "+f+"\n", a...)
		return 1
	}
	usage := func() int { fmt.Fprint(errw, taskUsage); return 2 }
	if len(args) == 0 {
		return usage()
	}
	if a := args[0]; a == "-h" || a == "--help" || a == "help" {
		fmt.Fprint(out, taskUsage)
		return 0
	}
	cwd, err := getwd()
	if err != nil {
		return fail("no current folder: %v", err)
	}
	dir := taskDir(cwd)
	idArg := func() (int, bool) {
		if len(args) < 2 {
			return 0, false
		}
		n, err := strconv.Atoi(strings.TrimPrefix(args[1], "#"))
		return n, err == nil && n > 0
	}
	switch args[0] {
	case "list":
		all, js := false, false
		for _, a := range args[1:] {
			switch a {
			case "--all":
				all = true
			case "--json":
				js = true
			default:
				return usage()
			}
		}
		ts, err := tracker.List(dir)
		if err != nil {
			return fail("%v", err)
		}
		var shown []tracker.Task
		for _, t := range ts {
			if all || t.Status != tracker.Done {
				shown = append(shown, t)
			}
		}
		if js {
			if shown == nil {
				shown = []tracker.Task{}
			}
			_ = json.NewEncoder(out).Encode(shown)
			return 0
		}
		if len(shown) == 0 {
			fmt.Fprintln(out, "No open tasks. Add one: termocode task add <title>")
			return 0
		}
		tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tSTATUS\tOWNER\tTITLE")
		for _, t := range shown {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", t.ID, t.Status, dash(t.Owner), t.Title)
		}
		_ = tw.Flush()
		return 0
	case "add":
		if len(args) < 2 {
			return usage()
		}
		t, err := tracker.Add(dir, strings.Join(args[1:], " "), "", now())
		if err != nil {
			return fail("%v", err)
		}
		fmt.Fprintf(out, "%d\n", t.ID)
		return 0
	case "show":
		id, ok := idArg()
		if !ok || len(args) != 2 {
			return usage()
		}
		t, err := tracker.Find(dir, id)
		if err != nil {
			return fail("%v", err)
		}
		fmt.Fprintf(out, "#%d %s\nstatus: %s\n", t.ID, t.Title, t.Status)
		if t.Owner != "" {
			fmt.Fprintf(out, "owner: %s\n", t.Owner)
		}
		if t.Worktree != "" {
			fmt.Fprintf(out, "worktree: %s\n", t.Worktree)
		}
		fmt.Fprintf(out, "file: %s\n", t.File)
		if t.Body != "" {
			fmt.Fprintf(out, "\n%s\n", t.Body)
		}
		return 0
	case "claim":
		id, ok := idArg()
		if !ok {
			return usage()
		}
		as := ""
		switch {
		case len(args) == 2:
		case len(args) == 4 && args[2] == "--as":
			as = args[3]
		default:
			return usage()
		}
		t, err := tracker.Claim(dir, id, taskOwner(as, getenv), taskHere(cwd), now())
		if err != nil {
			return fail("%v", err)
		}
		fmt.Fprintf(out, "#%d is yours: %s\n", t.ID, t.Title)
		return 0
	case "status":
		id, ok := idArg()
		if !ok || len(args) != 3 {
			return usage()
		}
		if !tracker.ValidStatus(args[2]) {
			return fail("status must be one of %s", strings.Join(tracker.Statuses, ", "))
		}
		if _, err := tracker.SetStatus(dir, id, args[2], "", "", now()); err != nil {
			return fail("%v", err)
		}
		return 0
	case "note":
		id, ok := idArg()
		if !ok || len(args) < 3 {
			return usage()
		}
		if _, err := tracker.Note(dir, id, taskOwner("", getenv), strings.Join(args[2:], " "), now()); err != nil {
			return fail("%v", err)
		}
		return 0
	}
	return usage()
}

// taskDir and taskHere are vars for tests (no real git there).
var (
	taskDir  = tracker.Dir
	taskHere = tracker.Here
)

// taskOwner is who claims or notes: --as, $TERMOCODE_TASK_OWNER, the
// termocode tab, then $USER.
func taskOwner(as string, getenv func(string) string) string {
	for _, s := range []string{as, getenv("TERMOCODE_TASK_OWNER")} {
		if s = tracker.Clean(s, tracker.MaxField); s != "" {
			return s
		}
	}
	if tab := tracker.Clean(getenv(agentctl.EnvTab), 40); tab != "" {
		return "agent " + tab
	}
	return tracker.Clean(getenv("USER"), tracker.MaxField)
}
