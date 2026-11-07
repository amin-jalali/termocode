package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	wfrun "github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/score"
	"github.com/amin-jalali/termocode/internal/workflow/source"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// termocode workflow … (WFX-8, plan §5.9). list, show, validate, runs,
// logs and run --dry-run work without a session (a dry run never
// writes); a real run, stop, pause and resume go through the IDE.

const workflowUsage = `usage: termocode workflow <command>
  list [--json]                     the workflows of this folder
  show <name> [--json]              one workflow's steps
  validate [<file>…]                check files (all of this folder by default)
  run <name> --dry-run [--var k=v]  read for real, write nothing (no plan steps)
  runs [<name>] [--json] [--limit N]
  logs <run id>                     what a run did
  scores [<name>] [--json] [--limit N]  score steps per run, and runs compared by their vars
  again <name> <key>                forget a seen item: the next run handles it again
  stop <run id>                     (in the IDE: Workflows, x)
Exit codes: 0 ok, 1 failed, 2 usage, 3 needs the IDE session.`

func runWorkflow(args []string) int {
	ws, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "termocode:", err)
		return 1
	}
	return runWorkflowIn(args, ws, os.Stdout, os.Stderr)
}

func workflowPrivateDir() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "termocode", "workflows")
}

func runWorkflowIn(args []string, ws string, out, errw io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errw, workflowUsage)
		return 2
	}
	root, err := ledger.Root(ws)
	if err != nil {
		fmt.Fprintln(errw, "termocode:", err)
		return 1
	}
	load := func() []wire.Entry { return wire.Load(ws, workflowPrivateDir(), root) }
	fs := flag.NewFlagSet("workflow "+args[0], flag.ContinueOnError)
	fs.SetOutput(errw)
	asJSON := fs.Bool("json", false, "print JSON")
	dry := fs.Bool("dry-run", false, "dry run")
	limit := fs.Int("limit", 20, "most runs")
	var vars kvFlags
	fs.Var(&vars, "var", "k=v (repeat)")
	cmd := args[0]
	if err := fs.Parse(reorderFlags(args[1:])); err != nil {
		return 2
	}
	rest := fs.Args()
	switch cmd {
	case "list":
		return wfList(load(), root, *asJSON, out)
	case "show":
		if len(rest) != 1 {
			fmt.Fprintln(errw, "usage: termocode workflow show <name>")
			return 2
		}
		e, ok := wire.Find(load(), rest[0])
		if !ok {
			fmt.Fprintf(errw, "termocode: no workflow %q\n", rest[0])
			return 1
		}
		return wfShow(e, *asJSON, out, errw)
	case "validate":
		return wfValidate(rest, load(), out, errw)
	case "run":
		if len(rest) != 1 {
			fmt.Fprintln(errw, "usage: termocode workflow run <name> --dry-run")
			return 2
		}
		if !*dry {
			fmt.Fprintln(errw, "termocode: a real run writes to outside systems and asks on cards: start it in the IDE (Workflows, r)")
			return 3
		}
		e, ok := wire.Find(load(), rest[0])
		if !ok {
			fmt.Fprintf(errw, "termocode: no workflow %q\n", rest[0])
			return 1
		}
		return wfDryRun(ws, root, e, map[string]any(vars), out, errw)
	case "runs":
		name := ""
		if len(rest) > 0 {
			name = rest[0]
		}
		return wfRuns(root, name, *limit, *asJSON, out, errw)
	case "scores":
		name := ""
		if len(rest) > 0 {
			name = rest[0]
		}
		return wfScores(root, name, *limit, *asJSON, out, errw)
	case "logs":
		if len(rest) != 1 {
			fmt.Fprintln(errw, "usage: termocode workflow logs <run id>")
			return 2
		}
		rf, err := wire.RunFiles{Root: root}.Load(rest[0])
		if err != nil {
			fmt.Fprintln(errw, "termocode:", err)
			return 1
		}
		wfPrintRun(rf, out)
		return 0
	case "again":
		if len(rest) != 2 {
			fmt.Fprintln(errw, "usage: termocode workflow again <name> <key>")
			return 2
		}
		return wfAgain(load(), root, rest[0], rest[1], out, errw)
	case "stop", "pause", "resume":
		fmt.Fprintf(errw, "termocode: %s works on a live run in the IDE (Workflows, x)\n", cmd)
		return 3
	}
	fmt.Fprintln(errw, workflowUsage)
	return 2
}

