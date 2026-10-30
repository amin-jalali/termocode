// Package crash keeps a bug from losing the user's work: it recovers
// panics in background goroutines and Bubble Tea commands, writes a
// crash report, and tells the UI (a toast: the app goes on). A panic in
// the UI loop itself ends the program, but only after the report is
// written; the next start shows a card about it.
//
// Reports live in <state>/termocode/crash/ (0600, the last keepReports).
// They hold the time, the version, Go and OS, the panic value and stack
// and the types of the last UI events. Every string passes the app's
// redactor (SetRedactor) and a built-in mask first: no secrets, no
// environment values, no home folder. Buffer text and file contents are
// never put in on purpose; the panic value is shortened to maxValue.
package crash

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

const (
	keepReports = 5    // reports kept on disk
	maxEvents   = 30   // UI event types kept for a report
	maxValue    = 1000 // bytes of the panic value in a report
	pendingName = "pending"
)

var (
	redactor atomic.Pointer[func(string) string]
	version  atomic.Pointer[string]

	eventsMu sync.Mutex
	events   []string

	writeMu sync.Mutex // one report write (and prune) at a time
)

// SetRedactor sets the app's secret mask (ai.Redact). nil: only the
// built-in mask.
func SetRedactor(f func(string) string) {
	if f == nil {
		redactor.Store(nil)
		return
	}
	redactor.Store(&f)
}

// SetVersion sets the version written into reports.
func SetVersion(v string) { version.Store(&v) }

// RecordEvent remembers the type of one UI event (never its content).
func RecordEvent(kind string) {
	eventsMu.Lock()
	events = append(events, kind)
	if len(events) > maxEvents {
		events = append(events[:0], events[len(events)-maxEvents:]...)
	}
	eventsMu.Unlock()
}

// Events returns the remembered event types, oldest first.
func Events() []string {
	eventsMu.Lock()
	defer eventsMu.Unlock()
	return append([]string(nil), events...)
}

func resetEvents() {
	eventsMu.Lock()
	events = nil
	eventsMu.Unlock()
}

// Dir is <XDG_STATE_HOME or ~/.local/state>/termocode/crash.
func Dir() (string, error) {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "termocode", "crash"), nil
}

// tokenRe matches the usual key shapes (sk-…, ghp_…, Bearer …, key=…).
var tokenRe = lazyre.New(`(?i)(sk-[A-Za-z0-9_\-]{8,}|gh[pousr]_[A-Za-z0-9]{8,}|xox[abpr]-[A-Za-z0-9\-]{8,}|AKIA[A-Z0-9]{12,}|bearer\s+[A-Za-z0-9._\-]{8,}|((key|token|secret|password|passwd)\s*[=:]\s*)[^\s"']{4,})`)

// clean masks secrets, environment values and the home folder in s.
func clean(s string) string {
	if f := redactor.Load(); f != nil {
		s = (*f)(s)
	}
	s = tokenRe.ReplaceAllString(s, "[REDACTED]")
	home, _ := os.UserHomeDir()
	var vals []string
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if k == "HOME" || len(v) < 8 {
			continue
		}
		vals = append(vals, v)
	}
	// Longest first: a value inside another one is masked with it.
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	for _, v := range vals {
		if home != "" && v == home {
			continue
		}
		s = strings.ReplaceAll(s, v, "[ENV]")
	}
	if len(home) > 1 {
		s = strings.ReplaceAll(s, home, "~")
	}
	return s
}

// WriteReport writes a crash report for a panic in where and returns its
// path. Older reports beyond keepReports are removed.
func WriteReport(where string, r any, stack []byte) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	now := time.Now()
	value := fmt.Sprint(r)
	if len(value) > maxValue {
		value = value[:maxValue] + " …"
	}
	ver := "unknown"
	if v := version.Load(); v != nil && *v != "" {
		ver = *v
	}
	var b strings.Builder
	b.WriteString("termocode crash report\n\n")
	fmt.Fprintf(&b, "time:    %s\n", now.Format(time.RFC3339))
	fmt.Fprintf(&b, "version: %s\n", ver)
	fmt.Fprintf(&b, "go:      %s\n", runtime.Version())
	fmt.Fprintf(&b, "os:      %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "where:   %s\n", where)
	fmt.Fprintf(&b, "panic:   %s\n", value)
	b.WriteString("\nlast UI events (oldest first, types only):\n")
	for _, e := range Events() {
		b.WriteString("  " + e + "\n")
	}
	b.WriteString("\nstack:\n")
	b.Write(stack)
	b.WriteString("\n")
	name := fmt.Sprintf("crash-%s-%09d-%d.txt", now.Format("20060102-150405"), now.Nanosecond(), os.Getpid())
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(clean(b.String())), 0o600); err != nil {
		return "", err
	}
	prune(dir)
	return path, nil
}

// prune removes all but the newest keepReports reports (names sort by
// time).
func prune(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "crash-") && strings.HasSuffix(e.Name(), ".txt") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keepReports {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// MarkFatal records that the program ended with the report at path; the
// next start shows a card about it (Pending).
func MarkFatal(path string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, pendingName), []byte(path), 0o600)
}

// Pending returns the report of a crash the user was not told about yet
// ("" when none, or its report is gone).
func Pending() string {
	dir, err := Dir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, pendingName))
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(b))
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) || path == "" {
		return ""
	}
	return path
}

// ClearPending forgets the pending crash (the card was shown).
func ClearPending() {
	if dir, err := Dir(); err == nil {
		_ = os.Remove(filepath.Join(dir, pendingName))
	}
}
