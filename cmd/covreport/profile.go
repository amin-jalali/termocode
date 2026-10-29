package main

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Block is one line of a Go cover profile: a range of source with the
// number of statements in it and how often it ran.
type Block struct {
	File       string // import path of the file
	Pos        string // "line.col,line.col"
	Statements int
	Count      int
}

func (b Block) key() string { return b.File + ":" + b.Pos }

// ParseProfile reads a cover profile (the "mode:" line is skipped).
func ParseProfile(r io.Reader) ([]Block, error) {
	var out []Block
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		head, counts, ok := strings.Cut(line, " ")
		i := strings.LastIndex(head, ":")
		f := strings.Fields(counts)
		if !ok || i < 0 || len(f) != 2 {
			return nil, fmt.Errorf("profile line %d: not a block: %q", n, line)
		}
		st, err1 := strconv.Atoi(f[0])
		cnt, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("profile line %d: bad numbers: %q", n, line)
		}
		out = append(out, Block{File: head[:i], Pos: head[i+1:], Statements: st, Count: cnt})
	}
	return out, sc.Err()
}

// Merge unions profiles: a block that appears in several is kept once with
// the sum of its counts, so it is covered when any run hit it.
func Merge(profiles ...[]Block) []Block {
	idx := map[string]int{}
	var out []Block
	for _, p := range profiles {
		for _, b := range p {
			if i, ok := idx[b.key()]; ok {
				out[i].Count += b.Count
				continue
			}
			idx[b.key()] = len(out)
			out = append(out, b)
		}
	}
	return out
}

// WriteProfile writes blocks as a "count" mode profile, sorted like go does.
func WriteProfile(w io.Writer, blocks []Block) error {
	sorted := append([]Block(nil), blocks...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].File != sorted[j].File {
			return sorted[i].File < sorted[j].File
		}
		return sorted[i].Pos < sorted[j].Pos
	})
	if _, err := fmt.Fprintln(w, "mode: count"); err != nil {
		return err
	}
	for _, b := range sorted {
		if _, err := fmt.Fprintf(w, "%s:%s %d %d\n", b.File, b.Pos, b.Statements, b.Count); err != nil {
			return err
		}
	}
	return nil
}

// Stat is a statement tally.
type Stat struct {
	Name           string
	Total, Covered int
}

// Percent is covered/total in percent; a tally without statements is 0.
func (s Stat) Percent() float64 {
	if s.Total == 0 {
		return 0
	}
	return 100 * float64(s.Covered) / float64(s.Total)
}

// Report is the coverage of every system, weighted by statements.
type Report struct {
	Systems  []Stat   // in mapping-file order
	Total    Stat     // all measured systems together
	Unmapped []string // packages in the profile that no system owns
}

// System returns the tally of one system (zero Stat when unknown).
func (r Report) System(name string) Stat {
	for _, s := range r.Systems {
		if s.Name == name {
			return s
		}
	}
	return Stat{Name: name}
}

// pkgOf turns a profile file name into a module-relative package path.
func pkgOf(file, module string) string {
	return path.Dir(strings.TrimPrefix(file, module+"/"))
}

// Summarize adds up statements per system. Excluded packages are ignored;
// packages without a system are listed in Unmapped and not counted.
func Summarize(blocks []Block, module string, sys *Systems) Report {
	tally := map[string]*Stat{}
	r := Report{Total: Stat{Name: "Total"}}
	for _, n := range sys.Order {
		tally[n] = &Stat{Name: n}
	}
	unmapped := map[string]bool{}
	for _, b := range blocks {
		pkg := pkgOf(b.File, module)
		name, excluded := sys.Classify(pkg)
		if excluded {
			continue
		}
		if name == "" {
			unmapped[pkg] = true
			continue
		}
		t := tally[name]
		t.Total += b.Statements
		r.Total.Total += b.Statements
		if b.Count > 0 {
			t.Covered += b.Statements
			r.Total.Covered += b.Statements
		}
	}
	for _, n := range sys.Order {
		r.Systems = append(r.Systems, *tally[n])
	}
	for p := range unmapped {
		r.Unmapped = append(r.Unmapped, p)
	}
	sort.Strings(r.Unmapped)
	return r
}
