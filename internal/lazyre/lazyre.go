// Package lazyre is a regular expression compiled on first use.
//
// A package-level regexp.MustCompile runs in package init, in every
// termocode process (the UI, the session client and server, mcp …), for
// every pattern of every package, needed or not: ~140 patterns, ~2.5 ms
// of each start and their compiled programs on the heap for ever. New
// only records the pattern; the first match compiles it.
//
// A bad pattern no longer panics at init: CompileAll (run by a test of
// the termocode binary, which links every package) reports it instead.
package lazyre

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"
)

// Regexp is a regexp.Regexp compiled on first use. Safe for concurrent
// use, like regexp.Regexp.
type Regexp struct {
	expr string
	once sync.Once
	re   *regexp.Regexp
	err  error
	done atomic.Bool // load ran (tests)
}

var (
	allMu sync.Mutex
	all   []*Regexp
)

// New records expr, to be compiled on first use (it panics then if expr
// does not compile, like regexp.MustCompile).
func New(expr string) *Regexp {
	r := &Regexp{expr: expr}
	allMu.Lock()
	all = append(all, r)
	allMu.Unlock()
	return r
}

// CompileAll compiles every pattern made with New and reports the ones
// that do not compile.
func CompileAll() error {
	allMu.Lock()
	rs := slices.Clone(all)
	allMu.Unlock()
	var errs []error
	for _, r := range rs {
		if r.load(); r.err != nil {
			errs = append(errs, fmt.Errorf("lazyre: %q: %w", r.expr, r.err))
		}
	}
	return errors.Join(errs...)
}

// forget drops r from the CompileAll list (tests).
func forget(r *Regexp) {
	allMu.Lock()
	defer allMu.Unlock()
	all = slices.DeleteFunc(all, func(x *Regexp) bool { return x == r })
}

func (r *Regexp) load() {
	r.once.Do(func() {
		r.re, r.err = regexp.Compile(r.expr)
		r.done.Store(true)
	})
}

// compiled reports whether r was compiled (tests).
func (r *Regexp) compiled() bool { return r.done.Load() }

// Regexp returns the compiled expression (it panics if expr does not
// compile, like regexp.MustCompile).
func (r *Regexp) Regexp() *regexp.Regexp {
	r.load()
	if r.err != nil {
		panic(fmt.Sprintf("lazyre: Compile(%q): %v", r.expr, r.err))
	}
	return r.re
}

// String returns the source text (without compiling).
func (r *Regexp) String() string { return r.expr }

// The methods below are regexp.Regexp's.

func (r *Regexp) Match(b []byte) bool        { return r.Regexp().Match(b) }
func (r *Regexp) MatchString(s string) bool  { return r.Regexp().MatchString(s) }
func (r *Regexp) Find(b []byte) []byte       { return r.Regexp().Find(b) }
func (r *Regexp) FindIndex(b []byte) []int   { return r.Regexp().FindIndex(b) }
func (r *Regexp) FindString(s string) string { return r.Regexp().FindString(s) }
func (r *Regexp) FindStringIndex(s string) []int {
	return r.Regexp().FindStringIndex(s)
}
func (r *Regexp) FindSubmatch(b []byte) [][]byte { return r.Regexp().FindSubmatch(b) }
func (r *Regexp) FindSubmatchIndex(b []byte) []int {
	return r.Regexp().FindSubmatchIndex(b)
}
func (r *Regexp) FindStringSubmatch(s string) []string {
	return r.Regexp().FindStringSubmatch(s)
}
func (r *Regexp) FindStringSubmatchIndex(s string) []int {
	return r.Regexp().FindStringSubmatchIndex(s)
}
func (r *Regexp) FindAll(b []byte, n int) [][]byte { return r.Regexp().FindAll(b, n) }
func (r *Regexp) FindAllIndex(b []byte, n int) [][]int {
	return r.Regexp().FindAllIndex(b, n)
}
func (r *Regexp) FindAllString(s string, n int) []string {
	return r.Regexp().FindAllString(s, n)
}
func (r *Regexp) FindAllStringIndex(s string, n int) [][]int {
	return r.Regexp().FindAllStringIndex(s, n)
}
func (r *Regexp) FindAllSubmatch(b []byte, n int) [][][]byte {
	return r.Regexp().FindAllSubmatch(b, n)
}
func (r *Regexp) FindAllStringSubmatch(s string, n int) [][]string {
	return r.Regexp().FindAllStringSubmatch(s, n)
}
func (r *Regexp) FindAllStringSubmatchIndex(s string, n int) [][]int {
	return r.Regexp().FindAllStringSubmatchIndex(s, n)
}
func (r *Regexp) ReplaceAll(src, repl []byte) []byte {
	return r.Regexp().ReplaceAll(src, repl)
}
func (r *Regexp) ReplaceAllFunc(src []byte, f func([]byte) []byte) []byte {
	return r.Regexp().ReplaceAllFunc(src, f)
}
func (r *Regexp) ReplaceAllString(src, repl string) string {
	return r.Regexp().ReplaceAllString(src, repl)
}
func (r *Regexp) ReplaceAllLiteralString(src, repl string) string {
	return r.Regexp().ReplaceAllLiteralString(src, repl)
}
func (r *Regexp) ReplaceAllStringFunc(src string, f func(string) string) string {
	return r.Regexp().ReplaceAllStringFunc(src, f)
}
func (r *Regexp) Split(s string, n int) []string { return r.Regexp().Split(s, n) }
func (r *Regexp) SubexpNames() []string          { return r.Regexp().SubexpNames() }
func (r *Regexp) SubexpIndex(name string) int    { return r.Regexp().SubexpIndex(name) }
func (r *Regexp) NumSubexp() int                 { return r.Regexp().NumSubexp() }
