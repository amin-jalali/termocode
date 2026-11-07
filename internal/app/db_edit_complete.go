package app

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Value completion in a cell (DBX-55): Tab in the edit prompt completes
// the typed start from the values already in that column of the loaded
// rows (true / false first for a bool column). Tab again goes to the next
// value that starts the same way. Only what is on the page is offered;
// nothing is asked of the server.

// dbCompleteMax caps the values offered for one column.
const dbCompleteMax = 500

// dbCellCompletions are the values Tab offers for column col of tab t.
func dbCellCompletions(t *dbTab, col int) []string {
	if t == nil || t.run == nil || col < 0 || col >= len(t.run.result.Columns) {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(v string) {
		if v == "" || seen[v] || len(v) > 200 || !utf8.ValidString(v) || strings.IndexFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	switch strings.ToLower(strings.TrimSpace(t.run.result.Columns[col].Type)) {
	case "bool", "boolean":
		add("true")
		add("false")
	}
	fixed := len(out)
	for _, row := range t.run.result.Rows {
		if len(out) >= dbCompleteMax {
			break
		}
		if col < len(row) && !row[col].Null && row[col].Kind != "binary" {
			add(row[col].Text)
		}
	}
	sort.Strings(out[fixed:])
	return out
}

// dbCycleCompleter completes from cands: one match takes it, several
// take their common start, and Tab on a value it gave goes to the next
// match of the same start.
func dbCycleCompleter(cands []string) func(string) string {
	var matches []string
	at, last := 0, ""
	return func(v string) string {
		if len(matches) > 1 && v == last {
			at = (at + 1) % len(matches)
			last = matches[at]
			return last
		}
		matches = matches[:0]
		low := strings.ToLower(v)
		for _, c := range cands {
			if strings.HasPrefix(strings.ToLower(c), low) {
				matches = append(matches, c)
			}
		}
		switch len(matches) {
		case 0:
			return v
		case 1:
			last = matches[0]
			return last
		}
		common := matches[0]
		for _, c := range matches[1:] {
			for !strings.HasPrefix(c, common) {
				_, size := utf8.DecodeLastRuneInString(common)
				common = common[:len(common)-size]
			}
		}
		if len(common) > len(v) && strings.HasPrefix(common, v) {
			last = ""
			return common
		}
		at = 0
		last = matches[0]
		return last
	}
}
