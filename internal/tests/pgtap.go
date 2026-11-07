package tests

import (
	"strconv"
	"strings"
)

// PgTAPPackage is the Event.Package of every pgTAP result, so the test
// view groups database tests under one node.
const PgTAPPackage = "pgTAP"

// PgTAPRunSQL returns the statement that runs pgTAP xUnit-style test
// functions: runtests(), optionally limited to a schema and/or a name
// pattern (a regular expression). Values are quoted as SQL literals.
func PgTAPRunSQL(schema, pattern string) string {
	var args []string
	if schema != "" {
		args = append(args, sqlLiteral(schema)+"::name")
	}
	if pattern != "" {
		args = append(args, sqlLiteral(pattern)+"::text")
	}
	return "SELECT * FROM runtests(" + strings.Join(args, ", ") + ");"
}

func sqlLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// PgTAPParser parses pgTAP's TAP output, either from runtests() (one
// "# Subtest: fn()" block per test function, closed by a top-level
// ok/not ok line) or a plain TAP script (one result per assertion).
// Psql decoration (column header, dashes, "(N rows)") is output only.
type PgTAPParser struct {
	sub     string   // open subtest function name ("" = none)
	subOut  []string // lines of the open subtest
	subFail []string // failing assertion + diagnostics inside the subtest
	inFail  bool     // the last nested assertion failed (collect its "#" lines)
	last    *Event   // last top-level plain result, waiting for diagnostics
}

// NewPgTAPParser returns an empty parser.
func NewPgTAPParser() *PgTAPParser { return &PgTAPParser{} }

// tapLine is one parsed "ok"/"not ok" line.
type tapLine struct {
	ok     bool
	num    int
	desc   string
	skip   bool // # SKIP or # TODO directive
	parsed bool
}

func parseTAPResult(s string) tapLine {
	var t tapLine
	switch {
	case strings.HasPrefix(s, "not ok"):
		s = s[len("not ok"):]
	case strings.HasPrefix(s, "ok"):
		t.ok = true
		s = s[len("ok"):]
	default:
		return t
	}
	if s != "" && s[0] != ' ' {
		return t // "okay", "nothing" …
	}
	t.parsed = true
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i > 0 || (s != "" && isDigits(s)) {
		head := s
		if i > 0 {
			head = s[:i]
		}
		if n, err := strconv.Atoi(head); err == nil {
			t.num = n
			s = strings.TrimSpace(s[len(head):])
		}
	}
	s = strings.TrimPrefix(s, "- ")
	if s == "-" {
		s = ""
	}
	if i := strings.Index(s, "#"); i >= 0 {
		dir := strings.ToUpper(strings.TrimSpace(s[i+1:]))
		if strings.HasPrefix(dir, "SKIP") || strings.HasPrefix(dir, "TODO") {
			t.skip = true
			s = strings.TrimSpace(s[:i])
		}
	}
	t.desc = strings.TrimSpace(s)
	return t
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func (t tapLine) status() Status {
	switch {
	case t.skip:
		return StatusSkipped
	case t.ok:
		return StatusPassed
	}
	return StatusFailed
}

func (t tapLine) name() string {
	if t.desc != "" {
		return t.desc
	}
	return "test " + strconv.Itoa(t.num)
}

// Line parses one output line.
func (p *PgTAPParser) Line(line string) []Event {
	raw := strings.TrimRight(line, "\r")
	evs := []Event{}
	if strings.TrimSpace(raw) != "" {
		evs = append(evs, Event{Kind: EventOutput, Line: raw})
	}
	nested := strings.HasPrefix(raw, "    ") || strings.HasPrefix(raw, "\t")
	s := strings.TrimSpace(raw)
	if s == "" {
		return evs
	}

	if name, ok := strings.CutPrefix(s, "# Subtest:"); ok && !nested {
		evs = append(evs, p.closeSub(StatusFailed, true)...)
		evs = append(evs, p.flushLast()...)
		p.sub = strings.TrimSuffix(strings.TrimSpace(name), "()")
		return evs
	}

	if p.sub != "" && nested {
		p.subOut = append(p.subOut, s)
		if t := parseTAPResult(s); t.parsed {
			p.inFail = t.status() == StatusFailed
			if p.inFail {
				p.subFail = append(p.subFail, s)
			}
		} else if strings.HasPrefix(s, "1..") {
			p.inFail = false
		} else if strings.HasPrefix(s, "#") && p.inFail {
			p.subFail = append(p.subFail, s)
		}
		return evs
	}

	t := parseTAPResult(s)
	if t.parsed {
		if p.sub != "" {
			return append(evs, p.closeSub(t.status(), false)...)
		}
		evs = append(evs, p.flushLast()...)
		p.last = &Event{Kind: EventResult, Package: PgTAPPackage, Test: t.name(), Status: t.status()}
		if p.last.Status == StatusFailed {
			p.last.Message = s
		}
		return evs
	}
	if strings.HasPrefix(s, "#") && p.last != nil && p.last.Status == StatusFailed {
		p.last.Message += "\n" + s
	}
	return evs
}

// closeSub ends the open subtest with st. cut marks a subtest that never
// got its closing line (crash, cancel).
func (p *PgTAPParser) closeSub(st Status, cut bool) []Event {
	if p.sub == "" {
		return nil
	}
	e := Event{Kind: EventResult, Package: PgTAPPackage, Test: p.sub, Status: st,
		Message: strings.Join(p.subFail, "\n"), Output: p.subOut}
	if cut && e.Message == "" {
		e.Message = "test function ended without a result"
	}
	p.sub, p.subOut, p.subFail, p.inFail = "", nil, nil, false
	return []Event{e}
}

func (p *PgTAPParser) flushLast() []Event {
	if p.last == nil {
		return nil
	}
	e := *p.last
	p.last = nil
	return []Event{e}
}

// Flush returns the pending result; an unfinished subtest counts as failed.
func (p *PgTAPParser) Flush() []Event {
	evs := p.closeSub(StatusFailed, true)
	return append(evs, p.flushLast()...)
}
