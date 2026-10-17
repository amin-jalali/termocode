// Package servicestest has a fake `docker` for tests: a shell script in
// a temp dir that logs its argv and answers from files. No test ever
// talks to a real Docker daemon.
//
// Files in Fake.Data (set them with Set):
//
//	config.json          answer of `compose config --format json`
//	ps.json              answer of `compose ps --all --format json`
//	logs-<name>.txt      answer of `compose logs … <name>`
//	all.err / all.code   every call fails with this stderr / exit code
//	<sub>.err/<sub>.code `compose <sub>` fails (sub = ps, config, stop, …)
//	<sub>.sleep          `compose <sub>` sleeps this many seconds first
//	up.code              exit code of `compose up`
//	events.log           `docker events` follows it (PushEvent appends)
//
// Written by the script: argv.log (one call per line), events.pid,
// <sub>.pid (the script's pid while it runs).
package servicestest

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Fake is one installed fake docker.
type Fake struct {
	Bin  string // dir with the `docker` script: put it first on PATH
	Data string // the answer files and logs
}

const script = `#!/bin/sh
D='@DATA@'
printf '%s\n' "$*" >> "$D/argv.log"
if [ -f "$D/all.err" ]; then cat "$D/all.err" >&2; exit "$(cat "$D/all.code" 2>/dev/null || echo 1)"; fi
if [ "$1" = "events" ]; then
  echo $$ > "$D/events.pid"
  touch "$D/events.log"
  exec tail -n 0 -f "$D/events.log"
fi
if [ "$1" != "compose" ]; then exit 0; fi
shift
while [ $# -gt 0 ]; do
  case "$1" in
    -f|-p|--profile|--project-directory|--project-name|--file) shift 2 ;;
    -*) shift ;;
    *) break ;;
  esac
done
sub="$1"; shift
echo $$ > "$D/$sub.pid"
if [ -f "$D/$sub.err" ]; then cat "$D/$sub.err" >&2; exit "$(cat "$D/$sub.code" 2>/dev/null || echo 1)"; fi
if [ -f "$D/$sub.sleep" ]; then sleep "$(cat "$D/$sub.sleep")" & echo $! > "$D/$sub.child"; wait; fi
case "$sub" in
  config) cat "$D/config.json" ;;
  ps) cat "$D/ps.json" 2>/dev/null ;;
  logs) name=""; for a in "$@"; do name="$a"; done; cat "$D/logs-$name.txt" 2>/dev/null ;;
  up) echo "[+] Running"; exit "$(cat "$D/up.code" 2>/dev/null || echo 0)" ;;
  version) echo "Docker Compose version v2.30.0" ;;
  exec) echo "fake shell"; exit 0 ;;
esac
exit 0
`

// New installs a fake docker in temp dirs.
func New(t testing.TB) *Fake {
	t.Helper()
	f := &Fake{Bin: t.TempDir(), Data: t.TempDir()}
	body := strings.ReplaceAll(script, "@DATA@", f.Data)
	if err := os.WriteFile(filepath.Join(f.Bin, "docker"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

// Docker is the script's path.
func (f *Fake) Docker() string { return filepath.Join(f.Bin, "docker") }

// PathEnv is a PATH value with the fake first.
func (f *Fake) PathEnv() string { return f.Bin + string(os.PathListSeparator) + os.Getenv("PATH") }

// Set writes one answer file.
func (f *Fake) Set(t testing.TB, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.Data, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Remove deletes one answer file.
func (f *Fake) Remove(name string) { _ = os.Remove(filepath.Join(f.Data, name)) }

// Calls returns the logged argv lines (without "docker").
func (f *Fake) Calls() []string {
	b, _ := os.ReadFile(filepath.Join(f.Data, "argv.log"))
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// Count returns how many calls contain sub.
func (f *Fake) Count(sub string) int {
	n := 0
	for _, c := range f.Calls() {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

// PushEvent appends one line to the events stream.
func (f *Fake) PushEvent(t testing.TB, line string) {
	t.Helper()
	fh, err := os.OpenFile(filepath.Join(f.Data, "events.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	if _, err := fh.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

// PID reads a pid file the script wrote (events.pid, ps.pid, ps.child).
func (f *Fake) PID(name string) int {
	b, err := os.ReadFile(filepath.Join(f.Data, name))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// Alive reports whether pid still runs (and is not a zombie).
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true // no /proc (macOS): trust kill -0
	}
	// "pid (comm) S …": the state is the field after the last ')'.
	s := string(b)
	if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) {
		return s[i+2] != 'Z'
	}
	return true
}

// WaitGone waits up to d for pid to end.
func WaitGone(pid int, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if !Alive(pid) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return !Alive(pid)
}

// WaitFor waits up to d for cond.
func WaitFor(d time.Duration, cond func() bool) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}