// reorderFlags puts flags before the other arguments (flag stops at the
// first non-flag).
func reorderFlags(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if (a == "--var" || a == "--limit" || a == "-var" || a == "-limit") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, a)
	}
	return append(flags, rest...)
}

type kvFlags map[string]any

func (k *kvFlags) String() string { return "" }
func (k *kvFlags) Set(v string) error {
	key, val, ok := strings.Cut(v, "=")
	if !ok || key == "" {
		return fmt.Errorf("want k=v, not %q", v)
	}
	if *k == nil {
		*k = kvFlags{}
	}
	(*k)[key] = val
	return nil
}

func wfState(e wire.Entry) string {
	switch {
	case e.Broken():
		return "file error"
	case !e.Trusted:
		return "not trusted"
	case e.Enabled:
		return "on"
	}
	return "off"
}

func wfList(list []wire.Entry, root string, asJSON bool, out io.Writer) int {
	type row struct {
		Name    string `json:"name"`
		Title   string `json:"title,omitempty"`
		State   string `json:"state"`
		Path    string `json:"path"`
		LastRun string `json:"last_run,omitempty"`
	}
	rows := []row{}
	for _, e := range list {
		r := row{Name: e.Name, State: wfState(e), Path: e.Path}
		if e.File != nil {
			r.Title = e.File.Title
		}
		if runs, _ := (wire.RunFiles{Root: root}).List(e.Name, 1); len(runs) > 0 {
			r.LastRun = runs[0].ID + " " + runs[0].State
		}
		rows = append(rows, r)
	}
	if asJSON {
		b, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Fprintln(out, string(b))
		return 0
	}
	for _, r := range rows {
		fmt.Fprintf(out, "%-24s %-12s %s\n", r.Name, r.State, r.Title)
	}
	return 0
}

func wfShow(e wire.Entry, asJSON bool, out, errw io.Writer) int {
	if e.Broken() {
		wfProblems(e, errw)
		return 1
	}
	type step struct {
		ID, Type, What string
		Depth          int
	}
	var steps []step
	var add func([]workflow.Step, int)
	add = func(list []workflow.Step, d int) {
		for i := range list {
			s := &list[i]
			steps = append(steps, step{s.ID, s.Type, wfWhat(s), d})
			for _, l := range s.Lists() {
				add(l, d+1)
			}
		}
	}
	add(e.File.Steps, 0)
	if asJSON {
		b, _ := json.MarshalIndent(map[string]any{"name": e.Name, "title": e.File.Title, "state": wfState(e), "hash": e.Hash, "steps": steps}, "", "  ")
		fmt.Fprintln(out, string(b))
		return 0
	}
	fmt.Fprintf(out, "%s · %s · %s\n", e.Name, e.File.Title, wfState(e))
	for _, s := range steps {
		fmt.Fprintf(out, "  %s%-10s %-8s %s\n", strings.Repeat("  ", s.Depth), s.ID, s.Type, s.What)
	}
	return 0
}

func wfWhat(s *workflow.Step) string {
	switch {
	case s.Source != nil:
		return s.Source.From + " " + strings.TrimSpace(s.Source.Server+" "+s.Source.Tool+" "+s.Source.Glob+strings.Join(s.Source.Argv, " ")+s.Source.Request+s.Source.Connection+s.Source.What)
	case s.Write != nil:
		switch s.Write.To {
		case "file":
			return "file " + s.Write.Path
		case "mcp", "status":
			return s.Write.Server + " " + s.Write.Tool
		}
		return s.Write.To
	case s.Each != nil:
		return s.Each.Items
	case s.Filter != nil:
		return s.When
	case s.Loop != nil:
		return fmt.Sprintf("max %d %s", s.Loop.Max, s.Loop.Until)
	case s.Branch != nil:
		names := make([]string, len(s.Branch.Branches))
		for i, b := range s.Branch.Branches {
			names[i] = b.Name
		}
		return "join " + s.Branch.Join + ": " + strings.Join(names, ", ")
	case s.Wait != nil:
		return s.Wait.For + " " + s.Wait.PR
	}
	return ""
}

