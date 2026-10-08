package tests

import (
	"encoding/json"
	"encoding/xml"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// EventKind classifies a parsed runner event.
type EventKind int

const (
	// EventOutput is one raw output line for the run log.
	EventOutput EventKind = iota
	// EventResult is a test result (pass / fail / skip), possibly with a
	// failure message. Several results may arrive for one test; fold them
	// with MergeStatus.
	EventResult
	// EventPackage is a package/file-level outcome (build failed,
	// collection error). Status is StatusFailed or StatusPassed.
	EventPackage
)

// Event is a runner-neutral result event. Which identity fields are set
// depends on the framework (see Tree.Apply):
//
//	Go:     Package = import path, Test = top-level test name
//	pytest: File = rel path, Test = "Cls::test" — or (junit) Class = dotted classname
//	cargo:  Package = source of the test binary (src/lib.rs, tests/x.rs), Test = full path
//	jest:   File = absolute path, Test = titles joined by JSNameSep
type Event struct {
	Kind     EventKind
	Package  string
	File     string
	Class    string
	Test     string
	Status   Status
	Duration time.Duration
	Message  string
	FailFile string
	FailLine int
	Output   []string
	Line     string // EventOutput only
}

// LineParser turns a runner's output stream into events, one line at a
// time. Implementations keep state between lines.
type LineParser interface {
	Line(line string) []Event
	// Flush returns events still buffered at end of stream.
	Flush() []Event
}

// NewLineParser returns the streaming parser for fw.
func NewLineParser(fw Framework) LineParser {
	switch fw {
	case FrameworkGo:
		return NewGoParser()
	case FrameworkPytest:
		return &PytestParser{}
	case FrameworkCargo:
		return &CargoParser{}
	}
	return rawParser{}
}

// rawParser passes lines through as output (jest/vitest stream human text;
// results come from the JSON report file at the end).
type rawParser struct{}

func (rawParser) Line(line string) []Event {
	return []Event{{Kind: EventOutput, Line: line}}
}
func (rawParser) Flush() []Event { return nil }

// ── go test -json ────────────────────────────────────────────────────────

// goEvent is one line of `go test -json` (test2json).
type goEvent struct {
	Action      string
	Package     string
	Test        string
	Elapsed     float64
	Output      string
	ImportPath  string // build-output events (Go 1.24+)
	FailedBuild string
}

// GoParser parses `go test -json`. It buffers each top-level test's output
// (subtests included) so a failure carries its message and location.
type GoParser struct {
	testOut map[string][]string // pkg + "\x00" + top-level test
	pkgOut  map[string][]string // package-level + build output
}

// NewGoParser returns an empty GoParser.
func NewGoParser() *GoParser {
	return &GoParser{testOut: map[string][]string{}, pkgOut: map[string][]string{}}
}

// goTopLevel returns the top-level test of a (sub)test name.
func goTopLevel(name string) string {
	if i := strings.IndexByte(name, '/'); i >= 0 {
		return name[:i]
	}
	return name
}

// Line parses one line. Non-JSON lines (build errors on stderr) become
// output events.
func (p *GoParser) Line(line string) []Event {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return []Event{{Kind: EventOutput, Line: line}}
	}
	var e goEvent
	if err := json.Unmarshal([]byte(trimmed), &e); err != nil {
		return []Event{{Kind: EventOutput, Line: line}}
	}
	pkg := e.Package
	if pkg == "" {
		pkg = e.ImportPath
	}
	switch e.Action {
	case "output", "build-output":
		out := strings.TrimRight(e.Output, "\n")
		ev := Event{Kind: EventOutput, Package: pkg, Test: e.Test, Line: out}
		if e.Test != "" {
			key := pkg + "\x00" + goTopLevel(e.Test)
			p.testOut[key] = append(p.testOut[key], out)
		} else {
			p.pkgOut[pkg] = append(p.pkgOut[pkg], out)
		}
		return []Event{ev}
	case "pass", "fail", "skip":
		if e.Test == "" {
			return p.packageDone(pkg, e)
		}
		if strings.Contains(e.Test, "/") {
			return nil // subtests roll up into their parent
		}
		key := pkg + "\x00" + e.Test
		out := p.testOut[key]
		delete(p.testOut, key)
		ev := Event{
			Kind:     EventResult,
			Package:  pkg,
			Test:     e.Test,
			Status:   goStatus(e.Action),
			Duration: time.Duration(e.Elapsed * float64(time.Second)),
			Output:   out,
		}
		switch ev.Status {
		case StatusFailed:
			ev.Message, ev.FailFile, ev.FailLine = goFailure(out)
		case StatusSkipped:
			ev.Message, _, _ = goFailure(out) // the t.Skip reason
		}
		return []Event{ev}
	}
	return nil
}

