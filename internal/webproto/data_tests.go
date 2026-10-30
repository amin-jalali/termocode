package webproto

import "strings"

// The Test Explorer (WEBP-4): the tests the laptop's TESTING view lists
// (its own discovery: go test, pytest, cargo, jest, vitest), each with
// the state of its last run, and the run itself. The web never names a
// command: it runs an item of the tree by its id, and the laptop builds
// the command line as for a click in its own view. Paths are workspace
// relative. The tests topic sends the whole tree as a delta.

// Test op names. Reading the tree and a test's output is open to
// viewers; running, debugging and stopping need a controller (a run is
// the project's own code, as on the laptop: no card).
const (
	OpTestsRun     = "tests.run"
	OpTestsDebug   = "tests.debug"
	OpTestsStop    = "tests.stop"
	OpTestsRefresh = "tests.refresh"
	OpTestsOutput  = "tests.output"
)

var opTestsRows = []Policy{
	{Op: OpTestsOutput, New: newOf[TestOutputArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpTestsRefresh, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpTestsRun, New: newOf[TestRunArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpTestsDebug, New: newOf[TestIDArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpTestsStop, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opTestsRows...); return true }()

func init() {
	registerTopic("tests", TestsSnap{}, TestsSnap{})
	registerResult(OpTestsOutput, TestOutput{})
}

// Test Explorer limits.
const (
	MaxTestItems       = 5000      // rows of the tree (packages, files, tests)
	MaxTestMessage     = 2 << 10   // bytes of a failure message
	MaxTestOutputLines = 2000      // lines of one tests.output
	MaxTestOutputBytes = 256 << 10 // bytes of one tests.output, the newest kept
	MaxTestID          = MaxPath + 1024
)

// TestsSnap is the tests topic.
type TestsSnap struct {
	// Framework is the toolchain the laptop found ("go test", "pytest",
	// "cargo test", "jest", "vitest"); "" when none.
	Framework string `json:"framework,omitempty"`
	State     string `json:"state" enum:"idle|discovering|running"`
	// Error is why there is no tree (no runner found, discovery failed);
	// RunError why the last run did not start or ended without results.
	Error    string     `json:"error,omitempty"`
	RunError string     `json:"run_error,omitempty"`
	Counts   TestCounts `json:"counts"`
	// Command is the last run's command, to read ("go test -json ./...").
	Command   string `json:"command,omitempty"`
	Started   int64  `json:"started,omitempty"`    // Unix ms the last run started
	ElapsedMs int64  `json:"elapsed_ms,omitempty"` // of the last run (at the snap while it runs)
	Live      bool   `json:"live,omitempty"`       // elapsed grows: the page adds the time since
	// Items is the tree, depth first: each package, its files, their tests.
	Items []TestItem `json:"items"`
	More  int        `json:"more,omitempty"` // rows past MaxTestItems
}

// TestCounts tallies the tests of the tree.
type TestCounts struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Running int `json:"running"`
}

// TestItem is one row of the tree. ID is what the ops take: "p:<dir>"
// a package, "f:<path>" a file, "c:<path>::<name>" a test. A package
// or file's State sums up its tests (failed, running, passed, skipped,
// then pending).
type TestItem struct {
	ID     string `json:"id"`
	Kind   string `json:"kind" enum:"package|file|case"`
	Parent string `json:"parent,omitempty"`
	Label  string `json:"label"`
	Path   string `json:"path,omitempty"` // a file or test's file
	Line   int    `json:"line,omitempty"` // a test's line, 1-based (0: unknown)
	State  string `json:"state" enum:"pending|running|passed|failed|skipped"`
	Ms     int64  `json:"ms,omitempty"` // how long the test took
	// Message is a failed test's first lines, or a package's error.
	Message string `json:"message,omitempty"`
	// FailPath and FailLine point at the failing line when the runner
	// said where (workspace relative).
	FailPath string `json:"fail_path,omitempty"`
	FailLine int    `json:"fail_line,omitempty"`
	// Debug: the laptop can debug this test (Go and pytest).
	Debug bool `json:"debug,omitempty"`
}

// TestOutput answers tests.output: one test's own output from the last
// run, or (no id) the whole run's log.
type TestOutput struct {
	ID        string   `json:"id,omitempty"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated,omitempty"`
}

// TestIDArgs names one row of the tree.
type TestIDArgs struct {
	ID string `json:"id"`
}

// TestOutputArgs names a row whose output to read; no ID: the whole
// run's log.
type TestOutputArgs struct {
	ID string `json:"id,omitempty"`
}

// TestRunArgs runs one row of the tree (ID), every test (no ID), or the
// tests that failed last time (Failed, no ID).
type TestRunArgs struct {
	ID     string `json:"id,omitempty"`
	Failed bool   `json:"failed,omitempty"`
}

// validTestID is "p:", "f:" or "c:" and a workspace path ("c:" with
// "::name" after it); "p:" may name the root ("p:").
func validTestID(id string) bool {
	if len(id) > MaxTestID || hasControl(id) || len(id) < 2 || id[1] != ':' {
		return false
	}
	rest := id[2:]
	switch id[0] {
	case 'p':
		return rest == "" || validPath(rest, false)
	case 'f':
		return validPath(rest, false)
	case 'c':
		path, name, ok := strings.Cut(rest, "::")
		return ok && name != "" && validPath(path, false)
	}
	return false
}

func (a *TestIDArgs) validate(int) error {
	if !validTestID(a.ID) {
		return invalid("bad test id")
	}
	return nil
}

func (a *TestOutputArgs) validate(int) error {
	if a.ID != "" && !validTestID(a.ID) {
		return invalid("bad test id")
	}
	return nil
}

func (a *TestRunArgs) validate(int) error {
	if a.ID != "" && (a.Failed || !validTestID(a.ID)) {
		return invalid("bad test id")
	}
	return nil
}
