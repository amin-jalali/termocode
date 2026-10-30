package lazyre

import (
	"strings"
	"testing"
)

func TestCompiledOnFirstUseOnly(t *testing.T) {
	r := New(`a(b+)c`)
	if r.compiled() {
		t.Fatal("compiled at New; it must wait for the first use")
	}
	if !r.MatchString("xabbc") || r.MatchString("ac") {
		t.Fatal("MatchString wrong")
	}
	if !r.compiled() {
		t.Fatal("not compiled after the first use")
	}
	if got := r.FindStringSubmatch("abbbc"); len(got) != 2 || got[1] != "bbb" {
		t.Fatalf("FindStringSubmatch = %q", got)
	}
	if got := r.ReplaceAllString("abc-abbc", "X"); got != "X-X" {
		t.Fatalf("ReplaceAllString = %q", got)
	}
	if r.String() != `a(b+)c` {
		t.Fatalf("String = %q", r.String())
	}
}

// Every pattern made with New compiles: CompileAll finds a bad one the
// way regexp.MustCompile at package init did, but in a test.
func TestCompileAllReportsABadPattern(t *testing.T) {
	bad := New(`a(b`)
	defer forget(bad)
	err := CompileAll()
	if err == nil || !strings.Contains(err.Error(), "a(b") {
		t.Fatalf("CompileAll = %v, want an error naming the bad pattern", err)
	}
}

func TestConcurrentFirstUse(t *testing.T) {
	r := New(`^x+$`)
	done := make(chan bool)
	for i := 0; i < 8; i++ {
		go func() { done <- r.MatchString("xxx") }()
	}
	for i := 0; i < 8; i++ {
		if !<-done {
			t.Fatal("no match")
		}
	}
}

func BenchmarkFirstUse(b *testing.B) {
	for b.Loop() {
		r := New(`(?i)\b(error|warning):\s+(.*)$`)
		_ = r.MatchString("error: x")
		forget(r)
	}
}
