package main

import (
	"fmt"
	"io"
	"strings"
)

// PrintTable writes the per-system table and the total.
func PrintTable(w io.Writer, r Report) {
	width := len("System")
	for _, s := range r.Systems {
		width = max(width, len(s.Name))
	}
	row := func(name, total, covered, pct string) {
		fmt.Fprintf(w, "%-*s  %10s  %10s  %8s\n", width, name, total, covered, pct)
	}
	row("System", "Statements", "Covered", "Percent")
	fmt.Fprintln(w, strings.Repeat("-", width+34))
	for _, s := range r.Systems {
		row(s.Name, fmt.Sprint(s.Total), fmt.Sprint(s.Covered), fmt.Sprintf("%.1f%%", s.Percent()))
	}
	fmt.Fprintln(w, strings.Repeat("-", width+34))
	row(r.Total.Name, fmt.Sprint(r.Total.Total), fmt.Sprint(r.Total.Covered), fmt.Sprintf("%.1f%%", r.Total.Percent()))
}

// PrintTopFuncs writes, per system, the functions with the most gaps.
func PrintTopFuncs(w io.Writer, r Report, funcs map[string][]FuncStat) {
	for _, s := range r.Systems {
		fmt.Fprintf(w, "\n%s: biggest uncovered functions\n", s.Name)
		list := funcs[s.Name]
		if len(list) == 0 {
			fmt.Fprintln(w, "  (nothing uncovered)")
			continue
		}
		for _, f := range list {
			fmt.Fprintf(w, "  %4d of %-4d  %s:%d  %s\n", f.Missed, f.Total, f.File, f.Line, f.Name)
		}
	}
}

// Below lists what is under the limits: perSystem applies to every system,
// total to the whole. A limit of 0 is off.
func Below(r Report, perSystem, total float64) []string {
	var out []string
	if perSystem > 0 {
		for _, s := range r.Systems {
			if s.Percent() < perSystem {
				out = append(out, fmt.Sprintf("%s is %.1f%%, below %.1f%%", s.Name, s.Percent(), perSystem))
			}
		}
	}
	if total > 0 && r.Total.Percent() < total {
		out = append(out, fmt.Sprintf("Total is %.1f%%, below %.1f%%", r.Total.Percent(), total))
	}
	return out
}