// packageDone handles a package-level pass/fail. Only build / setup
// failures become package events — an ordinary failing package is just
// the sum of its failing tests.
func (p *GoParser) packageDone(pkg string, e goEvent) []Event {
	out := p.pkgOut[pkg]
	delete(p.pkgOut, pkg)
	if e.Action != "fail" {
		return []Event{{Kind: EventPackage, Package: pkg, Status: StatusPassed}}
	}
	buildFail := e.FailedBuild != ""
	var msg []string
	for _, ln := range out {
		if strings.Contains(ln, "[build failed]") || strings.Contains(ln, "[setup failed]") {
			buildFail = true
		}
		if t := goPkgLine(ln); t != "" {
			msg = append(msg, t)
		}
	}
	if e.FailedBuild != "" {
		for _, ln := range p.pkgOut[e.FailedBuild] {
			if t := goPkgLine(ln); t != "" {
				msg = append(msg, t)
			}
		}
		delete(p.pkgOut, e.FailedBuild)
	}
	if !buildFail && len(msg) == 0 {
		return nil
	}
	m := strings.Join(msg, "\n")
	if m == "" {
		m = "build failed"
	}
	ev := Event{Kind: EventPackage, Package: pkg, Status: StatusFailed, Message: m}
	_, ev.FailFile, ev.FailLine = goFailure(msg)
	return []Event{ev}
}

// goPkgLine filters package-level output down to the useful lines (no
// "FAIL\tpkg", "ok pkg" or "# pkg" headers).
func goPkgLine(ln string) string {
	t := strings.TrimSpace(ln)
	if t == "" || t == "FAIL" || t == "PASS" || strings.HasPrefix(t, "FAIL\t") ||
		strings.HasPrefix(t, "ok ") || strings.HasPrefix(t, "# ") {
		return ""
	}
	return t
}

// Flush has nothing to emit for Go: every test ends with its own action.
func (p *GoParser) Flush() []Event { return nil }

func goStatus(action string) Status {
	switch action {
	case "pass":
		return StatusPassed
	case "skip":
		return StatusSkipped
	}
	return StatusFailed
}

// goLocRe matches the "file.go:12:" prefix `t.Errorf` puts on each line
// (and compiler errors "./x.go:3:5:").
var goLocRe = regexp.MustCompile(`^\s*((?:[\w.\-]+/)*[\w.\-]+\.go):(\d+):`)

// goFailure strips test2json framing lines from a failing test's output
// and returns the message plus the first file:line it mentions.
func goFailure(out []string) (msg, file string, line int) {
	var keep []string
	for _, ln := range out {
		t := strings.TrimSpace(ln)
		switch {
		case t == "",
			strings.HasPrefix(t, "=== RUN"), strings.HasPrefix(t, "=== PAUSE"),
			strings.HasPrefix(t, "=== CONT"), strings.HasPrefix(t, "=== NAME"),
			strings.HasPrefix(t, "--- PASS"), strings.HasPrefix(t, "--- FAIL"),
			strings.HasPrefix(t, "--- SKIP"), t == "PASS", t == "FAIL":
			continue
		}
		if file == "" {
			if m := goLocRe.FindStringSubmatch(ln); m != nil {
				file = m[1]
				line, _ = strconv.Atoi(m[2])
			}
		}
		keep = append(keep, t)
	}
	return strings.Join(keep, "\n"), file, line
}

