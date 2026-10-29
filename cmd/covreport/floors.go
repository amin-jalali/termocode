package main

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// ParseFloors reads a floors file (scripts/coverage-floors.txt): one
// "System name: percent" per line; "Total" is the whole. Blank lines and
// lines starting with # are skipped.
func ParseFloors(r io.Reader) (map[string]float64, error) {
	floors := map[string]float64{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.LastIndexByte(line, ':')
		if i <= 0 {
			return nil, fmt.Errorf("line %d: want \"System: percent\"", n)
		}
		name := strings.TrimSpace(line[:i])
		pct, err := strconv.ParseFloat(strings.TrimSpace(line[i+1:]), 64)
		if err != nil || pct < 0 || pct > 100 {
			return nil, fmt.Errorf("line %d: %q is not a percent", n, strings.TrimSpace(line[i+1:]))
		}
		if _, dup := floors[name]; dup {
			return nil, fmt.Errorf("line %d: %s has two floors", n, name)
		}
		floors[name] = pct
	}
	return floors, sc.Err()
}

// BelowFloors lists each system under its floor, each system without a
// floor and each floor whose system does not exist. A "Total" floor
// checks the whole.
func BelowFloors(r Report, floors map[string]float64) []string {
	var out []string
	known := map[string]bool{"Total": true}
	for _, s := range r.Systems {
		known[s.Name] = true
		floor, ok := floors[s.Name]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s has no floor", s.Name))
		case s.Percent() < floor:
			out = append(out, fmt.Sprintf("%s is %.1f%%, below its floor %.1f%%", s.Name, s.Percent(), floor))
		}
	}
	if floor, ok := floors["Total"]; ok && r.Total.Percent() < floor {
		out = append(out, fmt.Sprintf("Total is %.1f%%, below its floor %.1f%%", r.Total.Percent(), floor))
	}
	var unknown []string
	for name := range floors {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	for _, name := range unknown {
		out = append(out, fmt.Sprintf("floor for %q, which is not a system", name))
	}
	return out
}
