package crash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTempState points the crash dir at a temp state dir and resets the
// package state the tests change.
func useTempState(t *testing.T) string {
	t.Helper()
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("HOME", t.TempDir())
	resetEvents()
	SetRedactor(nil)
	return filepath.Join(state, "termocode", "crash")
}

func TestWriteReportHasTheFacts(t *testing.T) {
	dir := useTempState(t)
	SetVersion("v9.9.9-test")
	RecordEvent("tea.KeyMsg")
	RecordEvent("nvim.RedrawMsg")
	path, err := WriteReport("update", "index out of range", []byte("goroutine 1 [running]:\nmain.f()\n\t/src/x.go:3"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("report at %s, want in %s", path, dir)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("report mode %v, want 0600", info.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	for _, want := range []string{"v9.9.9-test", "go1", "update", "index out of range", "main.f()", "tea.KeyMsg", "nvim.RedrawMsg"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("report misses %q:\n%s", want, b)
		}
	}
}

// TestReportHidesSecrets: a secret in the panic value or the stack, an
// environment value and the home folder never reach the file.
func TestReportHidesSecrets(t *testing.T) {
	useTempState(t)
	canary := "sk-" + "canary" + "0123456789abcdef"
	envVal := "env" + "-canary-" + "value-42"
	t.Setenv("TERMOCODE_TEST_ENV_CANARY", envVal)
	home := os.Getenv("HOME")
	SetRedactor(func(s string) string { return strings.ReplaceAll(s, canary, "[REDACTED]") })
	path, err := WriteReport("worker", "bad token "+canary+" in "+home+"/x and "+envVal,
		[]byte("stack "+canary+" "+envVal))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, bad := range []string{canary, envVal, home} {
		if strings.Contains(string(b), bad) {
			t.Errorf("report holds %q:\n%s", bad, b)
		}
	}
}

// TestReportHidesTokensWithoutRedactor: the built-in mask catches the
// usual key shapes even before the app set its redactor.
func TestReportHidesTokensWithoutRedactor(t *testing.T) {
	useTempState(t)
	canary := "sk-" + "canary" + "0123456789abcdef"
	path, err := WriteReport("worker", "key="+canary, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), canary) {
		t.Errorf("report holds the token:\n%s", b)
	}
}

func TestReportValueIsShortened(t *testing.T) {
	useTempState(t)
	path, err := WriteReport("worker", strings.Repeat("x", 5000), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Count(string(b), "x") > maxValue+10 {
		t.Errorf("panic value not shortened: %d bytes", len(b))
	}
}

func TestKeepsLastFiveReports(t *testing.T) {
	dir := useTempState(t)
	var paths []string
	for i := 0; i < 8; i++ {
		p, err := WriteReport("worker", i, nil)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	ents, _ := os.ReadDir(dir)
	var left []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".txt") {
			left = append(left, filepath.Join(dir, e.Name()))
		}
	}
	if len(left) != keepReports {
		t.Fatalf("%d reports left, want %d: %v", len(left), keepReports, left)
	}
	for _, p := range paths[len(paths)-keepReports:] {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("newest report %s gone", p)
		}
	}
}

func TestEventsRingKeepsTheLast(t *testing.T) {
	useTempState(t)
	for i := 0; i < maxEvents+7; i++ {
		RecordEvent("a")
	}
	RecordEvent("last")
	ev := Events()
	if len(ev) != maxEvents || ev[len(ev)-1] != "last" {
		t.Fatalf("events = %d, last %q", len(ev), ev[len(ev)-1])
	}
}

func TestPendingMarker(t *testing.T) {
	useTempState(t)
	if p := Pending(); p != "" {
		t.Fatalf("pending %q before any crash", p)
	}
	path, err := WriteReport("update", "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkFatal(path); err != nil {
		t.Fatal(err)
	}
	if p := Pending(); p != path {
		t.Fatalf("pending %q, want %q", p, path)
	}
	ClearPending()
	if p := Pending(); p != "" {
		t.Fatalf("pending %q after clear", p)
	}
}