// ── pytest -v (streaming) + junitxml (details) ───────────────────────────

// pytestLineRe matches a verbose result line, with or without an xdist
// "[gw0]" prefix: "tests/test_a.py::TestX::test_m[1] PASSED   [ 50%]".
var pytestLineRe = regexp.MustCompile(`^(?:\[gw\d+\]\s+(?:\[\s*\d+%\]\s+)?)?(\S+?\.py)::(\S+)\s+(PASSED|FAILED|SKIPPED|XFAIL|XPASS|ERROR)\b`)

// pytestXdistRe matches xdist's "[gw0] [ 50%] PASSED tests/x.py::t" order.
var pytestXdistRe = regexp.MustCompile(`^\[gw\d+\]\s+(?:\[\s*\d+%\]\s+)?(PASSED|FAILED|SKIPPED|XFAIL|XPASS|ERROR)\s+(\S+?\.py)::(\S+)`)

// PytestParser parses `pytest -v` result lines as they stream.
type PytestParser struct{}

// Line parses one line of pytest output.
func (PytestParser) Line(line string) []Event {
	out := []Event{{Kind: EventOutput, Line: line}}
	var file, name, word string
	if m := pytestLineRe.FindStringSubmatch(line); m != nil {
		file, name, word = m[1], m[2], m[3]
	} else if m := pytestXdistRe.FindStringSubmatch(line); m != nil {
		word, file, name = m[1], m[2], m[3]
	} else {
		return out
	}
	return append(out, Event{
		Kind:   EventResult,
		File:   file,
		Test:   StripPytestParams(name),
		Status: pytestStatus(word),
	})
}

// Flush has nothing buffered.
func (PytestParser) Flush() []Event { return nil }

func pytestStatus(word string) Status {
	switch word {
	case "PASSED", "XPASS":
		return StatusPassed
	case "SKIPPED", "XFAIL":
		return StatusSkipped
	}
	return StatusFailed
}

// StripPytestParams drops a parametrisation suffix: "test_x[1-2]" → "test_x".
func StripPytestParams(name string) string {
	if i := strings.IndexByte(name, '['); i > 0 && strings.HasSuffix(name, "]") {
		return name[:i]
	}
	return name
}

type junitSuites struct {
	Suites []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Suites []junitSuite `xml:"testsuite"`
	Cases  []junitCase  `xml:"testcase"`
}