func wfProblems(e wire.Entry, errw io.Writer) {
	for _, p := range e.Problems {
		fmt.Fprintf(errw, "%s: %s\n", e.Path, p.String())
	}
}

func wfValidate(files []string, list []wire.Entry, out, errw io.Writer) int {
	bad := false
	check := func(path string, data []byte) {
		if _, warns, err := workflow.Parse(path, data); err != nil {
			bad = true
			if pe, ok := err.(*workflow.ParseError); ok {
				for _, p := range pe.Problems {
					fmt.Fprintf(errw, "%s: %s\n", path, p.String())
				}
			} else {
				fmt.Fprintf(errw, "%s: %v\n", path, err)
			}
		} else {
			for _, w := range warns {
				fmt.Fprintf(errw, "%s: warning: %s\n", path, w.String())
			}
			fmt.Fprintf(out, "%s: ok\n", path)
		}
	}
	if len(files) == 0 {
		if len(list) == 0 {
			fmt.Fprintln(out, "no workflow files in "+wire.ProjectDir)
		}
		for _, e := range list {
			check(e.Path, e.Data)
		}
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(errw, "termocode:", err)
			bad = true
			continue
		}
		check(f, b)
	}
	if bad {
		return 1
	}
	return 0
}

func wfDryRun(ws, root string, e wire.Entry, vars map[string]any, out, errw io.Writer) int {
	if e.Broken() {
		wfProblems(e, errw)
		return 1
	}
	if !e.Trusted {
		fmt.Fprintln(errw, "termocode: "+e.Name+" is a repo file and is not trusted yet: open it in the IDE (Workflows, d) to read and trust it")
		return 1
	}
	h, err := wire.Start(context.Background(), wire.Config{Workspace: ws, Root: root, Entry: e, Dry: true, NoPlan: true, Vars: vars,
		Src: &source.Sources{Workspace: ws}})
	if err != nil {
		fmt.Fprintln(errw, "termocode:", err)
		return 1
	}
	select {
	case <-h.Done():
	case <-time.After(30 * time.Minute):
		h.Cancel()
		<-h.Done()
	}
	rf := h.Snapshot()
	wfPrintRun(rf, out)
	if rf.State != wfrun.RunOK {
		return 1
	}
	return 0
}

func wfPrintRun(rf *wfrun.RunFile, out io.Writer) {
	kind := "run"
	if rf.Dry {
		kind = "dry run"
	}
	fmt.Fprintf(out, "%s · %s %s · %s\n", rf.Workflow, kind, rf.ID, rf.State)
	if rf.Error != "" {
		fmt.Fprintln(out, "  error: "+rf.Error)
	}
	keys := make([]string, 0, len(rf.Steps))
	for k := range rf.Steps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s := rf.Steps[k]
		line := fmt.Sprintf("  %-10s %s", k, s.State)
		if len(s.Items) > 0 {
			line += " · " + strconv.Itoa(len(s.Items)) + " items"
		}
		if s.Score != nil {
			line += " · " + wfScoreText(*s.Score)
		}
		if s.Error != "" {
			line += " · " + s.Error
		}
		fmt.Fprintln(out, line)
	}
	if all := rf.Scores(); len(all) > 0 {
		sum := score.Summarize(all)
		fmt.Fprintf(out, "  scores     mean %.2f · %d of %d passed\n", sum.Mean, sum.Passed, sum.Count)
	}
	for _, w := range rf.Writes {
		verb := w.State
		if w.State == wfrun.StateRecorded {
			verb = "would write"
		}
		text := strings.TrimSpace(w.Spec.Content + w.Spec.Body + w.Spec.Text)
		fmt.Fprintf(out, "  %-10s %s %s %s\n", w.Step, verb, w.To, strconv.Quote(text))
	}
}

