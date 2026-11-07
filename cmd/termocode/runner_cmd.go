package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/runner"
)

// `termocode runner serve|add|list|show|cancel` (CMP-12): this machine
// serves agent tasks. `serve` is the loop (keep it in a session, a
// service or tmux); the others work on the queue folder, so a remote
// client adds a task with `ssh host termocode runner add …`.

const runnerUsage = `termocode runner: this machine serves agent tasks

  termocode runner serve [--folder DIR]… [--pool N]
        take queued tasks of these folders (default: the current folder)
        and run at most N at once (default 2, at most 8); Ctrl+C stops
  termocode runner add [--folder DIR] [--agent claude|codex] [--model M]
                       [--read-only] [--base BRANCH] <prompt…>   (prompt "-" reads stdin)
        queue a task and print its id; a task that may change files runs
        in its own worktree from BRANCH (default: HEAD), never in your checkout
  termocode runner list [--json]
        the tasks: id, state, agent, folder, prompt
  termocode runner show <id> [--json]
        one task: its answer, changed files, worktree and log file
  termocode runner cancel <id>
        drop a queued task, or stop a running one

The agents run on your own Claude Code or Codex login. The runner only
serves the folders it was started for; a task for another folder fails.
`

func runRunner(args []string) int {
	return runRunnerIn(args, os.Stdin, os.Stdout, os.Stderr, os.Getwd, runner.DefaultDir)
}

