// Command bench measures termocode against other editors. Dev tool only.
//
//	bench fixtures <dir>   generate the repos and big files
//	bench pins             print the pinned language servers
package main

import (
	"fmt"
	"os"
	"path/filepath"

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
		return fmt.Errorf("usage: bench fixtures <dir> | pins")
	}
	switch args[0] {
	case "fixtures":
		if len(args) != 2 {
			return fmt.Errorf("usage: bench fixtures <dir>")
		}
		return makeFixtures(args[1])
	case "pins":
		for _, p := range bench.LSPPins {
			fmt.Printf("%s %s\t%s\n", p.Name, p.Version, p.Install)
		}
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func makeFixtures(dir string) error {
	for name, spec := range map[string]bench.RepoSpec{
		"small": {Files: 200, Seed: 1, Git: true},
		"large": {Files: 20000, Seed: 2, Git: true},
	} {
		if err := bench.GenRepo(filepath.Join(dir, name), spec); err != nil {
			return err
		}
	}
	if err := bench.BigLines(filepath.Join(dir, "big-50k-lines.txt"), 50000); err != nil {
		return err
	}
	return bench.BigBytes(filepath.Join(dir, "big-50mb.log"), 50<<20)
}