func wfRuns(root, name string, limit int, asJSON bool, out, errw io.Writer) int {
	runs, err := wire.RunFiles{Root: root}.List(name, limit)
	if err != nil {
		fmt.Fprintln(errw, "termocode:", err)
		return 1
	}
	if asJSON {
		b, _ := json.MarshalIndent(runs, "", "  ")
		fmt.Fprintln(out, string(b))
		return 0
	}
	for _, rf := range runs {
		kind := ""
		if rf.Dry {
			kind = "dry"
		}
		fmt.Fprintf(out, "%s  %-20s %-11s %s\n", rf.ID, rf.Workflow, rf.State, kind)
	}
	return 0
}

// wfScoreText is one score result in short: "score 0.83 ✓ (pass 0.70)".
func wfScoreText(r score.Result) string {
	mark := "✗"
	if r.Passed {
		mark = "✓"
	}
	return fmt.Sprintf("score %.2f %s (pass %.2f)", r.Score, mark, r.Pass)
}

// wfScoreRow is one run in the scores table (CMP-23).
type wfScoreRow struct {
	ID       string         `json:"id"`
	Workflow string         `json:"workflow"`
	State    string         `json:"state"`
	Dry      bool           `json:"dry,omitempty"`
	Variant  string         `json:"variant"`
	Summary  score.Summary  `json:"summary"`
	Results  []score.Result `json:"results"`
}

// wfScores lists the score steps of the last runs, then compares the
// runs by their vars (the model, prompt or profile a run was given), the
// best first. Runs without a score step are left out.
func wfScores(root, name string, limit int, asJSON bool, out, errw io.Writer) int {
	runs, err := wire.RunFiles{Root: root}.List(name, limit)
	if err != nil {
		fmt.Fprintln(errw, "termocode:", err)
		return 1
	}
	var rows []wfScoreRow
	for _, rf := range runs {
		res := rf.Scores()
		if len(res) == 0 {
			continue
		}
		rows = append(rows, wfScoreRow{ID: rf.ID, Workflow: rf.Workflow, State: rf.State, Dry: rf.Dry,
			Variant: score.Variant(rf.Vars), Summary: score.Summarize(res), Results: res})
	}
	variants := make([]string, len(rows))
	sums := make([]score.Summary, len(rows))
	for i, r := range rows {
		variants[i], sums[i] = r.Workflow+" "+r.Variant, r.Summary
	}
	groups := score.Compare(variants, sums)
	if asJSON {
		if rows == nil {
			rows = []wfScoreRow{}
		}
		if groups == nil {
			groups = []score.Group{}
		}
		b, _ := json.MarshalIndent(map[string]any{"runs": rows, "compare": groups}, "", "  ")
		fmt.Fprintln(out, string(b))
		return 0
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, "no scored runs: add a score step to a workflow (see docs/guides/workflows.md, Score agent output)")
		return 0
	}
	for _, r := range rows {
		kind := ""
		if r.Dry {
			kind = " dry"
		}
		fmt.Fprintf(out, "%s  %-20s mean %.2f  %d/%d passed  %s%s\n", r.ID, r.Workflow, r.Summary.Mean, r.Summary.Passed, r.Summary.Count,
			wfFirstOr(r.Variant, "(no vars)"), kind)
	}
	if len(groups) > 1 {
		fmt.Fprintln(out, "compare (best first):")
		for _, g := range groups {
			fmt.Fprintf(out, "  mean %.2f  %d runs  %d/%d passed  %s\n", g.Mean, g.Runs, g.Passed, g.Count, strings.TrimSpace(g.Variant))
		}
	}
	return 0
}

func wfFirstOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// wfAgain forgets key in the seen ledger of workflow name.
func wfAgain(list []wire.Entry, root, name, key string, out, errw io.Writer) int {
	if _, ok := wire.Find(list, name); !ok {
		fmt.Fprintf(errw, "termocode: no workflow %q\n", name)
		return 1
	}
	seen, err := ledger.OpenSeen(root, name)
	if err != nil {
		fmt.Fprintln(errw, "termocode:", err)
		return 1
	}
	if !seen.Has(key) {
		fmt.Fprintf(errw, "termocode: %s has not seen %q\n", name, key)
		return 1
	}
	if err := seen.Forget(key); err != nil {
		fmt.Fprintln(errw, "termocode:", err)
		return 1
	}
	fmt.Fprintf(out, "%s: %s is new again; the next run handles it\n", name, key)
	return 0
}