func runRunnerIn(args []string, in io.Reader, out, errw io.Writer, getwd func() (string, error), queueDir func() (string, error)) int {
	fail := func(f string, a ...any) int {
		fmt.Fprintf(errw, "termocode runner: "+f+"\n", a...)
		return 1
	}
	usage := func() int { fmt.Fprint(errw, runnerUsage); return 2 }
	if len(args) == 0 {
		return usage()
	}
	if a := args[0]; a == "-h" || a == "--help" || a == "help" {
		fmt.Fprint(out, runnerUsage)
		return 0
	}
	dir, err := queueDir()
	if err != nil {
		return fail("no queue folder: %v", err)
	}
	store := &runner.Store{Dir: dir}
	cwd := func() (string, error) {
		d, err := getwd()
		if err != nil {
			return "", fmt.Errorf("no current folder: %w", err)
		}
		return d, nil
	}
	// flagVal reads "--name value" or "--name=value" at args[*i].
	flagVal := func(rest []string, i *int, name string) (string, bool) {
		a := rest[*i]
		if a == name && *i+1 < len(rest) {
			*i++
			return rest[*i], true
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v, true
		}
		return "", false
	}
	rest := args[1:]
	switch args[0] {
	case "serve":
		var folders []string
		pool := runner.DefaultPool
		for i := 0; i < len(rest); i++ {
			if v, ok := flagVal(rest, &i, "--folder"); ok {
				abs, err := filepath.Abs(v)
				if err != nil {
					return fail("folder %q: %v", v, err)
				}
				folders = append(folders, abs)
				continue
			}
			if v, ok := flagVal(rest, &i, "--pool"); ok {
				n, err := strconv.Atoi(v)
				if err != nil || n < 1 || n > runner.MaxPool {
					fmt.Fprintf(errw, "termocode runner: --pool must be 1…%d\n", runner.MaxPool)
					return 2
				}
				pool = n
				continue
			}
			return usage()
		}
		if len(folders) == 0 {
			d, err := cwd()
			if err != nil {
				return fail("%v", err)
			}
			folders = []string{d}
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		srv := &runner.Server{Store: store, Pool: pool, Folders: folders,
			Log: func(l string) { fmt.Fprintf(out, "%s %s\n", time.Now().Format("15:04:05"), l) }}
		if err := srv.Serve(ctx); err != nil {
			return fail("%v", err)
		}
		return 0
	case "add":
		t := runner.Task{Agent: ai.CLIClaude}
		var words []string
		for i := 0; i < len(rest); i++ {
			if len(words) > 0 {
				words = append(words, rest[i])
				continue
			}
			if v, ok := flagVal(rest, &i, "--folder"); ok {
				t.Folder = v
				continue
			}
			if v, ok := flagVal(rest, &i, "--agent"); ok {
				t.Agent = v
				continue
			}
			if v, ok := flagVal(rest, &i, "--model"); ok {
				t.Model = v
				continue
			}
			if v, ok := flagVal(rest, &i, "--base"); ok {
				t.Base = v
				continue
			}
			switch a := rest[i]; {
			case a == "--read-only":
				t.ReadOnly = true
			case a == "--":
				words = append(words, rest[i+1:]...)
				i = len(rest)
			case strings.HasPrefix(a, "--"):
				return usage()
			default:
				words = append(words, a)
			}
		}
		if len(words) == 0 {
			return usage()
		}
		t.Prompt = strings.Join(words, " ")
		if t.Prompt == "-" {
			b, err := io.ReadAll(io.LimitReader(in, runner.MaxPrompt+1))
			if err != nil {
				return fail("read stdin: %v", err)
			}
			t.Prompt = string(b)
		}
		if t.Folder == "" || !filepath.IsAbs(t.Folder) {
			d, err := cwd()
			if err != nil {
				return fail("%v", err)
			}
			t.Folder = filepath.Join(d, t.Folder)
		}
		added, err := store.Add(t)
		if err != nil {
			return fail("%v", err)
		}
		fmt.Fprintln(out, added.ID)
		return 0
	case "list":
		asJSON := false
		for _, a := range rest {
			if a != "--json" {
				return usage()
			}
			asJSON = true
		}
		list, err := store.List()
		if err != nil {
			return fail("%v", err)
		}
		if asJSON {
			if list == nil {
				list = []runner.Task{}
			}
			_ = json.NewEncoder(out).Encode(list)
			return 0
		}
		tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tSTATE\tAGENT\tFOLDER\tPROMPT")
		for _, t := range list {
			state := t.State
			if t.CancelAsked && !runner.Final(t.State) {
				state += " (stopping)"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", t.ID, state, t.Agent, t.Folder, runnerShort(t.Prompt, 50))
		}
		_ = tw.Flush()
		return 0
	case "show":
		if len(rest) < 1 || len(rest) > 2 || (len(rest) == 2 && rest[1] != "--json") {
			return usage()
		}
		t, err := store.Get(rest[0])
		if err != nil {
			return fail("%v", err)
		}
		if len(rest) == 2 {
			_ = json.NewEncoder(out).Encode(t)
			return 0
		}
		mode := "may change files (own worktree)"
		if t.ReadOnly {
			mode = "read-only"
		}
		fmt.Fprintf(out, "Task     %s\nState    %s\nAgent    %s %s\nMode     %s\nFolder   %s\n", t.ID, t.State, t.Agent, t.Model, mode, t.Folder)
		if t.Worktree != "" {
			fmt.Fprintf(out, "Worktree %s (%s)\n", t.Worktree, t.Branch)
		}
		fmt.Fprintf(out, "Log      %s\n", store.LogPath(t.ID))
		if t.Error != "" {
			fmt.Fprintf(out, "Error    %s\n", t.Error)
		}
		if len(t.Files) > 0 {
			fmt.Fprintf(out, "Changed  %s\n", strings.Join(t.Files, ", "))
		}
		fmt.Fprintf(out, "\nPrompt:\n%s\n", t.Prompt)
		if t.Answer != "" {
			fmt.Fprintf(out, "\nAnswer:\n%s\n", t.Answer)
		}
		return 0
	case "cancel":
		if len(rest) != 1 {
			return usage()
		}
		t, err := store.Cancel(rest[0])
		if err != nil {
			return fail("%v", err)
		}
		if t.State == runner.StateCanceled {
			fmt.Fprintf(out, "%s canceled\n", t.ID)
		} else {
			fmt.Fprintf(out, "%s: asked the runner to stop it\n", t.ID)
		}
		return 0
	}
	return usage()
}

// runnerShort is the first line of s, cut to n runes.
func runnerShort(s string, n int) string {
	s, _, _ = strings.Cut(s, "\n")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
