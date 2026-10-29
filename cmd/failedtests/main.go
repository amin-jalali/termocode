// Command failedtests reads `go test -json` output on stdin and lists
// the tests that failed. The nightly flaky hunter
// (.github/workflows/nightly.yml) files one GitHub issue per test.
//
//	go test -json -count=5 ./... | failedtests
//	    one line per failed test: package, name, "failed/runs"
//	failedtests -pkg P -test T -tail 80 < out.json
//	    the last 80 log lines of the last failing run of P's test T
//	    (-test "" for a package that failed with no test to blame)
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// Failure is one test (top level; a failing subtest counts for its
// parent) that failed at least once. Test is "" when the package failed
// with no test to blame (a build error, TestMain).
type Failure struct {
	Package string
	Test    string
	Failed  int      // runs that failed
	Runs    int      // runs that ended (pass or fail)
	Log     []string // output of the last failing run
}

// Name is the issue title name: the test, or the package.
func (f Failure) Name() string {
	if f.Test == "" {
		return f.Package
	}
	return f.Test
}

// Tail is the last n lines of the log.
func (f Failure) Tail(n int) []string {
	if len(f.Log) > n {
		return f.Log[len(f.Log)-n:]
	}
	return f.Log
}

type event struct {
	Action  string
	Package string
	Test    string
	Output  string
}

type pkgState struct {
	running map[string]bool     // top-level tests started and not ended
	buf     map[string][]string // output of each top-level test's current run
	out     []string            // package-level output
	failed  bool                // some test of this package failed
}

// maxLog caps the kept log of one run (a chatty test).
const maxLog = 2000

func appendCapped(s []string, line string) []string {
	s = append(s, line)
	if len(s) > maxLog {
		s = s[len(s)-maxLog:]
	}
	return s
}

// Failures parses `go test -json` output. Lines that are not JSON are
// ignored. The order is the order of the first failure.
func Failures(r io.Reader) ([]Failure, error) {
	pkgs := map[string]*pkgState{}
	index := map[string]int{} // package + "\x00" + test → position in out
	var out []Failure
	state := func(p string) *pkgState {
		s := pkgs[p]
		if s == nil {
			s = &pkgState{running: map[string]bool{}, buf: map[string][]string{}}
			pkgs[p] = s
		}
		return s
	}
	ended := map[string]int{} // package + "\x00" + test → runs that ended
	fail := func(pkg, test string, log []string) {
		k := pkg + "\x00" + test
		ended[k]++
		i, ok := index[k]
		if !ok {
			out = append(out, Failure{Package: pkg, Test: test})
			i = len(out) - 1
			index[k] = i
		}
		out[i].Failed++
		out[i].Log = append([]string(nil), log...)
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var e event
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Package == "" {
			continue
		}
		s := state(e.Package)
		top, _, sub := strings.Cut(e.Test, "/")
		switch {
		case e.Test == "" && e.Action == "output":
			s.out = appendCapped(s.out, e.Output)
		case e.Test == "" && e.Action == "start":
			s.out = nil
		case e.Test == "" && e.Action == "fail":
			blamed := false
			for t := range s.running {
				log := append(append([]string(nil), s.buf[t]...), s.out...)
				fail(e.Package, t, log)
				blamed = true
			}
			if !blamed && !s.failed {
				fail(e.Package, "", s.out)
			}
			delete(pkgs, e.Package)
		case e.Test == "" && e.Action == "pass":
			delete(pkgs, e.Package)
		case e.Action == "run" && !sub:
			s.running[top] = true
			s.buf[top] = nil
		case e.Action == "output":
			s.buf[top] = appendCapped(s.buf[top], e.Output)
		case (e.Action == "pass" || e.Action == "fail") && !sub:
			delete(s.running, top)
			if e.Action == "fail" {
				s.failed = true
				fail(e.Package, top, s.buf[top])
			} else {
				ended[e.Package+"\x00"+top]++
			}
		case e.Action == "skip" && !sub:
			delete(s.running, top)
		}
	}
	for i := range out {
		out[i].Runs = ended[out[i].Package+"\x00"+out[i].Test]
	}
	return out, sc.Err()
}

func main() {
	pkg := flag.String("pkg", "", "with -tail: the package of the test")
	test := flag.String("test", "", "with -tail: the test name")
	tail := flag.Int("tail", 0, "print the last N log lines of -pkg/-test")
	flag.Parse()
	fs, err := Failures(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failedtests:", err)
		os.Exit(2)
	}
	if *tail > 0 {
		for _, f := range fs {
			if f.Package == *pkg && f.Test == *test {
				fmt.Print(strings.Join(f.Tail(*tail), ""))
				return
			}
		}
		fmt.Fprintf(os.Stderr, "failedtests: no failure %s %s\n", *pkg, *test)
		os.Exit(1)
	}
	for _, f := range fs {
		fmt.Printf("%s\t%s\t%d/%d\n", f.Package, f.Test, f.Failed, f.Runs)
	}
}
