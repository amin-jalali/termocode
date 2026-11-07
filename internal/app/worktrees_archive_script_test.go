//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/worktreeform"
)

// wtArchiveScriptSeam records the archive script runs instead of running
// them; fail makes each run fail on its first line.
func wtArchiveScriptSeam(t *testing.T, fail bool) *[]string {
	t.Helper()
	var runs []string
	old := wtArchiveScriptExec
	wtArchiveScriptExec = func(dir, _ string, lines []string) (string, error) {
		if _, err := os.Stat(dir); err != nil {
			runs = append(runs, "after the folder went: "+dir)
			return "", nil
		}
		runs = append(runs, dir+": "+strings.Join(lines, " && "))
		if fail {
			return lines[0], os.ErrInvalid
		}
		return "", nil
	}
	t.Cleanup(func() { wtArchiveScriptExec = old })
	return &runs
}

func wtWriteArchiveConfig(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, ".termocode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(worktreeform.ConfigFile)),
		[]byte(`{"archive": ["docker compose down"]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, hash, err := worktreeform.LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// CMP-15: the repository's archive script is untrusted input. x shows it
// on the archive card; untrusted, plain Archive skips it and "Trust
// script and archive" (never first) stores the trust and runs it in the
// folder before the folder goes. Trusted, it runs with no extra card.
func TestWorktreeArchiveScriptTrust(t *testing.T) {
	runs := wtArchiveScriptSeam(t, false)
	m, root, x, _ := wtRealModel(t)
	hash := wtWriteArchiveConfig(t, root)

	m.wt.cursorPath = x
	wtStep(t, m, wtKey(t, m, "x"))
	if !m.confirmOpen || m.confirmKind != confirmKindWTArchive || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no safe-first card: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	for _, want := range []string{"docker compose down", "not trusted", "Trust script and archive"} {
		if !strings.Contains(wtBox(m), want) {
			t.Errorf("card lacks %q:\n%s", want, wtBox(m))
		}
	}
	// Plain Archive skips the untrusted script.
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("archive")
	wtStep(t, m, cmd)
	if len(*runs) != 0 || loadSession().WorktreeSetupTrusted != "" {
		t.Fatalf("untrusted script ran: %v", *runs)
	}
	if _, err := os.Stat(x); !os.IsNotExist(err) {
		t.Fatal("the folder is still there")
	}

	// Restore, then trust and archive: the script runs first, in the folder.
	wtRunLoad(t, m, m.worktreesReload())
	m.wt.cursorPath = x
	wtStep(t, m, wtKey(t, m, "x"))
	wtRunLoad(t, m, m.worktreesReload())
	m.wt.cursorPath = x
	wtStep(t, m, wtKey(t, m, "x"))
	m.confirmOpen = false
	cmd, _ = m.handleAIConfirm("trust")
	wtStep(t, m, cmd)
	if strings.Join(*runs, "|") != x+": docker compose down" {
		t.Fatalf("runs %v", *runs)
	}
	if loadSession().WorktreeSetupTrusted != hash {
		t.Fatalf("trust not stored: %q", loadSession().WorktreeSetupTrusted)
	}
	if _, err := os.Stat(x); !os.IsNotExist(err) {
		t.Fatal("the folder is still there")
	}

	// Trusted now: the card says it runs and has no trust button.
	wtRunLoad(t, m, m.worktreesReload())
	m.wt.cursorPath = x
	wtStep(t, m, wtKey(t, m, "x"))
	wtRunLoad(t, m, m.worktreesReload())
	m.wt.cursorPath = x
	wtStep(t, m, wtKey(t, m, "x"))
	if box := wtBox(m); !strings.Contains(box, "runs first") || strings.Contains(box, "Trust script") {
		t.Errorf("trusted card:\n%s", box)
	}
}

// A failing archive script does not keep the folder: the archive goes on
// and the result names the failed line.
func TestWorktreeArchiveScriptFails(t *testing.T) {
	runs := wtArchiveScriptSeam(t, true)
	m, root, x, _ := wtRealModel(t)
	hash := wtWriteArchiveConfig(t, root)
	sess := loadSession()
	sess.WorktreeSetupTrusted = hash
	saveSession(sess)

	m.wt.cursorPath = x
	wtStep(t, m, wtKey(t, m, "x"))
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("archive")
	msg, ok := cmd().(wtArchivedMsg)
	if !ok {
		t.Fatal("not an archive result")
	}
	if msg.err != nil || len(*runs) != 1 {
		t.Fatalf("err %v runs %v", msg.err, *runs)
	}
	if len(msg.scriptFailed) != 1 || !strings.Contains(msg.scriptFailed[0], "docker compose down") {
		t.Errorf("failed lines %v", msg.scriptFailed)
	}
	if _, err := os.Stat(x); !os.IsNotExist(err) {
		t.Fatal("the folder is still there")
	}
}

// The note on the delete and clean up cards.
func TestWTArchiveScriptNote(t *testing.T) {
	if n := wtArchiveScriptNote(wtArchiveScript{}); n != "" {
		t.Errorf("no script: %q", n)
	}
	n := wtArchiveScriptNote(wtArchiveScript{lines: []string{"make stop"}, trusted: true})
	if !strings.Contains(n, "make stop") || !strings.Contains(n, "runs first") {
		t.Errorf("trusted: %q", n)
	}
	n = wtArchiveScriptNote(wtArchiveScript{lines: []string{"make stop"}})
	if !strings.Contains(n, "not trusted") || !strings.Contains(n, "skipped") {
		t.Errorf("untrusted: %q", n)
	}
}