type junitCase struct {
	Classname string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	File      string        `xml:"file,attr"`
	Line      string        `xml:"line,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitProblem `xml:"failure"`
	Error     *junitProblem `xml:"error"`
	Skipped   *junitProblem `xml:"skipped"`
}

type junitProblem struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

// pyLocRe matches a traceback location line "tests/test_a.py:12: Error".
var pyLocRe = regexp.MustCompile(`(?m)^(\S+\.py):(\d+):`)

// ParseJUnitXML parses a pytest --junitxml report (xunit1 or xunit2).
// Events carry Class = dotted classname and Test = the test name with
// parameters stripped; Tree.Apply resolves the file from the classname.
func ParseJUnitXML(data []byte) ([]Event, error) {
	var root junitSuites
	if err := xml.Unmarshal(data, &root); err != nil || len(root.Suites) == 0 {
		// Some writers emit a bare <testsuite> root.
		var single junitSuite
		if err2 := xml.Unmarshal(data, &single); err2 != nil {
			if err != nil {
				return nil, err
			}
			return nil, err2
		}
		root.Suites = []junitSuite{single}
	}
	var out []Event
	var walk func(s junitSuite)
	walk = func(s junitSuite) {
		for _, c := range s.Cases {
			ev := Event{Kind: EventResult, Class: c.Classname, Test: StripPytestParams(c.Name), File: c.File, Status: StatusPassed}
			if secs, err := strconv.ParseFloat(c.Time, 64); err == nil {
				ev.Duration = time.Duration(secs * float64(time.Second))
			}
			prob := c.Failure
			if prob == nil {
				prob = c.Error
			}
			switch {
			case prob != nil:
				ev.Status = StatusFailed
				ev.Message = strings.TrimSpace(prob.Message)
				text := strings.TrimSpace(prob.Text)
				if text != "" {
					ev.Output = strings.Split(text, "\n")
					if ev.Message == "" {
						ev.Message = text
					}
				}
				if locs := pyLocRe.FindAllStringSubmatch(text, -1); len(locs) > 0 {
					last := locs[len(locs)-1]
					ev.FailFile = last[1]
					ev.FailLine, _ = strconv.Atoi(last[2])
				}
			case c.Skipped != nil:
				ev.Status = StatusSkipped
				ev.Message = strings.TrimSpace(c.Skipped.Message)
			}
			out = append(out, ev)
		}
		for _, sub := range s.Suites {
			walk(sub)
		}
	}
	for _, s := range root.Suites {
		walk(s)
	}
	return out, nil
}

// ── cargo test (libtest text output) ─────────────────────────────────────

var (
	cargoRunningRe = regexp.MustCompile(`^\s*Running (?:unittests )?(\S+)`)
	cargoResultRe  = regexp.MustCompile(`^test (\S+) \.\.\. (ok|FAILED|ignored)`)
	cargoStdoutRe  = regexp.MustCompile(`^---- (\S+) stdout ----$`)
	// New format: "panicked at src/lib.rs:10:5:"; old: "panicked at 'msg', src/lib.rs:10:5".
	cargoPanicRe = regexp.MustCompile(`panicked at (?:'.*', )?([^\s:']+\.rs):(\d+):\d+`)
)

// CargoParser parses `cargo test` output: per-test result lines as they
// stream, then the failure details in the "failures:" section.
type CargoParser struct {
	src     string   // source of the running test binary
	capName string   // test whose stdout block is being captured
	capBuf  []string // captured lines
}

// Line parses one line of cargo output.
func (p *CargoParser) Line(line string) []Event {
	out := []Event{{Kind: EventOutput, Line: line}}
	trimmed := strings.TrimRight(line, " \r")
	if m := cargoStdoutRe.FindStringSubmatch(trimmed); m != nil {
		out = append(out, p.endCapture()...)
		p.capName = m[1]
		return out
	}
	if p.capName != "" {
		if trimmed == "failures:" || strings.HasPrefix(trimmed, "test result:") {
			return append(out, p.endCapture()...)
		}
		p.capBuf = append(p.capBuf, line)
		return out
	}
	if m := cargoRunningRe.FindStringSubmatch(trimmed); m != nil {
		p.src = m[1]
		return out
	}
	if m := cargoResultRe.FindStringSubmatch(trimmed); m != nil {
		st := StatusPassed
		switch m[2] {
		case "FAILED":
			st = StatusFailed
		case "ignored":
			st = StatusSkipped
		}
		out = append(out, Event{Kind: EventResult, Package: p.src, Test: m[1], Status: st})
	}
	return out
}

// endCapture turns a captured "---- name stdout ----" block into a failed
// result carrying the panic message and location.
func (p *CargoParser) endCapture() []Event {
	if p.capName == "" {
		return nil
	}
	for len(p.capBuf) > 0 && strings.TrimSpace(p.capBuf[len(p.capBuf)-1]) == "" {
		p.capBuf = p.capBuf[:len(p.capBuf)-1]
	}
	ev := Event{Kind: EventResult, Package: p.src, Test: p.capName, Status: StatusFailed, Output: p.capBuf}
	var msg []string
	for _, ln := range p.capBuf {
		if m := cargoPanicRe.FindStringSubmatch(ln); m != nil && ev.FailFile == "" {
			ev.FailFile = m[1]
			ev.FailLine, _ = strconv.Atoi(m[2])
		}
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "note: run with `RUST_BACKTRACE") {
			continue
		}
		msg = append(msg, t)
	}
	ev.Message = strings.Join(msg, "\n")
	p.capName, p.capBuf = "", nil
	return []Event{ev}
}

// Flush emits a capture cut off by the end of stream.
func (p *CargoParser) Flush() []Event { return p.endCapture() }

// ── jest / vitest JSON report ────────────────────────────────────────────

type jestReport struct {
	TestResults []struct {
		Name             string `json:"name"`
		Status           string `json:"status"`
		Message          string `json:"message"`
		AssertionResults []struct {
			AncestorTitles  []string `json:"ancestorTitles"`
			Title           string   `json:"title"`
			Status          string   `json:"status"`
			Duration        *float64 `json:"duration"`
			FailureMessages []string `json:"failureMessages"`
			Location        *struct {
				Line int `json:"line"`
			} `json:"location"`
		} `json:"assertionResults"`
	} `json:"testResults"`
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// ParseJestJSON parses a jest `--json` (or vitest `--reporter=json`)
// report. A file that failed to run at all (syntax error, missing module)
// becomes an EventPackage with File set.
func ParseJestJSON(data []byte) ([]Event, error) {
	var r jestReport
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	var out []Event
	for _, tr := range r.TestResults {
		if len(tr.AssertionResults) == 0 && tr.Status == "failed" {
			out = append(out, Event{Kind: EventPackage, File: tr.Name, Status: StatusFailed,
				Message: strings.TrimSpace(ansiRe.ReplaceAllString(tr.Message, ""))})
			continue
		}
		for _, a := range tr.AssertionResults {
			parts := append(append([]string(nil), a.AncestorTitles...), a.Title)
			ev := Event{Kind: EventResult, File: tr.Name, Test: strings.Join(parts, JSNameSep), Status: jestStatus(a.Status)}
			if a.Duration != nil {
				ev.Duration = time.Duration(*a.Duration * float64(time.Millisecond))
			}
			if len(a.FailureMessages) > 0 {
				text := ansiRe.ReplaceAllString(strings.Join(a.FailureMessages, "\n"), "")
				ev.Output = strings.Split(strings.TrimSpace(text), "\n")
				ev.Message = jestMessage(ev.Output)
				ev.FailFile, ev.FailLine = jestLocation(text, tr.Name)
			}
			if ev.FailLine == 0 && ev.Status == StatusFailed && a.Location != nil {
				ev.FailFile, ev.FailLine = tr.Name, a.Location.Line
			}
			out = append(out, ev)
		}
	}
	return out, nil
}

func jestStatus(s string) Status {
	switch s {
	case "passed":
		return StatusPassed
	case "failed":
		return StatusFailed
	}
	return StatusSkipped // pending, skipped, todo, disabled
}

// jestMessage keeps the lines before the stack trace.
func jestMessage(lines []string) string {
	var keep []string
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "at ") {
			break
		}
		keep = append(keep, strings.TrimRight(ln, " "))
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

var jsStackRe = regexp.MustCompile(`\(?((?:/|[A-Za-z]:\\)[^\s():]+):(\d+):\d+\)?`)

// jestLocation returns the first stack frame inside the test file itself
// (falling back to the first frame outside node_modules).
func jestLocation(text, testFile string) (string, int) {
	var fbFile string
	var fbLine int
	for _, m := range jsStackRe.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[2])
		if m[1] == testFile {
			return m[1], n
		}
		if fbFile == "" && !strings.Contains(m[1], "node_modules") {
			fbFile, fbLine = m[1], n
		}
	}
	return fbFile, fbLine
}
