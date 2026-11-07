package dbclient

import (
	"errors"
	"sort"
	"strings"
)

// Console execution modes (DBX-25): what Run sends from a SQL buffer.

// ExecMode is one way to pick the statements to run.
type ExecMode int

// Execution modes.
const (
	ExecSmallest       ExecMode = iota // the innermost statement under the caret (a subquery too)
	ExecLargest                        // the whole statement under the caret
	ExecFromCaret                      // the statement under the caret and every one after it
	ExecSelectionOne                   // the selection, sent as one text
	ExecSelectionSplit                 // the selection, split into statements
	ExecAsk                            // show ExecCandidates and let the person choose
)

var execModeNames = []string{"smallest", "largest", "from-caret", "selection-one", "selection-split", "ask"}

func (m ExecMode) String() string {
	if m >= 0 && int(m) < len(execModeNames) {
		return execModeNames[m]
	}
	return "unknown"
}

// ParseExecMode reads a mode name (as String writes it).
func ParseExecMode(s string) (ExecMode, bool) {
	for i, n := range execModeNames {
		if n == s {
			return ExecMode(i), true
		}
	}
	return 0, false
}

// StmtSpan is one statement and its byte range in the text
// (text[Start:End] == SQL).
type StmtSpan struct {
	SQL        string
	Start, End int
}

// StatementSpans splits text into statements with their byte ranges.
// A span starts at the statement's first code byte, after any leading
// comments.
func StatementSpans(text, driver string) []StmtSpan {
	sqls, err := Split(text, driver)
	if err != nil {
		return nil
	}
	out := make([]StmtSpan, 0, len(sqls))
	off := 0
	for _, s := range sqls {
		at := strings.Index(text[off:], s)
		if at < 0 {
			continue
		}
		at += off
		off = at + len(s)
		// Comments before the statement are not part of what runs.
		if k := skipLeadingComments(s); k < len(s) {
			at += k
			s = s[k:]
		}
		out = append(out, StmtSpan{SQL: s, Start: at, End: off})
	}
	return out
}

// spanAt is the index of the statement the caret belongs to: each
// statement owns the text up to the next one.
func spanAt(spans []StmtSpan, cursor int) int {
	if len(spans) == 0 {
		return -1
	}
	i := 0
	for k := range spans {
		if spans[k].Start <= cursor {
			i = k
		}
	}
	return i
}

// ExecCandidates lists what can run at the caret, innermost first: each
// parenthesised subquery around it (SELECT, WITH, VALUES), then the whole
// statement.
func ExecCandidates(text, driver string, cursor int) []StmtSpan {
	spans := StatementSpans(text, driver)
	i := spanAt(spans, cursor)
	if i < 0 {
		return nil
	}
	st := spans[i]
	rel := cursor - st.Start
	var out []StmtSpan
	for _, p := range parenPairs(st.SQL) {
		if rel <= p[0] || rel > p[1] {
			continue
		}
		inner := st.SQL[p[0]+1 : p[1]]
		trim := strings.TrimSpace(inner)
		if !startsQuery(trim) {
			continue
		}
		lead := strings.Index(inner, trim)
		start := st.Start + p[0] + 1 + lead
		out = append(out, StmtSpan{SQL: trim, Start: start, End: start + len(trim)})
	}
	sort.SliceStable(out, func(a, b int) bool { return len(out[a].SQL) < len(out[b].SQL) })
	return append(out, st)
}

func startsQuery(s string) bool {
	w := s
	if k := strings.IndexAny(w, " \t\r\n("); k >= 0 {
		w = w[:k]
	}
	switch strings.ToUpper(w) {
	case "SELECT", "WITH", "VALUES":
		return true
	}
	return false
}

// parenPairs returns the byte offsets of matching ( ) in sql, skipping
// quotes and comments.
func parenPairs(sql string) [][2]int {
	var out [][2]int
	var stack []int
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			for i++; i < len(sql); i++ {
				if sql[i] == c {
					if i+1 < len(sql) && sql[i+1] == c {
						i++
						continue
					}
					break
				}
			}
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				i = len(sql)
			} else {
				i += end + 3
			}
		case c == '(':
			stack = append(stack, i)
		case c == ')':
			if len(stack) > 0 {
				out = append(out, [2]int{stack[len(stack)-1], i})
				stack = stack[:len(stack)-1]
			}
		}
	}
	return out
}

// PickStatements returns the statements mode runs. cursor is the caret
// byte offset; selStart/selEnd the selection (used by the selection
// modes). ExecAsk has no single answer: the caller shows ExecCandidates.
func PickStatements(text, driver string, mode ExecMode, cursor, selStart, selEnd int) ([]string, error) {
	switch mode {
	case ExecSmallest, ExecLargest:
		c := ExecCandidates(text, driver, cursor)
		if len(c) == 0 {
			return nil, errors.New("no statement at the caret")
		}
		if mode == ExecSmallest {
			return []string{c[0].SQL}, nil
		}
		return []string{c[len(c)-1].SQL}, nil
	case ExecFromCaret:
		spans := StatementSpans(text, driver)
		i := spanAt(spans, cursor)
		if i < 0 {
			return nil, errors.New("no statement at the caret")
		}
		out := make([]string, 0, len(spans)-i)
		for _, s := range spans[i:] {
			out = append(out, s.SQL)
		}
		return out, nil
	case ExecSelectionOne, ExecSelectionSplit:
		if selStart < 0 || selEnd > len(text) || selStart > selEnd {
			return nil, errors.New("the selection is outside the text")
		}
		sel := text[selStart:selEnd]
		if mode == ExecSelectionOne {
			if t := strings.TrimSpace(sel); t != "" {
				return []string{t}, nil
			}
			return nil, errors.New("nothing selected")
		}
		sqls, err := Split(sel, driver)
		if err != nil {
			return nil, err
		}
		if len(sqls) == 0 {
			return nil, errors.New("nothing selected")
		}
		return sqls, nil
	case ExecAsk:
		return nil, errors.New("ask mode: choose from the candidates")
	}
	return nil, errors.New("unknown execution mode")
}
