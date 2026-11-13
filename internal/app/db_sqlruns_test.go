package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-45: saved SQL runs. The palette lists the runs of
// .termocode/sql-runs.json; a pick opens a Cancel-first card with the
// script, the connections, the before steps and the log file; the job
// keeps the passwords out of the command line.

func sqlRunsTestModel(t *testing.T, runs string) *Model {
	t.Helper()
	root := t.TempDir()
	if runs != "" {
		p := dbclient.SQLRunsPath(root)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(runs), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := newDBState()
	s.loaded, s.root = true, root
	s.conns = []dbclient.Conn{
		{ID: "local", Name: "Local PG", Driver: "postgres", Host: "db", User: "ann"},
		{ID: "file", Name: "App file", Driver: "sqlite", Path: "app.db"},
	}
	m := dbTestModel(s)
	m.w, m.h = 140, 40
	return m
}

const sqlRunsTestFile = `{"runs":[{"name":"nightly","script":"r.sql","connections":["local","App file"],"before":["make seed"],"log":"logs/r.log"}]}`

func TestSQLRunsPaletteListsRuns(t *testing.T) {
	found := false
	for _, it := range databasesPaletteItems() {
		found = found || (it.ID == dbSQLRunPaletteID && it.Title == dbSQLRunPaletteTitle)
	}
	if !found {
		t.Fatal("no palette item db-saved-runs")
	}
	m := sqlRunsTestModel(t, sqlRunsTestFile)
	m.databaseSavedRuns()
	if !m.pickerOpen || m.pickerKind != pickerKindDBSQLRuns {
		t.Fatal("no saved runs picker")
	}
	view := ansi.Strip(m.picker.View())
	for _, want := range []string{"nightly", "r.sql", "Edit sql-runs.json"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker lacks %q:\n%s", want, view)
		}
	}
}

func TestSQLRunsNoFileIsAToast(t *testing.T) {
	m := sqlRunsTestModel(t, "")
	m.databaseSavedRuns()
	if m.pickerOpen {
		t.Fatal("a picker without runs")
	}
	if got := toastText(*m); !strings.Contains(got, "No saved SQL runs") {
		t.Fatalf("toast %q", got)
	}
}

func TestSQLRunCardIsCancelFirstAndShowsEverything(t *testing.T) {
	m := sqlRunsTestModel(t, sqlRunsTestFile)
	m.databaseSavedRuns()
	m.pickerOpen = false
	if _, ok := m.databaseSQLRunPicked("nightly"); !ok {
		t.Fatal("pick not handled")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindDBSQLRun {
		t.Fatal("no run card")
	}
	if ids := buttonIDs(m.confirm); ids != "cancel,run" {
		t.Fatalf("buttons %s (Cancel first)", ids)
	}
	flat := strings.Join(strings.Fields(strings.NewReplacer("│", " ").Replace(ansi.Strip(m.confirm.Box()))), " ")
	for _, want := range []string{"r.sql", "Local PG, App file", "make seed", "logs/r.log"} {
		if !strings.Contains(flat, want) {
			t.Errorf("card lacks %q:\n%s", want, flat)
		}
	}
	m.confirmOpen = false
	if _, ok := m.databaseSQLRunConfirm("cancel"); !ok {
		t.Fatal("cancel not handled")
	}
	if m.databases.sqlRunPending != "" {
		t.Error("cancel kept the pending run")
	}
}

func TestSQLRunUnknownConnectionIsRefused(t *testing.T) {
	m := sqlRunsTestModel(t, `{"runs":[{"name":"x","script":"r.sql","connections":["nope"]}]}`)
	m.pickerKind = pickerKindDBSQLRuns
	m.databaseSQLRunPicked("x")
	if m.confirmOpen {
		t.Fatal("a card for an unknown connection")
	}
	if got := toastText(*m); !strings.Contains(got, "Unknown connection") {
		t.Fatalf("toast %q", got)
	}
}

func TestSQLRunJobKeepsPasswordsInTheEnvironment(t *testing.T) {
	c := dbclient.Conn{ID: "local", Name: "Local PG", Driver: "postgres", Host: "db", User: "ann"}
	cli, err := dbclient.CLICommand(c, dbclient.CLIOptions{Password: "pw-123"})
	if err != nil {
		t.Fatal(err)
	}
	j, err := dbSQLRunJob(dbclient.SQLRun{Name: "nightly", Script: "r.sql"}, []dbclient.SQLRunTarget{{Name: "Local PG", CLI: cli}}, "/work")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(j.Cmdline, "pw-123") {
		t.Errorf("password on the command line: %s", j.Cmdline)
	}
	if j.Env["TC_SQLRUN_1_PGPASSWORD"] != "pw-123" || j.Dir != "/work" || !strings.Contains(j.Label, "nightly") {
		t.Errorf("job %+v", j)
	}
	if _, err := dbSQLRunJob(dbclient.SQLRun{Name: "n"}, nil, "/w"); err == nil {
		t.Error("a job without connections")
	}
}

func TestSQLRunIsNeverAnAIAction(t *testing.T) {
	p, ok := aiActionPolicies[dbSQLRunPaletteID]
	if !ok || p.Gate != aiGateNever {
		t.Fatalf("db-saved-runs must be aiNever: %+v %v", p, ok)
	}
}

// DBX-45: pause output. "Databases: Pause / Resume SQL Run Output" sends
// Ctrl-S (stop) and then Ctrl-Q (go on) to the terminal tab of the run.
func TestSQLRunPauseTogglesOutputOfTheRunTab(t *testing.T) {
	var sent []string
	old := dbRunChansend
	dbRunChansend = func(_ *Model, buf int, text string) { sent = append(sent, strings.Join([]string{string(rune('0' + buf)), text}, ":")) }
	defer func() { dbRunChansend = old }()

	m := sqlRunsTestModel(t, "")
	m.terminalTabs = []terminalTab{
		{BufID: 1, Name: "SQL run · old", Running: false},
		{BufID: 2, Name: "SQL run · nightly", Running: true},
		{BufID: 3, Name: "zsh", Running: true},
	}
	m.terminalActiveTab = 2 // a plain shell is in front; the run is still found
	m.databasePauseRun()
	m.databasePauseRun()
	want := []string{"2:\x13", "2:\x11"}
	if strings.Join(sent, "|") != strings.Join(want, "|") {
		t.Fatalf("sent %q, want %q", sent, want)
	}
	if got := toastText(*m); !strings.Contains(got, "Resumed") {
		t.Fatalf("toast %q", got)
	}
}

func TestSQLRunPauseWithoutARunSaysSo(t *testing.T) {
	old := dbRunChansend
	dbRunChansend = func(*Model, int, string) { t.Error("sent to a tab that is no run") }
	defer func() { dbRunChansend = old }()
	m := sqlRunsTestModel(t, "")
	m.terminalTabs = []terminalTab{{BufID: 1, Name: "SQL run · done", Running: false}, {BufID: 3, Name: "zsh", Running: true}}
	m.databasePauseRun()
	if got := toastText(*m); !strings.Contains(got, "No SQL run is going") {
		t.Fatalf("toast %q", got)
	}
}

func TestSQLRunPauseIsInThePaletteAndNeverAnAIAction(t *testing.T) {
	found := false
	for _, it := range databasesPaletteItems() {
		found = found || it.ID == dbSQLRunPauseID
	}
	if !found {
		t.Fatal("no palette item for pause")
	}
	if p, ok := aiActionPolicies[dbSQLRunPauseID]; !ok || p.Gate != aiGateNever {
		t.Fatalf("must be aiNever: %+v %v", p, ok)
	}
}
