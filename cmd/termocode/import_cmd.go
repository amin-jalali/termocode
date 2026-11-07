package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/movein"
	"github.com/amin-jalali/termocode/internal/workflow"
)

// `termocode import <export>` (CMP-86): bring scheduled tasks, tasks,
// setup lines and agent notes over from another agent tool's export.

const importUsage = `Usage: termocode import [--dir d] [--agent codex|claude-code] [--yes] <export file or folder>

Bring your work over from another agent tool's export (JSON, JSON Lines,
Markdown notes, or a folder of them):
  scheduled tasks  → .termocode/workflows/<name>.jsonc (an agent step that
                     only reads; the schedule stays off until you turn it on)
  setup lines      → .termocode/worktrees.json "setup" (run after a trust card)
  agent notes      → AGENTS.md (new notes are added at the end)
  tasks            → .termocode/imported/tasks.md (a checklist)

It shows every file first and asks [y/N]; No is the default. It never
overwrites a file. --agent picks the agent of the workflows (default
codex); --dir the workspace (default: this folder); --yes skips the
question.
`

func runImport(args []string) int {
	cwd, _ := os.Getwd()
	return runImportIn(args, os.Stdin, os.Stdout, os.Stderr, cwd)
}

func runImportIn(args []string, in io.Reader, out, errw io.Writer, cwd string) int {
	dir, agent, yes, export := cwd, "codex", false, ""
	usage := func(msg string) int {
		if msg != "" {
			fmt.Fprintln(errw, "termocode import: "+msg)
			fmt.Fprintln(errw)
		}
		fmt.Fprint(errw, importUsage)
		return 2
	}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-h" || a == "--help" || a == "help":
			fmt.Fprint(out, importUsage)
			return 0
		case a == "--yes" || a == "-y":
			yes = true
		case a == "--dir" && i+1 < len(args):
			i++
			dir = args[i]
		case strings.HasPrefix(a, "--dir="):
			dir = strings.TrimPrefix(a, "--dir=")
		case a == "--agent" && i+1 < len(args):
			i++
			agent = args[i]
		case strings.HasPrefix(a, "--agent="):
			agent = strings.TrimPrefix(a, "--agent=")
		case strings.HasPrefix(a, "-"):
			return usage(fmt.Sprintf("unknown argument %q", a))
		case export == "":
			export = a
		default:
			return usage("only one export at a time")
		}
	}
	if export == "" {
		return usage("")
	}
	okAgent := false
	for _, a := range workflow.Agents {
		okAgent = okAgent || a == agent
	}
	if !okAgent {
		return usage(fmt.Sprintf("--agent must be one of %s", strings.Join(workflow.Agents, ", ")))
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		fmt.Fprintf(errw, "termocode import: %s is not a folder\n", dir)
		return 1
	}
	found, err := movein.Read(export)
	if err != nil {
		fmt.Fprintln(errw, "termocode import:", err)
		return 1
	}
	fmt.Fprintf(out, "Found %d scheduled task(s), %d task(s), %d setup line(s), %d note(s).\n",
		len(found.Scheduled), len(found.Tasks), len(found.Setup), len(found.Memory))
	if n := len(found.Skipped); n > 0 {
		fmt.Fprintf(out, "Not used (%d):\n", n)
		for i, s := range found.Skipped {
			if i == 10 {
				fmt.Fprintf(out, "  … %d more\n", n-10)
				break
			}
			fmt.Fprintln(out, "  "+s)
		}
	}
	if found.Empty() {
		fmt.Fprintln(out, "Nothing to bring over. Nothing was written.")
		return 1
	}
	plan, err := movein.Plan(found, dir, movein.Options{Agent: agent})
	if err != nil {
		fmt.Fprintln(errw, "termocode import:", err)
		return 1
	}
	writes := 0
	fmt.Fprintf(out, "\nIn %s:\n", dir)
	for _, w := range plan {
		fmt.Fprintf(out, "  %-6s  %s\n", w.Mode, w.Path)
		for _, l := range w.Preview {
			fmt.Fprintln(out, "            "+l)
		}
		if w.Why != "" {
			fmt.Fprintln(out, "            ("+w.Why+")")
		}
		if w.Mode != movein.ModeSkip {
			writes++
		}
	}
	if writes == 0 {
		fmt.Fprintln(out, "\nNothing to write. Nothing was written.")
		return 1
	}
	if !yes {
		fmt.Fprintf(out, "\nWrite these %d file(s)? [y/N] ", writes)
		line, _ := bufio.NewReader(in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Fprintln(out, "Nothing was written.")
			return 1
		}
	}
	done, err := movein.Apply(dir, plan)
	for _, p := range done {
		fmt.Fprintln(out, "wrote "+p)
	}
	if err != nil {
		fmt.Fprintln(errw, "termocode import:", err)
		return 1
	}
	fmt.Fprintln(out, "\nOpen Workflows to trust the new workflow files. Schedules stay off until you turn them on.")
	return 0
}
