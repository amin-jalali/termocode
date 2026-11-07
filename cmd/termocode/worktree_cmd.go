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
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// `termocode worktree list|note|status|link|block|unblock` (WTX-12, CMP-80): an agent (or you)
// leaves a note or a status on the worktree it works in, over the
// control socket of the running termocode. The worktree is the one the
// current folder is in.

const worktreeUsage = `termocode worktree: notes and status for the worktree you are in

  termocode worktree list [--json]
        the worktrees: branch, status, link and last note
  termocode worktree note <text…>
        leave a note on this worktree (one line, at most 500 characters)
  termocode worktree status <working|blocked|done>
        set this worktree's status
  termocode worktree link <number|#N|URL>
        link an issue or pull request (termocode asks you first)
  termocode worktree block <worktree>
        this worktree's work waits for the other one (a path, branch or name)
  termocode worktree unblock <worktree>
        it no longer waits for it

The worktree is the one the current folder is in. Notes are shown as plain
text in termocode's WORKTREES view.
`

func runWorktree(args []string) int {
	return runWorktreeIn(args, os.Stdout, os.Stderr, os.Getenv, os.Getwd)
}

func runWorktreeIn(args []string, out, errw io.Writer, getenv func(string) string, getwd func() (string, error)) int {
	fail := func(f string, a ...any) int {
		fmt.Fprintf(errw, "termocode worktree: "+f+"\n", a...)
		return 1
	}
	usage := func() int { fmt.Fprint(errw, worktreeUsage); return 2 }
	if len(args) == 0 {
		return usage()
	}
	if a := args[0]; a == "-h" || a == "--help" || a == "help" {
		fmt.Fprint(out, worktreeUsage)
		return 0
	}
	dir, err := getwd()
	if err != nil {
		return fail("no current folder: %v", err)
	}
	req := agentctl.Request{Dir: dir, Tab: getenv(agentctl.EnvTab)}
	timeout := 30 * time.Second
	switch args[0] {
	case "list":
		req.Op = agentctl.OpWorktreeList
	case "note":
		if len(args) < 2 {
			return usage()
		}
		req.Op, req.Text = agentctl.OpWorktreeNote, strings.Join(args[1:], " ")
	case "status":
		if len(args) != 2 {
			return usage()
		}
		if !worktrees.ValidCheckpoint(args[1]) {
			return fail("status must be one of %s", strings.Join(worktrees.Checkpoints, ", "))
		}
		req.Op, req.State = agentctl.OpWorktreeStatus, args[1]
	case "link":
		if len(args) != 2 {
			return usage()
		}
		req.Op, req.Text = agentctl.OpWorktreeLink, args[1]
		timeout = 3 * time.Minute // the user answers a card
	case "block", "unblock":
		if len(args) != 2 {
			return usage()
		}
		req.Op, req.Text = agentctl.OpWorktreeBlock, args[1]
		if args[0] == "unblock" {
			req.Op = agentctl.OpWorktreeUnblock
		}
	default:
		return usage()
	}
	rep, err := agentCall(agentSocket(getenv), req, timeout)
	switch {
	case err != nil:
		return fail("%v", err)
	case !rep.OK && rep.Error != "":
		return fail("%s", rep.Error)
	}
	if req.Op != agentctl.OpWorktreeList {
		return 0
	}
	if len(args) > 1 && args[1] == "--json" {
		_ = json.NewEncoder(out).Encode(rep.Worktrees)
		return 0
	}
	tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "BRANCH\tKIND\tSTATUS\tWAITS FOR\tLINK\tNOTE\tPATH")
	for _, w := range rep.Worktrees {
		b := dash(w.Branch)
		if w.Here {
			b += " *"
		}
		kind := w.Kind
		if w.Gone {
			kind += " (gone)"
		}
		waits := strings.Join(w.BlockedBy, ", ")
		if waits != "" && !w.Waits {
			waits += " (done)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", b, kind, dash(w.Checkpoint), dash(waits), dash(w.Link), dash(w.Note), w.Path)
	}
	_ = tw.Flush()
	return 0
}
