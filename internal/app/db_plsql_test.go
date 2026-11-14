package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-63: Oracle PL/SQL source edit. E on a package asks for the spec or
// the body; the source comes from all_source; a submit that compiles with
// errors reads all_errors and says where they are.

func plsqlModel(t *testing.T) (*Model, *cdbSession) {
	t.Helper()
	m := cdbModel(t, dbclient.Conn{ID: "ora", Driver: "oracle", Host: "h", Port: 1521})
	s := m.databases
	cdbAddNode(s, "ora", "ora/db/ORCL", dbNodeDatabase, "ORCL")
	cdbAddNode(s, "ora/db/ORCL", "ora/db/ORCL/schema/HR", dbNodeSchema, "HR")
	cdbAddNode(s, "ora/db/ORCL/schema/HR", "ora/db/ORCL/schema/HR/Packages", dbNodeGroup, "Packages")
	cdbAddNode(s, "ora/db/ORCL/schema/HR/Packages", "ora/db/ORCL/schema/HR/Packages/PKG", dbNodeRoutine, "PKG")
	cdbAddNode(s, "ora/db/ORCL/schema/HR", "ora/db/ORCL/schema/HR/routine/P1", dbNodeRoutine, "P1")
	sess := &cdbSession{run: func(q string) dbclient.Result {
		switch {
		case strings.Contains(q, "all_errors"):
			return dbclient.Result{Rows: [][]dbclient.Cell{{{Text: "3"}, {Text: "5"}, {Text: "PLS-00103: Encountered the symbol \"END\""}, {Text: "ERROR"}}}}
		case strings.Contains(q, "all_source") && strings.Contains(q, "'PACKAGE BODY'"):
			return dbclient.Result{Rows: [][]dbclient.Cell{{{Text: "PACKAGE BODY pkg AS\n"}}, {{Text: "  PROCEDURE p IS BEGIN NULL; END;\n"}}, {{Text: "END pkg;"}}}}
		case strings.Contains(q, "all_source"):
			return dbclient.Result{Rows: [][]dbclient.Cell{{{Text: "PROCEDURE p1 IS\n"}}, {{Text: "BEGIN NULL; END;"}}}}
		}
		return dbclient.Result{}
	}}
	s.sessions["ora"] = sess
	return m, sess
}

func TestOraclePackageEditAsksSpecOrBody(t *testing.T) {
	m, _ := plsqlModel(t)
	m.databases.databaseSelectPath("ora/db/ORCL/schema/HR/Packages/PKG")
	m.databaseEditSourceStart()
	if !m.pickerOpen || m.pickerKind != pickerKindDBSourcePackagePart {
		t.Fatalf("no spec/body picker: %q", covToastText(m))
	}
	m.pickerOpen = false
	cmd, ok := m.databaseConsolePicker("body")
	if !ok {
		t.Fatal("pick not routed")
	}
	msgs := cdbMsgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("msgs %v", msgs)
	}
	got := msgs[0].(dbSourceLoadedMsg)
	if got.err != nil || got.kind != dbclient.ObjectPackageBody || got.name != "HR.PKG" || !strings.HasPrefix(got.text, "CREATE OR REPLACE PACKAGE BODY pkg AS\n") {
		t.Fatalf("%+v", got)
	}
}

// A routine reads as the procedure or function it is.
func TestOracleRoutineEditFindsItsKind(t *testing.T) {
	m, _ := plsqlModel(t)
	m.databases.databaseSelectPath("ora/db/ORCL/schema/HR/routine/P1")
	msgs := cdbMsgs(m.databaseEditSourceStart())
	if len(msgs) != 1 {
		t.Fatalf("msgs %v (%q)", msgs, covToastText(m))
	}
	got := msgs[0].(dbSourceLoadedMsg)
	if got.err != nil || got.kind != dbclient.ObjectProcedure || !strings.HasPrefix(got.text, "CREATE OR REPLACE PROCEDURE p1") {
		t.Fatalf("%+v", got)
	}
}

func TestOracleSubmitShowsCompileErrors(t *testing.T) {
	m, sess := plsqlModel(t)
	st := m.databases.sourceState()
	text := "CREATE OR REPLACE PACKAGE BODY pkg AS\n  PROCEDURE p IS BEGIN NULL END;\nEND pkg;\n"
	e := dbclient.NewSourceEdit("ora", dbclient.ObjectPackageBody, "HR.PKG", "CREATE OR REPLACE PACKAGE BODY pkg AS\nEND pkg;\n")
	e.Text = text
	st.edits["/x/pkg.sql"] = &dbSourceOpen{edit: e, driver: "oracle", sessKey: "ora"}
	plan, err := dbclient.SubmitObjectSource("oracle", e.Kind, e.Name, text)
	if err != nil {
		t.Fatal(err)
	}
	st.submit = &dbSourceSubmit{file: "/x/pkg.sql", plan: plan}
	m.confirmKind = confirmKindDBSourceSubmit
	cmd, ok := m.databaseSourceConfirm("submit")
	if !ok {
		t.Fatal("not routed")
	}
	msgs := cdbMsgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("msgs %v", msgs)
	}
	stored := msgs[0].(dbSourceStoredMsg)
	if !strings.Contains(sess.ran(), "CREATE OR REPLACE PACKAGE BODY pkg") || !strings.Contains(sess.ran(), "all_errors") {
		t.Fatalf("ran:\n%s", sess.ran())
	}
	if stored.err != nil || len(stored.compile) != 1 || stored.compile[0].Line != 3 {
		t.Fatalf("%+v", stored)
	}
	m.onDBSourceStored(stored)
	if toast := covToastText(m); !strings.Contains(toast, "compiled with 1 error") || !strings.Contains(toast, "line 3") || !strings.Contains(toast, "PLS-00103") {
		t.Fatalf("toast %q", toast)
	}
}
