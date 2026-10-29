package app

import (
	"strings"
	"testing"
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
