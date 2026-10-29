package ai

import (
	"strings"
	"testing"
)

func redactPerfSecrets() {
	for i := 0; i < 12; i++ {
		RegisterSecret("perf-secret-value-" + strings.Repeat("x", i) + "-end")
	}
}

// A short line (a run's task, an error): the common call.
func BenchmarkRedactShort(b *testing.B) {
	redactPerfSecrets()
	s := "task 12/3: fix the flaky test in internal/app"
	b.ReportAllocs()
	for b.Loop() {
		Redact(s)
	}
}

// A 32 KB tool result without secrets.
func BenchmarkRedact32KB(b *testing.B) {
	redactPerfSecrets()
	s := strings.Repeat("func handler(w http.ResponseWriter, r *http.Request) { // Token handling\n", 450)
	b.ReportAllocs()
	for b.Loop() {
		Redact(s)
	}
}

func BenchmarkRedactSecrets32KB(b *testing.B) {
	redactPerfSecrets()
	s := strings.Repeat("func handler(w http.ResponseWriter, r *http.Request) { // Token handling\n", 450)
	b.ReportAllocs()
	for b.Loop() {
		RedactSecrets(s)
	}
}

// A secret registered after earlier redactions is hidden from then on,
// and a longer secret that holds a shorter one is hidden whole.
func TestRedactSecretListStaysCurrent(t *testing.T) {
	short, long := "zq-short-secret-1", "zq-short-secret-1-and-more"
	if got := Redact("x " + short); got != "x "+short {
		t.Fatalf("not registered yet: %q", got)
	}
	RegisterSecret(short)
	if got := Redact("x " + short); got != "x [REDACTED]" {
		t.Errorf("after register: %q", got)
	}
	RegisterSecret(long)
	RegisterSecret(long) // again: no change
	for _, f := range []func(string) string{Redact, RedactSecrets} {
		if got := f("a " + long + " b " + short); got != "a [REDACTED] b [REDACTED]" {
			t.Errorf("long and short: %q", got)
		}
	}
}
