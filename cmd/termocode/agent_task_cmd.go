package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
)

// `termocode agent start` and `termocode agent tasks` (CMP-39): hand a
// task to a new background agent in a new worktree, and list the
// worktrees agents work in. start waits for the user's answer on the
// New worktree card termocode shows filled in.

// agentGetwd is the current folder (a seam for tests).
var agentGetwd = os.Getwd

func runAgentStart(args []string, in io.Reader, out, errw io.Writer, fail func(string, ...any) int, usage func() int, sock string) int {
	req := agentctl.Request{Op: agentctl.OpTaskStart}
	asJSON := false
	var words []string
	flag := func(i *int, name string, dst *string) bool {
		a := args[*i]
		switch {
		case a == "--"+name && *i+1 < len(args):
			*i++
			*dst = args[*i]
			return true
		case strings.HasPrefix(a, "--"+name+"="):
			*dst = strings.TrimPrefix(a, "--"+name+"=")
			return true
		}
		return false
	}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case flag(&i, "agent", &req.Agent), flag(&i, "name", &req.Name), flag(&i, "from", &req.From):
		case a == "--json":
			asJSON = true
		case a == "--":
			words = append(words, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "--"):
			return usage()
		default:
			words = append(words, a)
		}
	}
	text := strings.Join(words, " ")
	if text == "-" {
		b, err := io.ReadAll(io.LimitReader(in, 32<<10+1))
		if err != nil {
			return fail("read stdin: %v", err)
		}
		text = string(b)
	}
	if strings.TrimSpace(text) == "" {
		return usage()
	}
	req.Text = text
	if dir, err := agentGetwd(); err == nil {
		req.Dir = dir
	}
	rep, err := agentCall(sock, req, agentctl.TaskStartTimeout+30*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	if asJSON {
		_ = json.NewEncoder(out).Encode(map[string]any{"state": rep.State, "worktrees": rep.Worktrees})
		return 0
	}
	if len(rep.Worktrees) == 0 {
		return fail("termocode made no worktree")
	}
	w := rep.Worktrees[0]
	fmt.Fprintln(out, w.Path)
	if rep.State != "started" {
		fmt.Fprintln(errw, "termocode agent start: the worktree was made, but no agent was started")
	}
	return 0
}

func runAgentTasks(args []string, out io.Writer, fail func(string, ...any) int, usage func() int, sock string) int {
	asJSON := false
	for _, a := range args {
		if a != "--json" {
			return usage()
		}
		asJSON = true
	}
	req := agentctl.Request{Op: agentctl.OpWorktreeList}
	if dir, err := agentGetwd(); err == nil {
		req.Dir = dir
	}
	rep, err := agentCall(sock, req, 30*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	var tasks []agentctl.Worktree
	for _, w := range rep.Worktrees {
		if w.Agent != "" || w.Kind == "agent" {
			tasks = append(tasks, w)
		}
	}
	if asJSON {
		if tasks == nil {
			tasks = []agentctl.Worktree{}
		}
		_ = json.NewEncoder(out).Encode(tasks)
		return 0
	}
	tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "BRANCH\tAGENT\tSTATUS\tLINK\tNOTE\tPATH")
	for _, w := range tasks {
		b := dash(w.Branch)
		if w.Gone {
			b += " (gone)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", b, dash(w.Agent), dash(w.Checkpoint), dash(w.Link), dash(w.Note), w.Path)
	}
	_ = tw.Flush()
	return 0
}
