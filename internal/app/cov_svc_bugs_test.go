package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Restart of a never-created service is a compose up. When the up
// cannot start, the waiting caller (the AI) must hear the error, not
// a success.
func TestCovSvcRestartNotCreatedReportsStartFailure(t *testing.T) {
	m, _ := covSvcProject(t) // no editor: the up tab cannot start
	called := 0
	var got error
	note, _ := m.servicesRestart(svcSecCompose, "mailhog", func(_ *Model, err error) { called++; got = err })
	if note != "" || !strings.Contains(regToast(m), "Cannot start services") {
		t.Fatalf("note=%q toast=%q", note, regToast(m))
	}
	if called != 1 || got == nil {
		t.Errorf("done called %d times with %v, want once with an error", called, got)
	}
}

// "Replace tag" on the card that moves an existing tag must run
// `docker tag`; it was dropped as a Cancel.
func TestCovSvcReplaceTagCardRunsTag(t *testing.T) {
	m, f := covSvcHost(t)
	m.askImageTag("postgres:16")
	submitPrompt(t, m, "redis:7")
	if !m.confirmOpen || !strings.Contains(cardText(m), "Replace image tag?") {
		t.Fatalf("card = %s", cardText(m))
	}
	pumpHost(t, m, covSvcConfirm(t, m, "tag"))
	if !strings.Contains(covSvcCalls(f), "tag postgres:16 redis:7") {
		t.Errorf("calls = %q", f.Calls())
	}
}

// The AI's "remove container" opens the remove card; it must not also
// answer "unsupported container command".
func TestCovSvcAIContainerRemoveOpensCardWithoutError(t *testing.T) {
	m, _ := covSvcHost(t)
	cmd, err := m.dispatchContainerAIAction("containers-remove", "nginx")
	if err != nil || cmd != nil {
		t.Errorf("err = %v", err)
	}
	if !m.confirmOpen || !strings.Contains(cardText(m), "Remove container?") {
		t.Errorf("card = %s", cardText(m))
	}
}

// A .env DATABASE_URL suggestion is tested with the URL's password, not
// with the whole URL as the password.
func TestCovSvcDBEnvSuggestionTestsWithURLPassword(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	if err := os.WriteFile(filepath.Join(s.root, ".env"), []byte("DATABASE_URL=postgres://app:pw@localhost:5432/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []string
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(_ context.Context, _ dbclient.Conn, password string) (dbclient.Session, error) {
		got = append(got, password)
		return &covSvcSQL{}, nil
	}
	defer func() { dbSQLSessionFactory = old }()
	s.nodes["suggest-env-database_url"] = dbNode{kind: dbNodeSuggestion, name: "DATABASE_URL", path: "suggest-env-database_url"}
	covSvcRun(t, m, m.databaseAddSuggestion("suggest-env-database_url"))
	if len(got) != 1 || got[0] != "pw" {
		t.Errorf("tested with password %q, want \"pw\"", got)
	}
}
