package perfbudget

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec: budgets.txt lines are "name limit unit", "#" starts a comment,
// blank lines are skipped; a bad line is an error with its line number.
func TestParse(t *testing.T) {
	bs, err := Parse("# comment\n\nstartup_ms  800  ms  # first frame\nrss 300 MB\n")
	if err != nil {
		t.Fatal(err)
	}
	if b := bs["startup_ms"]; b.Limit != 800 || b.Unit != "ms" {
		t.Errorf("startup_ms = %+v", b)
	}
	if b := bs["rss"]; b.Limit != 300 || b.Unit != "MB" {
		t.Errorf("rss = %+v", b)
	}
	for _, bad := range []string{"x\n", "x abc ms\n", "x 1 ms\nx 2 ms\n"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) gave no error", bad)
		}
	}
}

// Spec: the checked-in budgets.txt parses and has every budget the perf
// tests use.
func TestBudgetsFile(t *testing.T) {
	bs, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"startup_first_frame", "editor_key_to_frame_p95", "session_idle_rss",
		"open_50k_line_file", "palette_open_all", "explorer_5k_files",
		"agent_worktree_create_5k", "agent_worktree_merge_20", "chat_save_2k_messages", "chat_load_2k_messages"} {
		if _, ok := bs[name]; !ok {
			t.Errorf("budgets.txt has no %s", name)
		}
	}
}

type fakeT struct {
	logs, errs []string
}

func (f *fakeT) Helper()                        {}
func (f *fakeT) Logf(format string, a ...any)   { f.logs = append(f.logs, fmt.Sprintf(format, a...)) }
func (f *fakeT) Errorf(format string, a ...any) { f.errs = append(f.errs, fmt.Sprintf(format, a...)) }

// Spec: Check logs the number with its budget and writes a row to
// $TERMOCODE_PERF_OUT; a number over the budget fails the test; an
// unknown name fails too.
func TestCheck(t *testing.T) {
	out := filepath.Join(t.TempDir(), "perf.tsv")
	t.Setenv("TERMOCODE_PERF_OUT", out)
	bs := map[string]Budget{"a": {Name: "a", Limit: 100, Unit: "ms"}}

	ok := &fakeT{}
	check(ok, bs, "a", 40)
	if len(ok.errs) != 0 || len(ok.logs) != 1 || !strings.Contains(ok.logs[0], "40") || !strings.Contains(ok.logs[0], "100") {
		t.Errorf("under budget: logs %q errs %q", ok.logs, ok.errs)
	}
	over := &fakeT{}
	check(over, bs, "a", 140)
	if len(over.errs) != 1 || !strings.Contains(over.errs[0], "budget") {
		t.Errorf("over budget: errs %q", over.errs)
	}
	unknown := &fakeT{}
	check(unknown, bs, "nope", 1)
	if len(unknown.errs) != 1 {
		t.Errorf("unknown name: errs %q", unknown.errs)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), "a\t40\t100\tms\tok\na\t140\t100\tms\tOVER\n"; got != want {
		t.Errorf("perf out = %q, want %q", got, want)
	}
}

// Spec: perf tests run only with TERMOCODE_PERF=1 and not with -short.
func TestEnabled(t *testing.T) {
	t.Setenv("TERMOCODE_PERF", "")
	if enabled(false) {
		t.Error("enabled without TERMOCODE_PERF=1")
	}
	t.Setenv("TERMOCODE_PERF", "1")
	if !enabled(false) || enabled(true) {
		t.Error("want enabled with TERMOCODE_PERF=1, not with -short")
	}
}
