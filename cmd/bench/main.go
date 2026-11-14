// Command bench measures termocode against other editors. Dev tool only.
//
//	bench fixtures <dir>   generate the repos and big files
//	bench pins             print the pinned language servers
//	bench run [flags]      run every installed editor, write results.json and report.md
//	bench report <results.json>   print the report for saved raw data
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/bench"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: bench fixtures <dir> | run | report <results.json> | pins")
	}
	switch args[0] {
	case "fixtures":
		if len(args) != 2 {
			return fmt.Errorf("usage: bench fixtures <dir>")
		}
		return makeFixtures(args[1])
	case "run":
		return runCmd(args[1:])
	case "report":
		if len(args) != 2 {
			return fmt.Errorf("usage: bench report <results.json>")
		}
		d, err := bench.Load(args[1])
		if err != nil {
			return err
		}
		fmt.Print(bench.Markdown(d))
		return nil
	case "pins":
		for _, p := range bench.LSPPins {
			fmt.Printf("%s %s\t%s\n", p.Name, p.Version, p.Install)
		}
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func makeFixtures(dir string) error {
	for name, spec := range bench.FixtureRepos {
		if err := bench.GenRepo(filepath.Join(dir, name), spec); err != nil {
			return err
		}
	}
	if err := bench.BigLines(filepath.Join(dir, "big-50k-lines.txt"), 50000); err != nil {
		return err
	}
	return bench.BigBytes(filepath.Join(dir, "big-50mb.log"), 50<<20)
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fix := fs.String("fixtures", "", "directory made by `bench fixtures` (required)")
	repo := fs.String("repo", "small", "repo inside the fixtures dir: small or large")
	out := fs.String("out", ".", "where results.json and report.md go")
	runs := fs.Int("runs", 10, "measured runs per measure")
	warm := fs.Int("warmup", 1, "warm-up runs")
	idle := fs.Duration("idle", 60*time.Second, "idle window (0 = skip)")
	only := fs.String("editors", "", "comma-separated editor ids (default: all)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fix == "" {
		return fmt.Errorf("run needs -fixtures <dir>")
	}
	var eds []bench.Editor
	for _, e := range bench.Editors() {
		if *only == "" || contains(*only, e.ID) {
			eds = append(eds, e)
		}
	}
	d := bench.RunAll(bench.RunOpts{
		Editors: eds,
		Env:     bench.Env{Repo: filepath.Join(*fix, *repo), BigFile: filepath.Join(*fix, "big-50k-lines.txt")},
		Runs:    *runs, Warmup: *warm, IdleFor: *idle,
		Machine: bench.MachineInfo(), Date: time.Now().Format("2006-01-02"),
	}, func(s string) { fmt.Fprintln(os.Stderr, s) })
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	if err := d.Save(filepath.Join(*out, "results.json")); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(*out, "report.md"), []byte(bench.Markdown(d)), 0o644)
}

func contains(list, id string) bool {
	for _, p := range strings.Split(list, ",") {
		if strings.TrimSpace(p) == id {
			return true
		}
	}
	return false
}
