package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Archive script of a worktree (CMP-15) ────────────────────────────────
//
// .termocode/worktrees.json "archive" lines run in a worktree's folder
// just before the folder goes: Archive (x), Delete (D) and Clean Up (C).
// They stop the services and free the ports the worktree started. They
// come from the repository, so they are untrusted input: they run only
// when this exact file is trusted (the same trust as the setup lines;
// the hash covers the whole file). The archive card shows the lines;
// untrusted, plain Archive skips them and "Trust script and archive"
// (never first) stores the trust and runs them. The delete and clean up
// cards say whether they run. A failing line never keeps the folder: the
// removal goes on and the toast names the line and its log (in the git
// dir). Each line has a time limit, so a hung script cannot hold the
// removal forever. The environment is the user's minus termocode's
// secrets (clistep.CleanEnv).

// wtArchiveLineTimeout is the time limit of one archive line.
const wtArchiveLineTimeout = 2 * time.Minute

// wtArchiveScript is the archive script of the repository.
type wtArchiveScript struct {
	lines   []string
	hash    string
	trusted bool
}

// runs says whether the script runs before a folder goes.
func (s wtArchiveScript) runs() bool { return len(s.lines) > 0 && s.trusted }

// wtMainTop is the main checkout of the view's repository ("" = unknown).
func (m *Model) wtMainTop() string {
	for _, e := range m.wt.entries {
		if e.Kind == worktrees.KindMain {
			return e.Path
		}
	}
	return ""
}

// wtArchiveScriptLoad reads the archive lines of the main checkout's
// worktrees.json and whether this file is trusted. A missing or broken
// file is no script.
func (m *Model) wtArchiveScriptLoad() wtArchiveScript {
	if m.wt == nil || m.wt.off {
		return wtArchiveScript{}
	}
	top := m.wtMainTop()
	if top == "" {
		return wtArchiveScript{}
	}
	cfg, hash, err := worktreeform.LoadConfig(top)
	if err != nil {
		recordError("[worktrees] archive script: " + err.Error())
		return wtArchiveScript{}
	}
	var lines []string
	for _, l := range cfg.Archive {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return wtArchiveScript{lines: lines, hash: hash,
		trusted: worktreeform.SetupTrusted(hash, loadSession().WorktreeSetupTrusted)}
}

// wtArchiveScriptLines is the lines for a card, one per row.
func wtArchiveScriptLines(s wtArchiveScript) string {
	var b strings.Builder
	for _, l := range s.lines {
		b.WriteString("\n    " + aiCleanText(l))
	}
	return b.String()
}

// wtArchiveScriptNote is the piece of the delete and clean up cards
// ("" = no script).
func wtArchiveScriptNote(s wtArchiveScript) string {
	switch {
	case len(s.lines) == 0:
		return ""
	case s.trusted:
		return "\n\nThe archive script of " + worktreeform.ConfigFile + " runs first in each folder:" + wtArchiveScriptLines(s)
	}
	return "\n\nThe archive script of " + worktreeform.ConfigFile + " is not trusted, so it is skipped:" + wtArchiveScriptLines(s) +
		"\n\nx (Archive) can trust it."
}

// wtArchiveLogPath is the archive log of the worktree folder.
func wtArchiveLogPath(common, folder string) string {
	return filepath.Join(common, "termocode", "archive", filepath.Base(folder)+".log")
}

// wtArchiveScriptExec runs the lines in dir, output to logPath; the
// first failure stops it (a var: tests).
var wtArchiveScriptExec = func(dir, logPath string, lines []string) (failed string, err error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return "", err
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer logf.Close()
	env := clistep.CleanEnv(os.Environ())
	for _, l := range lines {
		fmt.Fprintf(logf, "$ %s\n", l)
		ctx, cancel := context.WithTimeout(context.Background(), wtArchiveLineTimeout)
		cmd := spawn.CommandContext(ctx, "/bin/sh", "-c", l)
		cmd.Dir = dir
		cmd.Env = env
		cmd.Stdout, cmd.Stderr = logf, logf
		cmd.WaitDelay = 5 * time.Second
		err := cmd.Run()
		timedOut := ctx.Err() != nil
		cancel()
		if err != nil {
			if timedOut {
				err = fmt.Errorf("stopped after %s", wtArchiveLineTimeout)
			}
			fmt.Fprintf(logf, "\n✗ %v\n", err)
			return l, err
		}
	}
	fmt.Fprintln(logf, "\n✓ archive script done")
	return "", nil
}

// wtRunArchiveScript runs the script in folder before it goes (from a
// background step). It returns a line for the toast when it failed ("" =
// it ran fine or did not run).
func wtRunArchiveScript(s wtArchiveScript, common, folder string) string {
	if !s.runs() {
		return ""
	}
	if _, err := os.Stat(folder); err != nil {
		return "" // no folder: nothing to stop
	}
	logPath := wtArchiveLogPath(common, folder)
	if common == "" {
		logPath = filepath.Join(os.TempDir(), "termocode-archive-"+filepath.Base(folder)+".log")
	}
	failed, err := wtArchiveScriptExec(folder, logPath, s.lines)
	if err == nil {
		return ""
	}
	what := "the archive script"
	if failed != "" {
		what = "`" + aiCleanText(failed) + "`"
	}
	return wtShortPath(folder) + ": " + what + " failed (" + aiCleanText(err.Error()) + "); log: " + wtShortPath(logPath)
}

// wtArchiveScriptToast is the detail of the warn toast after a removal
// whose script failed ("" = none failed).
func wtArchiveScriptToast(failed []string) string {
	if len(failed) == 0 {
		return ""
	}
	return "The folder went anyway. " + strings.Join(failed, "; ")
}
